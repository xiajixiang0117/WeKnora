package webcrawler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

func TestAssignFolderPathsUsesDirectoryIndexTitles(t *testing.T) {
	pages := []Page{
		{CanonicalURL: "https://docs.example.com/root/", Title: "SDK"},
		{CanonicalURL: "https://docs.example.com/root/quickstart/index.html", Title: "快速入门"},
		{CanonicalURL: "https://docs.example.com/root/quickstart/install/index.html", Title: "安装环境"},
		{CanonicalURL: "https://docs.example.com/root/quickstart/install/script/index.html", Title: "脚本安装"},
		{CanonicalURL: "https://docs.example.com/root/quickstart/install/script/windows.html", Title: "Windows 安装流程"},
	}
	assignFolderPaths(pages, Config{PathPrefixes: []string{"/root"}})
	if pages[0].FolderPath != "" {
		t.Fatalf("root page folder = %q, want empty", pages[0].FolderPath)
	}
	if pages[4].FolderPath != "快速入门/安装环境/脚本安装" {
		t.Fatalf("nested page folder = %q", pages[4].FolderPath)
	}
}

func TestAssignFolderPathsWithRootScope(t *testing.T) {
	pages := []Page{
		{CanonicalURL: "https://docs.example.com/quickstart/index.html", Title: "快速入门"},
		{CanonicalURL: "https://docs.example.com/quickstart/install/windows.html", Title: "Windows 安装流程"},
	}
	assignFolderPaths(pages, Config{PathPrefixes: []string{"/"}})
	if pages[1].FolderPath != "快速入门/install" {
		t.Fatalf("root-scope folder = %q", pages[1].FolderPath)
	}
}

func TestAssignFolderPathsUsesSeedRootInsteadOfBroaderCrawlScope(t *testing.T) {
	pages := []Page{
		{CanonicalURL: "https://docs.example.com/projects/sdk/latest/sf32lb52x/", Title: "SDK"},
		{CanonicalURL: "https://docs.example.com/projects/sdk/latest/sf32lb52x/quickstart/index.html", Title: "快速入门"},
		{CanonicalURL: "https://docs.example.com/projects/sdk/latest/sf32lb52x/quickstart/windows.html", Title: "Windows 安装流程"},
	}
	assignFolderPaths(pages, Config{
		SeedURLs:     []string{"https://docs.example.com/projects/sdk/latest/sf32lb52x/"},
		PathPrefixes: []string{"/"},
	})
	if pages[2].FolderPath != "快速入门" {
		t.Fatalf("folder = %q, want a path relative to the seed URL", pages[2].FolderPath)
	}
}

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

func TestExtractPageResolvesRelativeImageSources(t *testing.T) {
	page, _, err := extractPage([]byte(`<html><body><main><p>Guide</p><img src="../_images/app_folder.png" alt="Application folder"></main></body></html>`), "https://docs.sifli.com/projects/sdk/latest/sf32lb56x/quickstart/index.html", Config{})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://docs.sifli.com/projects/sdk/latest/sf32lb56x/_images/app_folder.png"
	if !strings.Contains(page.Content, want) {
		t.Fatalf("Content = %q, want absolute image URL %q", page.Content, want)
	}
}

func TestExtractPageResolvesImagesRelativeToPrefixSeedDirectory(t *testing.T) {
	page, _, err := extractPage([]byte(`<html><body><main><p>Docs</p><img src="img.png"></main></body></html>`), "https://docs.example.com/docs", Config{PathPrefixes: []string{"/docs"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://docs.example.com/docs/img.png"
	if !strings.Contains(page.Content, want) {
		t.Fatalf("Content = %q, want image URL relative to seed directory %q", page.Content, want)
	}
}

func TestExtractPageResolvesNavigableLinksBeforeMarkdownConversion(t *testing.T) {
	pageURL := "https://docs.sifli.com/projects/sdk/latest/sf32lb52x/audio/mic.html"
	page, _, err := extractPage([]byte(`<html><body><main>
		<a href="../tools/SiFli_EQ/SiFli_EQ_UM.html">SiFli_EQ</a>
		<a href="/projects/sdk/latest/sf32lb52x/app_development/create_board.html?tab=quickstart#install">创建板子</a>
		<a href="#gain">本页增益说明</a>
		<a href="https://example.org/guide">外部指南</a>
		<a href="mailto:help@example.org">联系支持</a>
		<a href="javascript:alert(1)">不安全链接</a>
	</main></body></html>`), pageURL, Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://docs.sifli.com/projects/sdk/latest/sf32lb52x/tools/SiFli_EQ/SiFli_EQ_UM.html",
		"https://docs.sifli.com/projects/sdk/latest/sf32lb52x/app_development/create_board.html?tab=quickstart#install",
		pageURL + "#gain",
		"https://example.org/guide",
		"mailto:help@example.org",
		"javascript:alert",
	} {
		if !strings.Contains(page.Content, want) {
			t.Fatalf("Content = %q, want link URL %q", page.Content, want)
		}
	}
	if strings.Contains(page.Content, "](../tools/") || strings.Contains(page.Content, "](#gain)") {
		t.Fatalf("Content retained a relative link: %q", page.Content)
	}
}

func TestExtractPageResolvesLinksUsingHTMLBaseHref(t *testing.T) {
	page, _, err := extractPage([]byte(`<html><head><base href="https://docs.example.com/reference/v2/"></head><body><main><a href="api.html">API</a></main></body></html>`), "https://docs.example.com/guides/start.html", Config{})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://docs.example.com/reference/v2/api.html"
	if !strings.Contains(page.Content, want) {
		t.Fatalf("Content = %q, want link URL %q", page.Content, want)
	}
}

func TestExtractPageCleansPrivateUseCharactersFromTitle(t *testing.T) {
	page, _, err := extractPage([]byte(`<html><head><title>Fallback title&#xf0c1;</title></head><body><main><h1>SiFli-SDK编程指南<span>&#xf0c1;</span></h1><p>Content</p></main></body></html>`), "https://docs.example.com/docs/", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "SiFli-SDK编程指南" {
		t.Fatalf("Title = %q, want private-use anchor icon removed", page.Title)
	}
}

func TestExtractPageCleansPrivateUseCharactersFromContent(t *testing.T) {
	page, _, err := extractPage([]byte(`<html><body><main><h1>register.h</h1><p>__CM33_REV<span>&#xf0c1;</span></p><p>__SAUREGION_PRESENT<span>&#xf0c1;</span></p></main></body></html>`), "https://docs.example.com/docs/register.html", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page.Content, "\uf0c1") {
		t.Fatalf("Content retained a private-use icon: %q", page.Content)
	}
	if !strings.Contains(page.Content, "CM33") || !strings.Contains(page.Content, "SAUREGION") {
		t.Fatalf("Content lost document text: %q", page.Content)
	}
}

func TestExtractPageRemovesHeadingAnchorLinksFromMarkdown(t *testing.T) {
	page, _, err := extractPage([]byte(`<html><body><main><h1>SiFli-SDK编程指南<a class="headerlink" href="#sifli-sdk" title="Link to this heading">&#xf0c1;</a></h1><p>Content</p></main></body></html>`), "https://docs.example.com/docs/", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page.Content, "Link to this heading") || strings.Contains(page.Content, "[\uf0c1]") {
		t.Fatalf("Content retained a heading anchor: %q", page.Content)
	}
	if !strings.Contains(page.Content, "SiFli-SDK编程指南") {
		t.Fatalf("Content lost the heading text: %q", page.Content)
	}
}

func TestExtractPageRemovesSphinxPilcrowAnchorFromTitleAndContent(t *testing.T) {
	page, _, err := extractPage([]byte(`<html><body><main><h1>SiFli Solution介绍<a class="headerlink" href="#sifli-solution" title="Link to this heading">¶</a></h1><p>Solution 正文</p></main></body></html>`), "https://docs.sifli.com/projects/solution/0.introduction/index.html", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "SiFli Solution介绍" {
		t.Fatalf("Title = %q, want Sphinx pilcrow anchor removed", page.Title)
	}
	if strings.Contains(page.Content, "¶") {
		t.Fatalf("Content retained Sphinx pilcrow anchor: %q", page.Content)
	}
}

func TestParseConfigKeepsDirectorySeedAsDefaultPathPrefix(t *testing.T) {
	cfg, err := ParseConfig(&types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls": []string{"https://docs.example.com/projects/sdk/latest/sf32lb52x/"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PathPrefixes) != 1 || cfg.PathPrefixes[0] != "/projects/sdk/latest/sf32lb52x" {
		t.Fatalf("PathPrefixes = %#v, want the seed directory", cfg.PathPrefixes)
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

func TestFetchPagesRechecksHistoricalURLsWithoutDiscoveryLimit(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var mu sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/docs/hal/dsi.html":
			http.NotFound(w, r)
		case "/docs/gone.html":
			w.WriteHeader(http.StatusGone)
		case "/docs/unavailable.html":
			w.WriteHeader(http.StatusInternalServerError)
		case "/docs/quickstart/index.html":
			_, _ = w.Write([]byte(`<html><body><div id="wanted"><h1>快速入门</h1><p>Index</p></div></body></html>`))
		case "/docs/quickstart/guide.html":
			w.Header().Set("ETag", `"guide-v1"`)
			w.Header().Set("Last-Modified", "Wed, 16 Sep 2026 00:00:00 GMT")
			_, _ = w.Write([]byte(`<html><body><h1>Guide</h1><div id="wanted"><p>Historical content</p><div class="remove">Discard</div><a href="../not-requested.html">Link</a></div><p>Outside selector</p></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	urls := []string{
		server.URL + "/docs/hal/dsi.html",
		server.URL + "/docs/gone.html",
		server.URL + "/docs/unavailable.html",
		server.URL + "/docs/quickstart/index.html",
		server.URL + "/docs/quickstart/guide.html",
	}
	pages, failures, err := NewConnector().FetchPages(context.Background(), &types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls":             []string{server.URL + "/docs/index.html"},
		"respect_robots":        false,
		"max_pages":             1,
		"web_content_selector":  "#wanted",
		"web_exclude_selectors": []string{".remove"},
	}}, urls)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || len(failures) != 3 {
		t.Fatalf("FetchPages() pages=%d failures=%d, want 2 pages and 3 failures beyond max_pages=1", len(pages), len(failures))
	}
	for i, status := range []int{http.StatusNotFound, http.StatusGone, http.StatusInternalServerError} {
		if failures[i].URL != urls[i] || failures[i].SourceStatus != status {
			t.Fatalf("failure[%d] = %#v, want URL %q status %d", i, failures[i], urls[i], status)
		}
	}
	guide := pages[1]
	if guide.CanonicalURL != urls[4] || guide.Title != "Guide" || guide.StatusCode != http.StatusOK || guide.ContentHash == "" {
		t.Fatalf("historical page metadata = %#v", guide)
	}
	if guide.FolderPath != "快速入门" || guide.ETag != `"guide-v1"` || guide.LastModified != "Wed, 16 Sep 2026 00:00:00 GMT" {
		t.Fatalf("historical page folder/HTTP metadata = %#v", guide)
	}
	if !strings.Contains(guide.Content, "Historical content") || strings.Contains(guide.Content, "Discard") || strings.Contains(guide.Content, "Outside selector") {
		t.Fatalf("historical Content = %q, want configured extraction", guide.Content)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != len(urls) {
		t.Fatalf("requests = %#v, want only explicitly supplied URLs", requests)
	}
	for _, raw := range urls {
		if got := requests[strings.TrimPrefix(raw, server.URL)]; got != 1 {
			t.Fatalf("requests for %q = %d, want 1", raw, got)
		}
	}
}

func TestFetchPagesRespectsScopeRobotsAndCanonicalDeduplication(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var mu sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		if r.URL.Path == "/robots.txt" {
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /docs/blocked.html\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><main><h1>Allowed</h1><p>Content</p></main></body></html>`))
	}))
	defer server.Close()
	pages, failures, err := NewConnector().FetchPages(context.Background(), &types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls":        []string{server.URL + "/docs/index.html"},
		"respect_robots":   true,
		"exclude_patterns": []string{`/excluded\.html$`},
	}}, []string{
		server.URL + "/docs/allowed.html",
		server.URL + "/docs/allowed.html?utm_source=duplicate#part",
		server.URL + "/docs/blocked.html",
		server.URL + "/docs/blocked.html#duplicate",
		server.URL + "/outside.html",
		server.URL + "/docs/excluded.html",
		"https://outside.invalid/docs/page.html",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || len(failures) != 1 {
		t.Fatalf("FetchPages() pages=%d failures=%d, want one page and one robots failure", len(pages), len(failures))
	}
	if failure := failures[0]; failure.URL != server.URL+"/docs/blocked.html" || failure.SourceStatus != 0 || failure.Err.Error() != "blocked by robots.txt" {
		t.Fatalf("robots failure = %#v", failure)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests["/robots.txt"] != 1 || requests["/docs/allowed.html"] != 1 {
		t.Fatalf("requests = %#v, want one robots request and one allowed page request", requests)
	}
}

func TestFetchPagesPreservesDirectAndRedirectSSRFProtection(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer server.Close()
	pages, failures, err := NewConnector().FetchPages(context.Background(), &types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls":      []string{server.URL + "/index.html"},
		"allowed_hosts":  []string{"127.0.0.1", "169.254.169.254"},
		"respect_robots": false,
	}}, []string{server.URL + "/redirect.html", "http://169.254.169.254/latest/meta-data"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 0 || len(failures) != 2 {
		t.Fatalf("FetchPages() pages=%d failures=%d, want both SSRF attempts blocked", len(pages), len(failures))
	}
	for _, failure := range failures {
		if failure.SourceStatus != 0 || !strings.Contains(failure.Error(), "SSRF validation failed") {
			t.Fatalf("SSRF failure = %v (status %d)", failure, failure.SourceStatus)
		}
	}
}

func TestFetchPagesStopsWhenContextIsCanceled(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewConnector().FetchPages(ctx, &types.DataSourceConfig{Settings: map[string]interface{}{
		"seed_urls":      []string{"http://127.0.0.1/docs/index.html"},
		"respect_robots": false,
	}}, []string{"http://127.0.0.1/docs/old.html"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchPages() error = %v, want context.Canceled", err)
	}
}
