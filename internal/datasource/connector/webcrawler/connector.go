package webcrawler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"codeberg.org/readeck/go-readability/v2"
	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/PuerkitoBio/goquery"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/andybalholm/cascadia"
)

const (
	defaultMaxPages = 500
	maxAllowedPages = 5000
	maxBodyBytes    = 5 * 1024 * 1024
	defaultTimeout  = 30 * time.Second
	userAgent       = "WeKnora-WebCrawler/1.0 (+https://weknora.ai)"
)

var _ datasource.Connector = (*Connector)(nil)

// Page is a crawled, normalized HTML document ready for review or ingestion.
type Page struct {
	CanonicalURL   string
	Title          string
	Content        string
	ContentHash    string
	DiscoveredFrom string
	StatusCode     int
	ETag           string
	LastModified   string
}

// PageError keeps a failed URL visible in the review result rather than
// failing an entire scan.
type PageError struct {
	URL          string
	SourceStatus int
	Err          error
}

func (e *PageError) Error() string {
	if e == nil {
		return "web crawl page failed"
	}
	return fmt.Sprintf("%s: %v", e.URL, e.Err)
}

type Config struct {
	SeedURLs         []string
	AllowedHosts     []string
	PathPrefixes     []string
	ExcludePatterns  []string
	ContentSelector  string
	ExcludeSelectors []string
	MaxPages         int
	RespectRobots    bool
}

type Connector struct{}

func NewConnector() *Connector { return &Connector{} }

func (c *Connector) Type() string { return types.ConnectorTypeWebCrawler }

func (c *Connector) Validate(ctx context.Context, config *types.DataSourceConfig) error {
	cfg, err := ParseConfig(config)
	if err != nil {
		return err
	}
	if len(cfg.SeedURLs) == 0 {
		return fmt.Errorf("at least one seed URL is required")
	}
	for _, seed := range cfg.SeedURLs {
		if err := utils.ValidateURLForSSRF(seed); err != nil {
			return fmt.Errorf("seed URL %s is not allowed: %w", seed, err)
		}
	}
	return nil
}

func (c *Connector) ResolveResourceAncestors(context.Context, *types.DataSourceConfig, []string) ([]string, error) {
	return []string{}, nil
}

func (c *Connector) ListResources(ctx context.Context, config *types.DataSourceConfig, parentID string) ([]types.Resource, error) {
	if parentID != "" {
		return []types.Resource{}, nil
	}
	pages, failures, err := c.Crawl(ctx, config)
	if err != nil {
		return nil, err
	}
	resources := make([]types.Resource, 0, len(pages)+len(failures))
	for _, p := range pages {
		resources = append(resources, types.Resource{ExternalID: p.CanonicalURL, Name: p.Title, Type: "web_page", URL: p.CanonicalURL, Metadata: map[string]interface{}{"content_hash": p.ContentHash}})
	}
	for _, failure := range failures {
		resources = append(resources, types.Resource{ExternalID: failure.URL, Name: failure.URL, Type: "web_page", URL: failure.URL, Description: failure.Error()})
	}
	return resources, nil
}

func (c *Connector) FetchAll(ctx context.Context, config *types.DataSourceConfig, resourceIDs []string) ([]types.FetchedItem, error) {
	pages, failures, err := c.Crawl(ctx, config)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(resourceIDs))
	for _, id := range resourceIDs {
		selected[CanonicalURL(id)] = struct{}{}
	}
	items := make([]types.FetchedItem, 0, len(pages))
	for _, p := range pages {
		if len(selected) > 0 {
			if _, ok := selected[p.CanonicalURL]; !ok {
				continue
			}
		}
		items = append(items, types.FetchedItem{ExternalID: p.CanonicalURL, Title: p.Title, Content: []byte(p.Content), ContentType: "text/markdown", FileName: safeFileName(p.Title) + ".md", URL: p.CanonicalURL, UpdatedAt: time.Now().UTC(), Metadata: map[string]string{"channel": types.ChannelWeb, "content_hash": p.ContentHash, "status_code": fmt.Sprintf("%d", p.StatusCode)}})
	}
	if len(failures) > 0 {
		details := make([]string, 0, len(failures))
		for _, failure := range failures {
			details = append(details, failure.Error())
		}
		return items, &datasource.PartialFetchError{Details: details}
	}
	return items, nil
}

func (c *Connector) FetchIncremental(ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error) {
	items, err := c.FetchAll(ctx, config, config.ResourceIDs)
	return items, &types.SyncCursor{LastSyncTime: time.Now().UTC()}, err
}

// ParseConfig decodes settings and applies conservative crawl defaults.
func ParseConfig(config *types.DataSourceConfig) (Config, error) {
	if config == nil {
		return Config{}, fmt.Errorf("data source config is nil")
	}
	s := config.Settings
	cfg := Config{
		SeedURLs:         stringSlice(s["seed_urls"]),
		AllowedHosts:     stringSlice(s["allowed_hosts"]),
		PathPrefixes:     stringSlice(s["path_prefixes"]),
		ExcludePatterns:  stringSlice(s["exclude_patterns"]),
		ContentSelector:  stringValue(s["web_content_selector"]),
		ExcludeSelectors: stringSlice(s["web_exclude_selectors"]),
		MaxPages:         intValue(s["max_pages"], defaultMaxPages),
		RespectRobots:    boolValue(s["respect_robots"], true),
	}
	for i, seed := range cfg.SeedURLs {
		cfg.SeedURLs[i] = CanonicalURL(seed)
	}
	cfg.SeedURLs = uniqueSorted(cfg.SeedURLs)
	if len(cfg.SeedURLs) == 0 {
		return Config{}, fmt.Errorf("settings.seed_urls must contain at least one URL")
	}
	if len(cfg.AllowedHosts) == 0 {
		for _, seed := range cfg.SeedURLs {
			if parsed, err := url.Parse(seed); err == nil {
				cfg.AllowedHosts = append(cfg.AllowedHosts, strings.ToLower(parsed.Hostname()))
			}
		}
	}
	for i, host := range cfg.AllowedHosts {
		cfg.AllowedHosts[i] = strings.ToLower(strings.TrimSpace(host))
	}
	if len(cfg.PathPrefixes) == 0 {
		for _, seed := range cfg.SeedURLs {
			if parsed, err := url.Parse(seed); err == nil {
				prefix := path.Dir(parsed.EscapedPath())
				if prefix == "." || prefix == "/" {
					prefix = "/"
				}
				cfg.PathPrefixes = append(cfg.PathPrefixes, prefix)
			}
		}
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = defaultMaxPages
	}
	if cfg.MaxPages > maxAllowedPages {
		cfg.MaxPages = maxAllowedPages
	}
	for _, pattern := range cfg.ExcludePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return Config{}, fmt.Errorf("invalid exclude pattern %q: %w", pattern, err)
		}
	}
	if cfg.ContentSelector != "" {
		if _, err := cascadia.Compile(cfg.ContentSelector); err != nil {
			return Config{}, fmt.Errorf("invalid content CSS selector %q: %w", cfg.ContentSelector, err)
		}
	}
	for _, selector := range cfg.ExcludeSelectors {
		if _, err := cascadia.Compile(selector); err != nil {
			return Config{}, fmt.Errorf("invalid exclude CSS selector %q: %w", selector, err)
		}
	}
	return cfg, nil
}

// Crawl performs a bounded breadth-first traversal in one goroutine. The
// worker that invokes it is already asynchronous, while sequential requests
// keep rate limiting and memory behavior predictable.
func (c *Connector) Crawl(ctx context.Context, config *types.DataSourceConfig) ([]Page, []*PageError, error) {
	cfg, err := ParseConfig(config)
	if err != nil {
		return nil, nil, err
	}
	if err := c.Validate(ctx, config); err != nil {
		return nil, nil, err
	}
	client := datasource.NewConnectorHTTPClient(defaultTimeout)
	queue := make([]string, 0, len(cfg.SeedURLs))
	for _, seed := range cfg.SeedURLs {
		queue = append(queue, seed)
	}
	visited := make(map[string]struct{})
	robots := make(map[string]*robotsRules)
	pages := make([]Page, 0, minInt(cfg.MaxPages, len(queue)))
	failures := make([]*PageError, 0)
	for len(queue) > 0 && len(visited) < cfg.MaxPages {
		select {
		case <-ctx.Done():
			return pages, failures, ctx.Err()
		default:
		}
		raw := queue[0]
		queue = queue[1:]
		canonical := CanonicalURL(raw)
		if canonical == "" {
			continue
		}
		if _, ok := visited[canonical]; ok || !AllowedURL(canonical, cfg) {
			continue
		}
		visited[canonical] = struct{}{}
		if cfg.RespectRobots {
			allowed, rulesErr := allowedByRobots(ctx, client, canonical, robots)
			if rulesErr != nil {
				failures = append(failures, &PageError{URL: canonical, Err: rulesErr})
				continue
			}
			if !allowed {
				continue
			}
		}
		body, status, headers, fetchErr := fetchBody(ctx, client, canonical, true)
		if fetchErr != nil {
			failures = append(failures, &PageError{URL: canonical, SourceStatus: status, Err: fetchErr})
			continue
		}
		page, links, extractErr := extractPage(body, canonical, cfg)
		if extractErr != nil {
			failures = append(failures, &PageError{URL: canonical, SourceStatus: status, Err: extractErr})
			continue
		}
		page.StatusCode = status
		page.ETag = headers.Get("ETag")
		page.LastModified = headers.Get("Last-Modified")
		for _, link := range links {
			if _, seen := visited[link]; !seen && AllowedURL(link, cfg) {
				queue = append(queue, link)
			}
		}
		pages = append(pages, page)
	}
	return pages, failures, nil
}

func fetchBody(ctx context.Context, client *http.Client, rawURL string, requireHTML bool) ([]byte, int, http.Header, error) {
	if err := utils.ValidateURLForSSRF(rawURL); err != nil {
		return nil, 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	if requireHTML {
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml") {
			return nil, resp.StatusCode, resp.Header, fmt.Errorf("unsupported content type %q", contentType)
		}
	}
	limited := io.LimitReader(resp.Body, maxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	if len(body) > maxBodyBytes {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("response exceeds %d bytes", maxBodyBytes)
	}
	return body, resp.StatusCode, resp.Header, nil
}

func extractPage(body []byte, pageURL string, cfg Config) (Page, []string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Page{}, nil, err
	}
	links := make([]string, 0)
	base, _ := url.Parse(pageURL)
	// CanonicalURL intentionally removes a trailing slash. When the seed is a
	// configured path prefix, restore directory semantics for relative links.
	if base != nil {
		for _, prefix := range cfg.PathPrefixes {
			prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
			if prefix != "" && strings.TrimRight(base.EscapedPath(), "/") == prefix {
				base.Path = strings.TrimRight(base.Path, "/") + "/"
				base.RawPath = ""
				break
			}
		}
	}
	doc.Find("a[href]").Each(func(_ int, selection *goquery.Selection) {
		href, ok := selection.Attr("href")
		if !ok || strings.HasPrefix(strings.TrimSpace(href), "#") {
			return
		}
		parsed, err := url.Parse(strings.TrimSpace(href))
		if err != nil {
			return
		}
		resolved := base.ResolveReference(parsed)
		canonical := CanonicalURL(resolved.String())
		if canonical != "" && isCrawlableDocumentURL(resolved) && (resolved.Scheme == "http" || resolved.Scheme == "https") {
			links = append(links, canonical)
		}
	})
	links = uniqueSorted(links)
	title := strings.TrimSpace(doc.Find("h1").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").First().Text())
	}
	contentDoc := doc.Find(cfg.ContentSelector).First()
	if cfg.ContentSelector != "" && contentDoc.Length() == 0 {
		return Page{}, links, fmt.Errorf("content CSS selector %q did not match the page", cfg.ContentSelector)
	}
	if contentDoc.Length() == 0 {
		contentDoc = doc.Find("main").First()
	}
	if contentDoc.Length() == 0 {
		contentDoc = doc.Find("article").First()
	}
	if contentDoc.Length() == 0 {
		contentDoc = doc.Find(`[role="main"]`).First()
	}
	if contentDoc.Length() == 0 {
		contentDoc = doc.Find("body").First()
	}
	if contentDoc.Length() == 0 {
		return Page{}, links, fmt.Errorf("page has no readable body")
	}
	contentDoc.Find("script,style,noscript,nav,header,footer,aside,form").Remove()
	for _, selector := range cfg.ExcludeSelectors {
		contentDoc.Find(selector).Remove()
	}
	html, err := contentDoc.Html()
	if err != nil {
		return Page{}, links, err
	}
	markdown, err := htmltomd.ConvertString(html)
	if err != nil {
		reader, readErr := readability.FromReader(strings.NewReader(string(body)), base)
		if readErr != nil {
			return Page{}, links, err
		}
		var readableHTML bytes.Buffer
		if renderErr := reader.RenderHTML(&readableHTML); renderErr != nil {
			return Page{}, links, renderErr
		}
		markdown, err = htmltomd.ConvertString(readableHTML.String())
		if err != nil {
			return Page{}, links, err
		}
	}
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return Page{}, links, fmt.Errorf("page content is empty")
	}
	hash := sha256.Sum256([]byte(markdown))
	return Page{CanonicalURL: pageURL, Title: title, Content: markdown, ContentHash: hex.EncodeToString(hash[:])}, links, nil
}

func isCrawlableDocumentURL(u *url.URL) bool {
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".css", ".js", ".map", ".woff", ".woff2", ".ttf", ".eot", ".pdf", ".zip", ".gz", ".tar", ".mp3", ".mp4", ".webm":
		return false
	default:
		return true
	}
}

func CanonicalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.Path = path.Clean(u.Path)
	u.RawPath = ""
	if u.Path == "." || u.Path == "" {
		u.Path = "/"
	}
	query := u.Query()
	for key := range query {
		if strings.HasPrefix(strings.ToLower(key), "utm_") || strings.EqualFold(key, "fbclid") {
			query.Del(key)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func AllowedURL(raw string, cfg Config) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	allowedHost := false
	for _, candidate := range cfg.AllowedHosts {
		if host == strings.ToLower(strings.TrimSpace(candidate)) {
			allowedHost = true
			break
		}
	}
	if !allowedHost {
		return false
	}
	p := u.EscapedPath()
	for _, prefix := range cfg.PathPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || prefix == "/" || p == prefix || strings.HasPrefix(p, strings.TrimRight(prefix, "/")+"/") {
			blocked := false
			for _, pattern := range cfg.ExcludePatterns {
				if ok, _ := regexp.MatchString(pattern, raw); ok {
					blocked = true
					break
				}
			}
			return !blocked
		}
	}
	return false
}

type robotsRules struct{ disallow []string }

func allowedByRobots(ctx context.Context, client *http.Client, rawURL string, cache map[string]*robotsRules) (bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, err
	}
	host := strings.ToLower(u.Host)
	rules, ok := cache[host]
	if !ok {
		robotsURL := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/robots.txt"}).String()
		body, status, _, fetchErr := fetchBody(ctx, client, robotsURL, false)
		if fetchErr != nil {
			if status == http.StatusNotFound || status == http.StatusForbidden {
				rules = &robotsRules{}
			} else {
				return false, fetchErr
			}
		} else {
			rules = parseRobots(body)
		}
		cache[host] = rules
	}
	for _, prefix := range rules.disallow {
		if prefix != "" && strings.HasPrefix(u.EscapedPath(), prefix) {
			return false, nil
		}
	}
	return true, nil
}

func parseRobots(body []byte) *robotsRules {
	rules := &robotsRules{}
	active := false
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
		switch key {
		case "user-agent":
			active = value == "*"
		case "disallow":
			if active && value != "" {
				rules.disallow = append(rules.disallow, value)
			}
		}
	}
	return rules
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func safeFileName(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "web-page"
	}
	title = regexp.MustCompile(`[^\pL\pN._-]+`).ReplaceAllString(title, "-")
	title = strings.Trim(title, "-.")
	if title == "" {
		title = "web-page"
	}
	if len(title) > 100 {
		title = title[:100]
	}
	return title
}

func stringSlice(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	case string:
		parts := strings.FieldsFunc(typed, func(r rune) bool { return r == '\n' || r == ',' })
		return parts
	default:
		return nil
	}
}

func stringValue(value interface{}) string {
	if value, ok := value.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func intValue(value interface{}, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var result int
		if _, err := fmt.Sscanf(typed, "%d", &result); err == nil {
			return result
		}
	}
	return fallback
}

func boolValue(value interface{}, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	if typed, ok := value.(string); ok {
		return strings.EqualFold(typed, "true") || (typed != "" && !strings.EqualFold(typed, "false"))
	}
	return fallback
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
