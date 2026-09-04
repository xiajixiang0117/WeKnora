package webcrawler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

func TestCanonicalURL(t *testing.T) {
	got := CanonicalURL("HTTPS://Docs.Example.COM/a/../guide/?utm_source=x&keep=1#part")
	if got != "https://docs.example.com/guide?keep=1" {
		t.Fatalf("CanonicalURL() = %q", got)
	}
}

func TestExtractPageSkipsStaticAssetLinks(t *testing.T) {
	page, links, err := extractPage([]byte(`<html><body><main><p>Docs</p><a href="guide.html">Guide</a><a href="_images/logo.png">Logo</a></main></body></html>`), "https://docs.example.com/docs/", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Content == "" || len(links) != 1 || links[0] != "https://docs.example.com/docs/guide.html" {
		t.Fatalf("links = %#v", links)
	}
}

func TestCrawlTreatsPrefixSeedAsDirectory(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\n"))
		case "/docs", "/docs/":
			_, _ = w.Write([]byte(`<html><body><main><h1>Docs</h1><a href="guide.html">Guide</a></main></body></html>`))
		case "/docs/guide.html":
			_, _ = w.Write([]byte(`<html><body><main><h1>Guide</h1><p>Content</p></main></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	pages, failures, err := NewConnector().Crawl(context.Background(), &types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls": []string{server.URL + "/docs"}, "path_prefixes": []string{"/docs"}, "respect_robots": true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 || len(pages) != 2 {
		t.Fatalf("Crawl() pages=%d failures=%d", len(pages), len(failures))
	}
}

func TestParseConfigUsesSeedDirectoryAsDefaultScope(t *testing.T) {
	config, err := ParseConfig(&types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls": []string{"https://docs.sifli.com/projects/sdk/latest/sf32lb52x/quickstart/index.html"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(config.AllowedHosts) != 1 || config.AllowedHosts[0] != "docs.sifli.com" {
		t.Fatalf("AllowedHosts = %#v", config.AllowedHosts)
	}
	if len(config.PathPrefixes) != 1 || config.PathPrefixes[0] != "/projects/sdk/latest/sf32lb52x/quickstart" {
		t.Fatalf("PathPrefixes = %#v", config.PathPrefixes)
	}
	if config.MaxPages != defaultMaxPages || !config.RespectRobots {
		t.Fatalf("defaults = max_pages=%d respect_robots=%t", config.MaxPages, config.RespectRobots)
	}
}

func TestParseConfigRejectsInvalidContentSelector(t *testing.T) {
	_, err := ParseConfig(&types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls":            []string{"https://docs.example.com/guide/index.html"},
		"web_content_selector": "[",
	}})
	if err == nil {
		t.Fatal("ParseConfig() accepted an invalid content CSS selector")
	}
}

func TestCrawlDiscoversPagesWithinScope(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private"))
		case "/index.html":
			_, _ = w.Write([]byte(`<html><title>Index</title><body><main><h1>Index</h1><p>Hello</p><a href="/guide.html">Guide</a><a href="/private.html">Private</a></main></body></html>`))
		case "/guide.html":
			_, _ = w.Write([]byte(`<html><body><main><h1>Guide</h1><p>World</p></main></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := &types.DataSourceConfig{Settings: map[string]interface{}{"seed_urls": []interface{}{server.URL + "/index.html"}, "respect_robots": true, "max_pages": 10}}
	pages, failures, err := NewConnector().Crawl(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 || len(pages) != 2 {
		t.Fatalf("Crawl() pages=%d failures=%d", len(pages), len(failures))
	}
}

func TestCrawlUsesConfiguredContentSelectors(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\n"))
		case "/index.html":
			_, _ = w.Write([]byte(`<html><title>Selector test</title><body><nav>Navigation</nav><div id="wanted"><p>Keep this text</p><div class="remove">Discard this text</div></div><div id="other">Do not import this text</div></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	pages, failures, err := NewConnector().Crawl(context.Background(), &types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls":             []string{server.URL + "/index.html"},
		"web_content_selector":  "#wanted",
		"web_exclude_selectors": ".remove",
		"respect_robots":        true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 || len(pages) != 1 {
		t.Fatalf("Crawl() pages=%d failures=%d", len(pages), len(failures))
	}
	if pages[0].Content != "Keep this text" {
		t.Fatalf("Content = %q, want selected content only", pages[0].Content)
	}
}
