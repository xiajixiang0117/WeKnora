package remotefile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

func TestURLs(t *testing.T) {
	config := &types.DataSourceConfig{Settings: map[string]interface{}{"file_urls": "https://example.com/a.pdf\nhttps://example.com/a.pdf, https://example.com/b.pdf"}}
	list, err := urls(config)
	if err != nil || len(list) != 2 {
		t.Fatalf("urls = %v, %v", list, err)
	}
	config.Settings["file_urls"] = "http://127.0.0.1/private.pdf"
	if _, err := urls(config); err == nil {
		t.Fatal("expected SSRF rejection")
	}
}

func TestIncrementalContentHash(t *testing.T) {
	// The SSRF whitelist is applied only to this test's loopback server.
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	body := "first"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	u := server.URL + "/document.pdf"
	config := &types.DataSourceConfig{Settings: map[string]interface{}{"file_urls": u}}
	connector := NewConnector()
	items, cursor, err := connector.FetchIncremental(context.Background(), config, nil)
	if err != nil || len(items) != 1 || items[0].FileName != "report.pdf" || string(items[0].Content) != "first" {
		t.Fatalf("first: %+v, %v", items, err)
	}
	items, cursor, err = connector.FetchIncremental(context.Background(), config, cursor)
	if err != nil || len(items) != 0 {
		t.Fatalf("unchanged: %+v, %v", items, err)
	}
	body = "second"
	items, _, err = connector.FetchIncremental(context.Background(), config, cursor)
	if err != nil || len(items) != 1 || string(items[0].Content) != "second" {
		t.Fatalf("changed: %+v, %v", items, err)
	}
	if !strings.HasSuffix(items[0].ContentType, "pdf") {
		t.Fatalf("content type: %s", items[0].ContentType)
	}
}
