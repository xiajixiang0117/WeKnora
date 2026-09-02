import os
import unittest
from unittest.mock import AsyncMock, patch

from docreader.parser.web_parser import (
    StdWebParser,
    WebParseError,
    WebParser,
    _ScrapeResult,
    apply_web_content_rules,
    build_visible_text_fallback,
    extract_markdown_from_html,
    install_ssrf_route_guard,
)
from docreader.utils.ssrf import is_ssrf_safe_url, reset_ssrf_whitelist_cache_for_test


class TestWebParserHelpers(unittest.TestCase):
    def setUp(self) -> None:
        self._env_patch = patch.dict(
            os.environ,
            {"SSRF_WHITELIST": "", "SSRF_WHITELIST_EXTRA": ""},
            clear=False,
        )
        self._env_patch.start()
        reset_ssrf_whitelist_cache_for_test()

    def tearDown(self) -> None:
        self._env_patch.stop()
        reset_ssrf_whitelist_cache_for_test()

    def test_extract_markdown_empty_html(self):
        self.assertIsNone(extract_markdown_from_html(""))
        self.assertIsNone(extract_markdown_from_html("   "))

    def test_extract_markdown_article_html(self):
        html = """
        <html><head><title>Demo</title></head><body>
        <article><h1>Hello</h1><p>World paragraph with enough text for extraction.</p></article>
        </body></html>
        """
        md = extract_markdown_from_html(html)
        self.assertIsNotNone(md)
        self.assertIn("Hello", md)

    def test_build_fallback_too_short(self):
        self.assertIsNone(build_visible_text_fallback("short"))
        self.assertIsNone(build_visible_text_fallback(""))

    def test_build_fallback_with_title(self):
        text = "A" * 60
        md = build_visible_text_fallback(text, page_title="WeKnora")
        self.assertIsNotNone(md)
        self.assertTrue(md.startswith("# WeKnora"))
        self.assertIn(text, md)

    def test_build_fallback_without_title(self):
        text = "B" * 60
        md = build_visible_text_fallback(text, page_title="")
        self.assertEqual(md, text)

    def test_install_ssrf_route_guard_is_importable(self):
        self.assertTrue(callable(install_ssrf_route_guard))

    def test_redirect_target_blocked_before_navigation(self):
        safe, reason = is_ssrf_safe_url("http://127.0.0.1:39127/audit.txt")
        self.assertFalse(safe)
        self.assertTrue(reason)

    def test_content_selector_keeps_only_selected_dom(self):
        html = """
        <html><body><nav>Side navigation</nav>
        <main><h1>Guide</h1><p>Main documentation content.</p></main>
        </body></html>
        """
        filtered, applied = apply_web_content_rules(html, "main", "")
        self.assertTrue(applied)
        self.assertIn("Guide", filtered)
        self.assertNotIn("Side navigation", filtered)

    def test_exclude_selector_removes_sidebar_from_selected_dom(self):
        html = """
        <main><article><h1>Guide</h1><aside>Sidebar links</aside>
        <p>Main documentation content.</p></article></main>
        """
        filtered, applied = apply_web_content_rules(html, "article", "aside")
        self.assertTrue(applied)
        self.assertIn("Main documentation content", filtered)
        self.assertNotIn("Sidebar links", filtered)

    def test_missing_content_selector_raises(self):
        with self.assertRaises(WebParseError):
            apply_web_content_rules("<main>Content</main>", ".does-not-exist", "")

    def test_invalid_css_selector_raises(self):
        with self.assertRaises(WebParseError):
            apply_web_content_rules("<main>Content</main>", "main[", "")


class TestStdWebParserFailures(unittest.TestCase):
    """Scrape/parse failures must raise, not become indexable document body."""

    def _parser(self) -> StdWebParser:
        return StdWebParser(title="page")

    def test_empty_scrape_raises_instead_of_error_body(self):
        empty = _ScrapeResult(
            html="",
            visible_text="",
            page_title="",
            error="navigation failed: Timeout",
        )
        parser = self._parser()
        with patch.object(parser, "scrape", new=AsyncMock(return_value=empty)):
            with self.assertRaises(WebParseError) as ctx:
                parser.parse_into_text(b"https://example.com/blocked")
        message = str(ctx.exception)
        self.assertIn("https://example.com/blocked", message)
        self.assertIn("navigation failed", message)
        self.assertNotIn("Error parsing web page:", message)

    def test_empty_scrape_without_error_field_still_raises(self):
        empty = _ScrapeResult(html="", visible_text="", page_title="")
        parser = self._parser()
        with patch.object(parser, "scrape", new=AsyncMock(return_value=empty)):
            with self.assertRaises(WebParseError) as ctx:
                parser.parse_into_text(b"https://example.com/blank")
        self.assertIn("no HTML or visible text", str(ctx.exception))

    def test_unextractable_page_raises_instead_of_error_body(self):
        scrape = _ScrapeResult(
            html="<html><body><div></div></body></html>",
            visible_text="short",
            page_title="",
        )
        parser = self._parser()
        with patch.object(parser, "scrape", new=AsyncMock(return_value=scrape)):
            with patch(
                "docreader.parser.web_parser.extract_markdown_from_html",
                return_value=None,
            ):
                with patch(
                    "docreader.parser.web_parser.build_visible_text_fallback",
                    return_value=None,
                ):
                    with self.assertRaises(WebParseError) as ctx:
                        parser.parse_into_text(b"https://example.com/empty")
        self.assertIn("Failed to parse web page", str(ctx.exception))
        self.assertIn("https://example.com/empty", str(ctx.exception))

    def test_successful_scrape_returns_markdown_document(self):
        html = """
        <html><head><title>Demo</title></head><body>
        <article><h1>Hello</h1><p>World paragraph with enough text for extraction.</p></article>
        </body></html>
        """
        scrape = _ScrapeResult(
            html=html,
            visible_text="Hello World paragraph with enough text for extraction.",
            page_title="Demo",
        )
        parser = self._parser()
        with patch.object(parser, "scrape", new=AsyncMock(return_value=scrape)):
            doc = parser.parse_into_text(b"https://example.com/ok")
        self.assertTrue(doc.is_valid())
        self.assertNotIn("Error parsing web page:", doc.content)
        self.assertIn("Hello", doc.content)

    def test_selector_rules_are_applied_before_markdown_extraction(self):
        html = """
        <html><body><nav>Navigation that must not be indexed</nav>
        <article><h1>Selected title</h1><ul><li>First item</li></ul>
        <aside>Sidebar that must not be indexed</aside>
        <pre><code>print('ok')</code></pre></article></body></html>
        """
        scrape = _ScrapeResult(html=html, visible_text="ignored", page_title="Demo")
        parser = StdWebParser(
            title="page",
            web_content_selector="article",
            web_exclude_selectors="aside",
        )
        with patch.object(parser, "scrape", new=AsyncMock(return_value=scrape)):
            doc = parser.parse_into_text(b"https://example.com/selected")
        self.assertIn("Selected title", doc.content)
        self.assertIn("First item", doc.content)
        self.assertNotIn("Navigation that must not be indexed", doc.content)
        self.assertNotIn("Sidebar that must not be indexed", doc.content)

    def test_pipeline_web_parser_does_not_index_scrape_error(self):
        empty = _ScrapeResult(
            html="",
            visible_text="",
            page_title="",
            error="URL blocked by SSRF guard: loopback",
        )
        pipeline = WebParser(title="page")
        with patch.object(StdWebParser, "scrape", new=AsyncMock(return_value=empty)):
            with self.assertRaises(WebParseError):
                pipeline.parse_into_text(b"https://example.com/ssrf")


if __name__ == "__main__":
    unittest.main()
