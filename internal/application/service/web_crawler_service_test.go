package service

import (
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/webcrawler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessWebCrawlScanMarksScanFailedWhenProcessingErrors(t *testing.T) {
	scan := &types.WebCrawlScan{ID: "scan-1", Status: types.WebCrawlScanStatusScanning}
	svc := &DataSourceService{
		dsRepo: &webCrawlTestDataSourceRepo{ds: &types.DataSource{
			ID:     "datasource-1",
			Type:   types.ConnectorTypeWebCrawler,
			Config: types.JSON("not-json"),
		}},
		webCrawlerRepo: &webCrawlTestRepo{scan: scan},
	}

	payload, err := json.Marshal(types.WebCrawlScanPayload{
		DataSourceID: "datasource-1",
		ScanID:       scan.ID,
	})
	require.NoError(t, err)

	err = svc.ProcessWebCrawlScan(context.Background(), asynq.NewTask(types.TypeWebCrawlScan, payload))
	require.Error(t, err)
	assert.Equal(t, types.WebCrawlScanStatusPartialFailed, scan.Status)
	assert.Equal(t, err.Error(), scan.ErrorMessage)
	assert.NotNil(t, scan.FinishedAt)
}

type webCrawlTestDataSourceRepo struct {
	interfaces.DataSourceRepository
	ds *types.DataSource
}

func (r *webCrawlTestDataSourceRepo) FindByID(context.Context, string) (*types.DataSource, error) {
	return r.ds, nil
}

type webCrawlTestRepo struct {
	interfaces.WebCrawlerRepository
	scan *types.WebCrawlScan
}

func (r *webCrawlTestRepo) FindScan(context.Context, string) (*types.WebCrawlScan, error) {
	return r.scan, nil
}

func (r *webCrawlTestRepo) UpdateScan(context.Context, *types.WebCrawlScan) error {
	return nil
}

func TestProcessWebCrawlScanAdoptsAnAppliedKnowledgeWithoutBaseline(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path != "/root" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><body><main><h1>Guide</h1><p>Content</p></main></body></html>`))
	}))
	defer server.Close()

	const (
		dataSourceID  = "datasource-1"
		knowledgeID   = "knowledge-1"
		knowledgeBase = "kb-1"
	)
	canonicalURL := server.URL + "/root"
	ds := &types.DataSource{
		ID:              dataSourceID,
		TenantID:        1,
		KnowledgeBaseID: knowledgeBase,
		Type:            types.ConnectorTypeWebCrawler,
		Config:          types.JSON(`{"settings":{"seed_urls":["` + server.URL + `/root/"],"max_pages":1,"respect_robots":false}}`),
	}
	scan := &types.WebCrawlScan{ID: "scan-1", DataSourceID: dataSourceID, Status: types.WebCrawlScanStatusScanning}
	pages := &webCrawlStateRepo{scan: scan, pages: map[string]*types.WebCrawlPage{}}
	knowledge := &types.Knowledge{ID: knowledgeID, Type: "url", Source: canonicalURL, FolderPath: ""}
	svc := &DataSourceService{
		dsRepo:         &webCrawlTestDataSourceRepo{ds: ds},
		webCrawlerRepo: pages,
		knowledgeService: &webCrawlBaselineKnowledgeService{repo: &webCrawlBaselineKnowledgeRepo{
			knowledge: knowledge,
		}},
	}

	payload, err := json.Marshal(types.WebCrawlScanPayload{DataSourceID: dataSourceID, ScanID: scan.ID})
	require.NoError(t, err)
	require.NoError(t, svc.ProcessWebCrawlScan(context.Background(), asynq.NewTask(types.TypeWebCrawlScan, payload)))

	assert.Empty(t, pages.changes, "an already-applied page must not reappear as added")
	page := pages.pages[canonicalURL]
	require.NotNil(t, page)
	assert.Equal(t, knowledgeID, page.KnowledgeID)
	assert.NotEmpty(t, page.LastAppliedHash)
	assert.Equal(t, 1, scan.ItemsSkipped)
}

func TestProcessWebCrawlScanAdoptsExistingURLKnowledgeWithoutDataSourceMetadata(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path != "/root" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><body><main><h1>Guide</h1><p>Content</p></main></body></html>`))
	}))
	defer server.Close()

	const (
		dataSourceID  = "datasource-1"
		knowledgeID   = "knowledge-1"
		knowledgeBase = "kb-1"
	)
	canonicalURL := server.URL + "/root"
	ds := &types.DataSource{
		ID:              dataSourceID,
		TenantID:        1,
		KnowledgeBaseID: knowledgeBase,
		Type:            types.ConnectorTypeWebCrawler,
		Config:          types.JSON(`{"settings":{"seed_urls":["` + server.URL + `/root/"],"max_pages":1,"respect_robots":false}}`),
	}
	scan := &types.WebCrawlScan{ID: "scan-1", DataSourceID: dataSourceID, Status: types.WebCrawlScanStatusScanning}
	pages := &webCrawlStateRepo{scan: scan, pages: map[string]*types.WebCrawlPage{}}
	knowledge := &types.Knowledge{ID: knowledgeID, Type: "url", Source: canonicalURL, FolderPath: ""}
	svc := &DataSourceService{
		dsRepo:         &webCrawlTestDataSourceRepo{ds: ds},
		webCrawlerRepo: pages,
		knowledgeService: &webCrawlBaselineKnowledgeService{repo: &webCrawlBaselineKnowledgeRepo{
			sourceKnowledge: knowledge,
		}},
	}

	payload, err := json.Marshal(types.WebCrawlScanPayload{DataSourceID: dataSourceID, ScanID: scan.ID})
	require.NoError(t, err)
	require.NoError(t, svc.ProcessWebCrawlScan(context.Background(), asynq.NewTask(types.TypeWebCrawlScan, payload)))

	assert.Empty(t, pages.changes, "an existing URL knowledge item must not reappear as added")
	page := pages.pages[canonicalURL]
	require.NotNil(t, page)
	assert.Equal(t, knowledgeID, page.KnowledgeID)
	assert.NotEmpty(t, page.LastAppliedHash)
	assert.Equal(t, 1, scan.ItemsSkipped)
}

func TestWebCrawlKnowledgeNeedsRefreshWhenPageTitleChanges(t *testing.T) {
	knowledge := &types.Knowledge{
		Type:       "url",
		Source:     "https://example.com/guide",
		FolderPath: "docs",
		Title:      "Guide.md",
	}
	page := webcrawler.Page{
		CanonicalURL: "https://example.com/guide",
		FolderPath:   "docs",
		Title:        "Guide",
	}

	assert.True(t, webCrawlKnowledgeNeedsRefresh(knowledge, page))
	knowledge.Title = page.Title
	assert.False(t, webCrawlKnowledgeNeedsRefresh(knowledge, page))
}

type webCrawlStateRepo struct {
	interfaces.WebCrawlerRepository
	scan    *types.WebCrawlScan
	pages   map[string]*types.WebCrawlPage
	changes []*types.WebCrawlChange
}

func (r *webCrawlStateRepo) FindScan(context.Context, string) (*types.WebCrawlScan, error) {
	return r.scan, nil
}

func (r *webCrawlStateRepo) ListPages(context.Context, string) ([]*types.WebCrawlPage, error) {
	pages := make([]*types.WebCrawlPage, 0, len(r.pages))
	for _, page := range r.pages {
		pages = append(pages, page)
	}
	return pages, nil
}

func (r *webCrawlStateRepo) FindPage(_ context.Context, _ string, canonicalURL string) (*types.WebCrawlPage, error) {
	return r.pages[canonicalURL], nil
}

func (r *webCrawlStateRepo) CreatePage(_ context.Context, page *types.WebCrawlPage) error {
	if page.ID == "" {
		page.ID = "page-" + page.CanonicalURL
	}
	r.pages[page.CanonicalURL] = page
	return nil
}

func (r *webCrawlStateRepo) UpdatePage(_ context.Context, page *types.WebCrawlPage) error {
	r.pages[page.CanonicalURL] = page
	return nil
}

func (r *webCrawlStateRepo) CreateChange(_ context.Context, change *types.WebCrawlChange) error {
	r.changes = append(r.changes, change)
	return nil
}

func (r *webCrawlStateRepo) UpdateScan(context.Context, *types.WebCrawlScan) error {
	return nil
}

type webCrawlBaselineKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledge       *types.Knowledge
	sourceKnowledge *types.Knowledge
}

func (r *webCrawlBaselineKnowledgeRepo) FindByDataSourceExternalID(
	context.Context, uint64, string, string, string,
) (*types.Knowledge, error) {
	return r.knowledge, nil
}

func (r *webCrawlBaselineKnowledgeRepo) CheckKnowledgeExists(
	context.Context, uint64, string, *types.KnowledgeCheckParams,
) (bool, *types.Knowledge, error) {
	return r.sourceKnowledge != nil, r.sourceKnowledge, nil
}

type webCrawlBaselineKnowledgeService struct {
	interfaces.KnowledgeService
	repo interfaces.KnowledgeRepository
}

func (s *webCrawlBaselineKnowledgeService) GetRepository() interfaces.KnowledgeRepository {
	return s.repo
}

func TestApplyWebCrawlChangeRebuildsLegacyURLKnowledge(t *testing.T) {
	const (
		dataSourceID  = "datasource-1"
		knowledgeBase = "kb-1"
		canonicalURL  = "https://docs.example.com/guide.html"
		legacyID      = "knowledge-legacy"
	)
	legacy := &types.Knowledge{ID: legacyID, Type: "url", Source: canonicalURL}
	knowledgeRepo := &webCrawlApplyKnowledgeRepo{live: map[string]*types.Knowledge{legacy.ID: legacy}}
	knowledgeService := &webCrawlApplyKnowledgeService{repo: knowledgeRepo}
	page := &types.WebCrawlPage{ID: "page-1", DataSourceID: dataSourceID, CanonicalURL: canonicalURL, KnowledgeID: legacyID}
	pages := &webCrawlStateRepo{pages: map[string]*types.WebCrawlPage{canonicalURL: page}}
	svc := &DataSourceService{webCrawlerRepo: pages, knowledgeService: knowledgeService}
	ds := &types.DataSource{ID: dataSourceID, TenantID: 1, KnowledgeBaseID: knowledgeBase, Type: types.ConnectorTypeWebCrawler}
	change := &types.WebCrawlChange{
		CanonicalURL: canonicalURL,
		ChangeType:   types.WebCrawlChangeUpdated,
		Title:        "GATT over BR/EDR",
		FolderPath:   "middleware",
		NewContent:   "# Latest guide",
		NewHash:      "latest-hash",
	}

	require.NoError(t, svc.applyWebCrawlChange(context.Background(), ds, change, nil))

	assert.Equal(t, []string{"delete:" + legacyID, "create:middleware/GATT-over-BR-EDR.md"}, knowledgeService.events)
	assert.Equal(t, []string{legacyID}, knowledgeRepo.hardDeleted)
	require.Len(t, knowledgeRepo.live, 1, "the updated page must replace, not duplicate, the legacy knowledge")
	assert.NotNil(t, knowledgeRepo.live["knowledge-rebuilt"])
	assert.Equal(t, change.Title, knowledgeRepo.live["knowledge-rebuilt"].Title)
	assert.Equal(t, "knowledge-rebuilt", page.KnowledgeID)
	assert.Equal(t, "latest-hash", page.LastAppliedHash)
	assert.False(t, webCrawlKnowledgeNeedsRefresh(knowledgeRepo.live["knowledge-rebuilt"], webcrawler.Page{
		CanonicalURL: canonicalURL,
		FolderPath:   change.FolderPath,
	}), "an applied page with an unchanged hash must not reappear due to a title slash")
}

type webCrawlApplyKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	live        map[string]*types.Knowledge
	hardDeleted []string
}

func (r *webCrawlApplyKnowledgeRepo) FindByDataSourceExternalID(
	context.Context, uint64, string, string, string,
) (*types.Knowledge, error) {
	return nil, nil
}

func (r *webCrawlApplyKnowledgeRepo) CheckKnowledgeExists(
	_ context.Context, _ uint64, _ string, params *types.KnowledgeCheckParams,
) (bool, *types.Knowledge, error) {
	for _, knowledge := range r.live {
		if knowledge.Type == "url" && knowledge.Source == params.URL {
			return true, knowledge, nil
		}
	}
	return false, nil, nil
}

func (r *webCrawlApplyKnowledgeRepo) HardDeleteKnowledge(_ context.Context, _ uint64, id string) error {
	r.hardDeleted = append(r.hardDeleted, id)
	delete(r.live, id)
	return nil
}

func (r *webCrawlApplyKnowledgeRepo) UpdateKnowledge(context.Context, *types.Knowledge) error {
	return nil
}

type webCrawlApplyKnowledgeService struct {
	interfaces.KnowledgeService
	repo   *webCrawlApplyKnowledgeRepo
	events []string
}

func (s *webCrawlApplyKnowledgeService) GetRepository() interfaces.KnowledgeRepository {
	return s.repo
}

func (s *webCrawlApplyKnowledgeService) DeleteKnowledge(_ context.Context, id string) error {
	s.events = append(s.events, "delete:"+id)
	return nil
}

func (s *webCrawlApplyKnowledgeService) CreateKnowledgeFromFile(
	_ context.Context, _ string, _ *multipart.FileHeader, _ map[string]string, _ *bool,
	customFileName string, _ []string, _ string, _ *types.KnowledgeProcessOverrides,
) (*types.Knowledge, error) {
	s.events = append(s.events, "create:"+customFileName)
	folderPath, _ := types.SplitKnowledgeRelativePath(customFileName)
	knowledge := &types.Knowledge{ID: "knowledge-rebuilt", FolderPath: folderPath}
	s.repo.live[knowledge.ID] = knowledge
	return knowledge, nil
}
