// Package webtitle reads a display title from a public HTML page without
// altering its stored content, chunks, or embeddings.
package webtitle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

const maxHTMLTitleBytes = 512 * 1024

// Fetch returns the same user-facing title a browser would normally expose:
// the HTML title first, then Open Graph metadata, then the first H1. It
// intentionally reads only a bounded HTML response and does not parse or
// persist document content.
func Fetch(ctx context.Context, rawURL string) (string, error) {
	if err := secutils.ValidateURLForSSRF(rawURL); err != nil {
		return "", fmt.Errorf("validate URL: %w", err)
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return secutils.ValidateURLForSSRF(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "WeKnora-TitleRefresh/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch page: unexpected status %s", resp.Status)
	}
	if contentType := strings.ToLower(resp.Header.Get("Content-Type")); contentType != "" && !strings.Contains(contentType, "html") {
		return "", fmt.Errorf("fetch page: unsupported content type %q", contentType)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxHTMLTitleBytes))
	if err != nil {
		return "", fmt.Errorf("parse HTML: %w", err)
	}
	if title := selectTitle(doc); title != "" {
		return title, nil
	}
	return "", fmt.Errorf("page has no usable title")
}

func selectTitle(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	for _, value := range []string{
		doc.Find("title").First().Text(),
		firstAttr(doc, `meta[property="og:title"]`, "content"),
		firstAttr(doc, `meta[name="og:title"]`, "content"),
		doc.Find("h1").First().Text(),
	} {
		if title := normalize(value); title != "" {
			return title
		}
	}
	return ""
}

func firstAttr(doc *goquery.Document, selector, name string) string {
	value, _ := doc.Find(selector).First().Attr(name)
	return value
}

func normalize(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
