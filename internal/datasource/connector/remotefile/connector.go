package remotefile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

const maxFileBytes = 100 << 20

var client = utils.NewSSRFSafeHTTPClient(utils.SSRFSafeHTTPClientConfig{Timeout: 90 * time.Second, MaxRedirects: 5})

type Connector struct{}

var _ datasource.Connector = (*Connector)(nil)

func NewConnector() *Connector  { return &Connector{} }
func (*Connector) Type() string { return types.ConnectorTypeRemoteFile }

func urls(config *types.DataSourceConfig) ([]string, error) {
	if config == nil {
		return nil, fmt.Errorf("%w: missing config", datasource.ErrInvalidConfig)
	}
	raw, ok := config.Settings["file_urls"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w: file_urls is required", datasource.ErrInvalidConfig)
	}
	seen := map[string]bool{}
	var result []string
	for _, s := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, fmt.Errorf("invalid file URL: %q", s)
		}
		if err := utils.ValidateURLForSSRF(s); err != nil {
			return nil, fmt.Errorf("file URL %q: %w", s, err)
		}
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: file_urls is required", datasource.ErrInvalidConfig)
	}
	return result, nil
}

func (*Connector) Validate(_ context.Context, config *types.DataSourceConfig) error {
	_, err := urls(config)
	return err
}

func (*Connector) ResolveResourceAncestors(context.Context, *types.DataSourceConfig, []string) ([]string, error) {
	return []string{}, nil
}

func filename(raw string, header http.Header) string {
	if _, params, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		return path.Base(strings.ReplaceAll(params["filename"], "\\", "/"))
	}
	u, _ := url.Parse(raw)
	name := path.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		return "download.pdf"
	}
	return name
}

func (*Connector) ListResources(_ context.Context, config *types.DataSourceConfig, parentID string) ([]types.Resource, error) {
	if parentID != "" {
		return []types.Resource{}, nil
	}
	list, err := urls(config)
	if err != nil {
		return nil, err
	}
	result := make([]types.Resource, 0, len(list))
	for _, u := range list {
		result = append(result, types.Resource{ExternalID: u, Name: filename(u, nil), Type: "file", URL: u})
	}
	return result, nil
}

func fetch(ctx context.Context, raw string) (types.FetchedItem, string, error) {
	if err := utils.ValidateURLForSSRF(raw); err != nil {
		return types.FetchedItem{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return types.FetchedItem{}, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return types.FetchedItem{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return types.FetchedItem{}, "", fmt.Errorf("GET %s: HTTP %d", raw, resp.StatusCode)
	}
	if resp.ContentLength > maxFileBytes {
		return types.FetchedItem{}, "", fmt.Errorf("file exceeds %d bytes: %s", maxFileBytes, raw)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return types.FetchedItem{}, "", err
	}
	if len(data) > maxFileBytes {
		return types.FetchedItem{}, "", fmt.Errorf("file exceeds %d bytes: %s", maxFileBytes, raw)
	}
	name := filename(raw, resp.Header)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	hash := sha256.Sum256(data)
	return types.FetchedItem{ExternalID: raw, SourceResourceID: raw, Title: name, FileName: name, Content: data, ContentType: contentType, URL: raw}, hex.EncodeToString(hash[:]), nil
}

func selected(config *types.DataSourceConfig, ids []string) ([]string, error) {
	all, err := urls(config)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return all, nil
	}
	chosen := map[string]bool{}
	for _, id := range ids {
		chosen[id] = true
	}
	var result []string
	for _, u := range all {
		if chosen[u] {
			result = append(result, u)
		}
	}
	return result, nil
}

func (*Connector) FetchAll(ctx context.Context, config *types.DataSourceConfig, ids []string) ([]types.FetchedItem, error) {
	list, err := selected(config, ids)
	if err != nil {
		return nil, err
	}
	items := make([]types.FetchedItem, 0, len(list))
	for _, u := range list {
		item, _, err := fetch(ctx, u)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (*Connector) FetchIncremental(ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error) {
	list, err := selected(config, config.ResourceIDs)
	if err != nil {
		return nil, nil, err
	}
	previous := map[string]interface{}{}
	if cursor != nil {
		for k, v := range cursor.ConnectorCursor {
			previous[k] = v
		}
	}
	next := map[string]interface{}{}
	items := make([]types.FetchedItem, 0, len(list))
	for _, u := range list {
		item, hash, err := fetch(ctx, u)
		if err != nil {
			return nil, nil, err
		}
		next[u] = hash
		if previous[u] != hash {
			items = append(items, item)
		}
	}
	return items, &types.SyncCursor{LastSyncTime: time.Now(), ConnectorCursor: next}, nil
}
