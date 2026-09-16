package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestProcessWebCrawlScanRechecksUnlinkedKnowledge(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests atomic.Int32
			fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/docs/old.html" {
					requests.Add(1)
					w.WriteHeader(status)
					return
				}
				writeWebCrawlIndex(w, "")
			}, nil)
			knowledge := fixture.addKnowledge("current-knowledge", "/docs/old.html")
			page := fixture.addBaseline(knowledge.Source, knowledge.ID)
			originalKnowledge, err := json.Marshal(knowledge)
			require.NoError(t, err)

			fixture.scan(t)

			require.EqualValues(t, 1, requests.Load(), "a known URL must be checked even after its navigation link disappears")
			require.Len(t, fixture.pages.changes, 1)
			change := fixture.pages.changes[0]
			require.Equal(t, knowledge.Source, change.CanonicalURL)
			require.Equal(t, page.ID, change.PageID)
			require.Equal(t, status, change.SourceStatus)
			if status == http.StatusNotFound || status == http.StatusGone {
				require.Equal(t, types.WebCrawlChangeMissing, change.ChangeType)
				require.Equal(t, "applied-hash", change.OldHash)
				require.Equal(t, "# Previously imported content", change.PreviousContent)
				require.Equal(t, 1, fixture.pages.scan.ItemsMissing)
				require.Zero(t, fixture.pages.scan.ItemsFailed)
				require.Equal(t, types.WebCrawlApplyApplied, change.ApplyStatus)
				require.Equal(t, types.WebCrawlDecisionApply, change.Decision)
				require.Equal(t, "delete", change.Action)
				require.Equal(t, []string{knowledge.ID}, fixture.knowledge.hardDeletedIDs)
				require.Equal(t, "deleted", fixture.pages.pages[knowledge.Source].Status)
				require.Empty(t, fixture.pages.pages[knowledge.Source].KnowledgeID)
				require.Equal(t, types.WebCrawlScanStatusCompleted, fixture.pages.scan.Status)
				require.Equal(t, 1, fixture.pages.scan.ItemsApplied)
			} else {
				require.Equal(t, types.WebCrawlChangeFailed, change.ChangeType)
				require.Zero(t, fixture.pages.scan.ItemsMissing)
				require.Equal(t, 1, fixture.pages.scan.ItemsFailed)
				require.Empty(t, fixture.knowledge.deletedIDs)
				require.Equal(t, "active", page.Status)
				afterKnowledge, err := json.Marshal(knowledge)
				require.NoError(t, err)
				require.JSONEq(t, string(originalKnowledge), string(afterKnowledge), "temporary failures must preserve imported knowledge")
				require.Equal(t, types.WebCrawlScanStatusPartialFailed, fixture.pages.scan.Status)
			}
			require.Equal(t, "applied-hash", page.LastAppliedHash)
			require.Equal(t, "# Previously imported content", page.LastAppliedContent)
		})
	}
}

func TestProcessWebCrawlScanReconcilesHistoricalKnowledge(t *testing.T) {
	for _, baseline := range []string{"none", "stale", "crawler_metadata"} {
		t.Run(baseline, func(t *testing.T) {
			fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/docs/old.html" {
					http.NotFound(w, r)
					return
				}
				writeWebCrawlIndex(w, "")
			}, nil)
			knowledge := fixture.addKnowledge("reimported-knowledge", "/docs/old.html")
			canonicalURL := knowledge.Source
			wantHash := ""
			if baseline == "stale" {
				fixture.addBaseline(canonicalURL, "deleted-knowledge")
				wantHash = "applied-hash"
			}
			if baseline == "crawler_metadata" {
				knowledge.Type = "file"
				knowledge.Source = "uploaded-document"
				metadata, err := json.Marshal(map[string]string{"datasource_id": fixture.ds.ID, "external_id": canonicalURL, "content_hash": "imported-hash"})
				require.NoError(t, err)
				knowledge.Metadata = metadata
				wantHash = "imported-hash"
			}

			fixture.scan(t)

			page := fixture.pages.pages[canonicalURL]
			require.NotNil(t, page)
			require.Equal(t, []string{knowledge.ID}, fixture.knowledge.hardDeletedIDs, "delete the current knowledge instead of the stale baseline ID")
			require.Empty(t, page.KnowledgeID)
			require.Equal(t, "deleted", page.Status)
			require.Equal(t, wantHash, page.LastAppliedHash)
			require.Len(t, fixture.pages.changes, 1)
			change := fixture.pages.changes[0]
			require.Equal(t, types.WebCrawlChangeMissing, change.ChangeType)
			require.Equal(t, page.ID, change.PageID)
			require.Equal(t, wantHash, change.OldHash)
			require.Equal(t, 1, fixture.pages.scan.ItemsMissing)
		})
	}
}

func TestProcessWebCrawlScanRespectsKnowledgeDataSourceOwnership(t *testing.T) {
	for _, order := range [][]string{{"legacy", "owned"}, {"owned", "legacy"}} {
		t.Run(order[0]+"_first", func(t *testing.T) {
			var foreignRequests atomic.Int32
			fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/docs/index.html" {
					writeWebCrawlIndex(w, "")
					return
				}
				if r.URL.Path == "/docs/other.html" {
					foreignRequests.Add(1)
				}
				http.NotFound(w, r)
			}, nil)
			for _, id := range order {
				knowledge := fixture.addKnowledge(id, "/docs/old.html")
				if id == "owned" {
					metadata, err := json.Marshal(map[string]string{"datasource_id": fixture.ds.ID, "external_id": knowledge.Source})
					require.NoError(t, err)
					knowledge.Metadata = metadata
				}
			}
			foreign := fixture.addKnowledge("foreign", "/docs/other.html")
			foreign.Metadata = types.JSON(`{"datasource_id":"other-datasource"}`)
			conflicting := fixture.addKnowledge("foreign-conflict", "/docs/old.html")
			conflicting.Metadata = foreign.Metadata

			fixture.scan(t)

			page := fixture.pages.pages[fixture.baseURL+"/docs/old.html"]
			require.NotNil(t, page)
			require.Equal(t, []string{"owned"}, fixture.knowledge.hardDeletedIDs, "the data source's import takes precedence over a legacy URL import")
			require.NotContains(t, fixture.pages.pages, foreign.Source)
			require.Zero(t, foreignRequests.Load(), "knowledge owned by another data source must not expand this scan")
			require.Len(t, fixture.pages.changes, 1)
			require.Equal(t, types.WebCrawlChangeMissing, fixture.pages.changes[0].ChangeType)
			require.Equal(t, page.ID, fixture.pages.changes[0].PageID)
		})
	}
}

func TestProcessWebCrawlScanDoesNotInferMissingFromSkippedDiscovery(t *testing.T) {
	for _, scenario := range []string{"page_budget", "robots", "outside_scope"} {
		t.Run(scenario, func(t *testing.T) {
			var requests atomic.Int32
			targetPath := "/docs/old.html"
			if scenario == "outside_scope" {
				targetPath = "/private/old.html"
			}
			fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/robots.txt":
					_, _ = w.Write([]byte("User-agent: *\nDisallow: /docs/old.html\n"))
				case targetPath:
					requests.Add(1)
					if scenario != "page_budget" {
						http.NotFound(w, r)
						return
					}
					writeWebCrawlIndex(w, "")
				default:
					writeWebCrawlIndex(w, `<a href="`+targetPath+`">Old page</a>`)
				}
			}, map[string]interface{}{"respect_robots": scenario == "robots"})
			knowledge := fixture.addKnowledge("knowledge-old", targetPath)
			page := fixture.addBaseline(knowledge.Source, knowledge.ID)

			fixture.scan(t)

			require.Zero(t, fixture.pages.scan.ItemsMissing)
			for _, change := range fixture.pages.changes {
				require.NotEqual(t, types.WebCrawlChangeMissing, change.ChangeType)
			}
			require.Equal(t, "applied-hash", page.LastAppliedHash)
			require.Equal(t, "# Previously imported content", page.LastAppliedContent)
			switch scenario {
			case "page_budget":
				require.EqualValues(t, 1, requests.Load(), "historical checks must run after the discovery budget is exhausted")
				require.Len(t, fixture.pages.changes, 1)
				require.Equal(t, types.WebCrawlChangeUpdated, fixture.pages.changes[0].ChangeType)
				require.Equal(t, http.StatusOK, fixture.pages.changes[0].SourceStatus)
			case "robots":
				require.Zero(t, requests.Load())
				require.Len(t, fixture.pages.changes, 1)
				require.Equal(t, types.WebCrawlChangeFailed, fixture.pages.changes[0].ChangeType)
				require.Contains(t, fixture.pages.changes[0].ErrorMessage, "robots.txt")
			case "outside_scope":
				require.Zero(t, requests.Load())
				require.Empty(t, fixture.pages.changes)
			}
		})
	}
}

func TestProcessWebCrawlScanNewBrokenLinkIsFailedWithoutPage(t *testing.T) {
	fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/broken.html" {
			http.NotFound(w, r)
			return
		}
		writeWebCrawlIndex(w, `<a href="/docs/broken.html">Broken page</a>`)
	}, map[string]interface{}{"max_pages": 2})

	fixture.scan(t)

	require.Len(t, fixture.pages.changes, 1)
	change := fixture.pages.changes[0]
	require.Equal(t, fixture.baseURL+"/docs/broken.html", change.CanonicalURL)
	require.Equal(t, types.WebCrawlChangeFailed, change.ChangeType)
	require.Equal(t, http.StatusNotFound, change.SourceStatus)
	require.Empty(t, change.PageID)
	require.Zero(t, fixture.pages.scan.ItemsMissing)
	require.Equal(t, 1, fixture.pages.scan.ItemsFailed)
}

func TestProcessWebCrawlScanDeletesDisabledKnowledge(t *testing.T) {
	fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/old.html" {
			http.NotFound(w, r)
			return
		}
		writeWebCrawlIndex(w, "")
	}, nil)
	knowledge := fixture.addKnowledge("disabled-knowledge", "/docs/old.html")
	knowledge.EnableStatus = "disabled"
	fixture.addBaseline(knowledge.Source, knowledge.ID).Status = "disabled"
	fixture.scan(t)
	require.Equal(t, []string{knowledge.ID}, fixture.knowledge.hardDeletedIDs)
	require.Equal(t, "deleted", fixture.pages.pages[knowledge.Source].Status)
	require.Equal(t, types.WebCrawlScanStatusCompleted, fixture.pages.scan.Status)
}

func TestProcessWebCrawlScanDeletionFailureCanBeRetried(t *testing.T) {
	fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs/old.html" {
			http.NotFound(w, r)
			return
		}
		writeWebCrawlIndex(w, "")
	}, nil)
	knowledge := fixture.addKnowledge("knowledge-old", "/docs/old.html")
	fixture.addBaseline(knowledge.Source, knowledge.ID)
	fixture.knowledge.deleteErr = errors.New("index unavailable")
	fixture.scan(t)
	require.Len(t, fixture.pages.changes, 1)
	change := fixture.pages.changes[0]
	require.Equal(t, "delete", change.Action)
	require.Equal(t, types.WebCrawlApplyFailed, change.ApplyStatus)
	require.Contains(t, change.ErrorMessage, "index unavailable")
	require.Empty(t, fixture.knowledge.hardDeletedIDs)
	require.False(t, knowledge.DeletedAt.Valid)
	require.Equal(t, knowledge.ID, fixture.pages.pages[knowledge.Source].KnowledgeID)
	require.Equal(t, types.WebCrawlScanStatusPartialFailed, fixture.pages.scan.Status)
	require.Zero(t, fixture.pages.scan.ItemsApplied)

	fixture.knowledge.deleteErr = nil
	enqueuer := &webCrawlRetryEnqueuer{}
	fixture.svc.taskEnqueuer = enqueuer
	fixture.svc.webCrawlerRepo = &webCrawlMissingApplyRepo{webCrawlStateRepo: fixture.pages}
	require.NoError(t, fixture.svc.RetryWebCrawlChanges(context.Background(), fixture.pages.scan.ID, []string{change.ID}))
	require.Len(t, enqueuer.tasks, 1)
	require.Equal(t, types.WebCrawlApplyQueued, change.ApplyStatus)
	require.Equal(t, "delete", change.Action)
	require.NoError(t, fixture.svc.ProcessWebCrawlApply(context.Background(), enqueuer.tasks[0]))
	require.Equal(t, types.WebCrawlApplyApplied, change.ApplyStatus)
	require.Empty(t, change.ErrorMessage)
	require.NotNil(t, change.AppliedAt)
	require.Equal(t, []string{knowledge.ID}, fixture.knowledge.hardDeletedIDs)
	require.Equal(t, types.WebCrawlScanStatusCompleted, fixture.pages.scan.Status)
	require.Equal(t, 1, fixture.pages.scan.ItemsApplied)
	// Redelivery of the apply task must not delete or count the page twice.
	require.NoError(t, fixture.svc.ProcessWebCrawlApply(context.Background(), enqueuer.tasks[0]))
	require.Len(t, fixture.knowledge.hardDeletedIDs, 1)
	require.Equal(t, 1, fixture.pages.scan.ItemsApplied)
}

type webCrawlRetryEnqueuer struct{ tasks []*asynq.Task }

func (e *webCrawlRetryEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	e.tasks = append(e.tasks, task)
	return &asynq.TaskInfo{}, nil
}

func TestProcessWebCrawlApplyKeepsPendingContentReviewableAfterDeletionRetry(t *testing.T) {
	fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/docs/old.html":
			http.NotFound(w, r)
		case "/docs/index.html":
			writeWebCrawlIndex(w, `<a href="/docs/new.html">New page</a>`)
		default:
			writeWebCrawlIndex(w, "")
		}
	}, map[string]interface{}{"max_pages": 2})
	knowledge := fixture.addKnowledge("knowledge-old", "/docs/old.html")
	fixture.addBaseline(knowledge.Source, knowledge.ID)
	fixture.knowledge.deleteErr = errors.New("temporary deletion failure")
	fixture.scan(t)
	require.Len(t, fixture.pages.changes, 2)
	var deletion, addition *types.WebCrawlChange
	for _, change := range fixture.pages.changes {
		if change.ChangeType == types.WebCrawlChangeMissing {
			deletion = change
		} else if change.ChangeType == types.WebCrawlChangeAdded {
			addition = change
		}
	}
	require.NotNil(t, deletion)
	require.NotNil(t, addition)
	fixture.knowledge.deleteErr = nil
	enqueuer := &webCrawlRetryEnqueuer{}
	fixture.svc.taskEnqueuer = enqueuer
	fixture.svc.webCrawlerRepo = &webCrawlMissingApplyRepo{webCrawlStateRepo: fixture.pages}
	require.NoError(t, fixture.svc.RetryWebCrawlChanges(context.Background(), fixture.pages.scan.ID, []string{deletion.ID}))
	require.Len(t, enqueuer.tasks, 1)
	require.NoError(t, fixture.svc.ProcessWebCrawlApply(context.Background(), enqueuer.tasks[0]))
	require.Equal(t, types.WebCrawlApplyApplied, deletion.ApplyStatus)
	require.Equal(t, types.WebCrawlApplyPending, addition.ApplyStatus)
	require.Equal(t, types.WebCrawlScanStatusReviewReady, fixture.pages.scan.Status)
	require.NoError(t, fixture.svc.ApplyWebCrawlChanges(context.Background(), fixture.pages.scan.ID, []string{addition.ID}, nil))
	require.Equal(t, types.WebCrawlApplyQueued, addition.ApplyStatus)
	require.Len(t, enqueuer.tasks, 2)
}

func TestProcessWebCrawlScanResumesDeletionAfterResultSaveFailure(t *testing.T) {
	var requests atomic.Int32
	fixture := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/docs/old.html" {
			http.NotFound(w, r)
			return
		}
		writeWebCrawlIndex(w, "")
	}, nil)
	knowledge := fixture.addKnowledge("knowledge-old", "/docs/old.html")
	fixture.addBaseline(knowledge.Source, knowledge.ID)
	repo := &webCrawlInterruptedDeleteRepo{webCrawlStateRepo: fixture.pages, failSave: true}
	fixture.svc.webCrawlerRepo = repo
	payload, err := json.Marshal(types.WebCrawlScanPayload{TenantID: fixture.ds.TenantID, DataSourceID: fixture.ds.ID, ScanID: fixture.pages.scan.ID})
	require.NoError(t, err)
	task := asynq.NewTask(types.TypeWebCrawlScan, payload)
	require.ErrorContains(t, fixture.svc.ProcessWebCrawlScan(context.Background(), task), "save interrupted")
	require.Equal(t, types.WebCrawlScanStatusPartialFailed, fixture.pages.scan.Status)
	require.Equal(t, []string{knowledge.ID}, fixture.knowledge.hardDeletedIDs)
	require.Equal(t, types.WebCrawlApplyQueued, repo.persisted[0].ApplyStatus)
	require.Zero(t, fixture.pages.scan.ItemsApplied)
	fetched := requests.Load()
	repo.failSave = false
	for i := 0; i < 2; i++ {
		require.NoError(t, fixture.svc.ProcessWebCrawlScan(context.Background(), task))
		require.Equal(t, fetched, requests.Load(), "resume the saved deletion rather than append another scan")
		require.Len(t, repo.persisted, 1)
		require.Equal(t, types.WebCrawlApplyApplied, repo.persisted[0].ApplyStatus)
		require.Empty(t, repo.persisted[0].ErrorMessage)
		require.Equal(t, 1, fixture.pages.scan.ItemsApplied)
		require.Len(t, fixture.knowledge.hardDeletedIDs, 1)
		require.Equal(t, types.WebCrawlScanStatusPartialFailed, fixture.pages.scan.Status, "unprocessed URLs still require a fresh scan")
	}
}

type webCrawlInterruptedDeleteRepo struct {
	*webCrawlStateRepo
	persisted []*types.WebCrawlChange
	failSave  bool
}

func (r *webCrawlInterruptedDeleteRepo) CreateChange(ctx context.Context, change *types.WebCrawlChange) error {
	if err := r.webCrawlStateRepo.CreateChange(ctx, change); err != nil {
		return err
	}
	copy := *change
	r.persisted = append(r.persisted, &copy)
	return nil
}

func (r *webCrawlInterruptedDeleteRepo) UpdateChange(_ context.Context, change *types.WebCrawlChange) error {
	if r.failSave {
		return errors.New("save interrupted")
	}
	for i, previous := range r.persisted {
		if previous.ID == change.ID {
			copy := *change
			r.persisted[i] = &copy
		}
	}
	return nil
}

func (r *webCrawlInterruptedDeleteRepo) ListChanges(context.Context, string, string, string, string, int, int) ([]*types.WebCrawlChange, error) {
	var copies []*types.WebCrawlChange
	for _, change := range r.persisted {
		copy := *change
		copies = append(copies, &copy)
	}
	return copies, nil
}

func TestProcessWebCrawlApplyRetainsScanFailure(t *testing.T) {
	for _, test := range []struct {
		name        string
		scanError   string
		itemsFailed int
		wantStatus  string
	}{
		{name: "scan_error", scanError: "scan persistence failed", wantStatus: types.WebCrawlScanStatusPartialFailed},
		{name: "fetch_failure", itemsFailed: 1, wantStatus: types.WebCrawlScanStatusPartialFailed},
		{name: "success", wantStatus: types.WebCrawlScanStatusCompleted},
	} {
		t.Run(test.name, func(t *testing.T) {
			ds := &types.DataSource{ID: "datasource-1", TenantID: 17, KnowledgeBaseID: "kb-1", Type: types.ConnectorTypeWebCrawler}
			scan := &types.WebCrawlScan{ID: "scan-1", DataSourceID: ds.ID, TenantID: ds.TenantID, Status: types.WebCrawlScanStatusApplying, ErrorMessage: test.scanError, ItemsFailed: test.itemsFailed}
			change := &types.WebCrawlChange{ID: "change-1", ScanID: scan.ID, ChangeType: types.WebCrawlChangeMissing, Action: "keep", ApplyStatus: types.WebCrawlApplyQueued}
			repo := &webCrawlMissingApplyRepo{webCrawlStateRepo: &webCrawlStateRepo{scan: scan, changes: []*types.WebCrawlChange{change}}}
			svc := &DataSourceService{webCrawlerRepo: repo, dsRepo: &webCrawlTestDataSourceRepo{ds: ds}, kbService: &webCrawlTestKBService{kb: &types.KnowledgeBase{ID: ds.KnowledgeBaseID, TenantID: ds.TenantID}}}
			payload, err := json.Marshal(types.WebCrawlApplyPayload{TenantID: ds.TenantID, DataSourceID: ds.ID, ScanID: scan.ID, ChangeIDs: []string{change.ID}})
			require.NoError(t, err)

			require.NoError(t, svc.ProcessWebCrawlApply(context.Background(), asynq.NewTask(types.TypeWebCrawlApply, payload)))

			require.Equal(t, types.WebCrawlApplyApplied, change.ApplyStatus)
			require.Equal(t, 1, scan.ItemsApplied)
			require.Equal(t, test.scanError, scan.ErrorMessage)
			require.Equal(t, test.itemsFailed, scan.ItemsFailed)
			require.Equal(t, test.wantStatus, scan.Status)
		})
	}
}

type webCrawlMissingFixture struct {
	svc       *DataSourceService
	ds        *types.DataSource
	pages     *webCrawlStateRepo
	knowledge *webCrawlMissingKnowledgeRepo
	baseURL   string
}

func newWebCrawlMissingFixture(t *testing.T, handler http.HandlerFunc, settings map[string]interface{}) *webCrawlMissingFixture {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	configSettings := map[string]interface{}{"seed_urls": []string{server.URL + "/docs/index.html"}, "path_prefixes": []string{"/docs/"}, "max_pages": 1, "respect_robots": false}
	for key, value := range settings {
		configSettings[key] = value
	}
	config, err := json.Marshal(map[string]interface{}{"settings": configSettings})
	require.NoError(t, err)
	ds := &types.DataSource{ID: "datasource-1", TenantID: 17, KnowledgeBaseID: "kb-1", Type: types.ConnectorTypeWebCrawler, Config: config}
	pages := &webCrawlStateRepo{scan: &types.WebCrawlScan{ID: "scan-1", DataSourceID: ds.ID, TenantID: ds.TenantID, Status: types.WebCrawlScanStatusScanning}, pages: make(map[string]*types.WebCrawlPage)}
	knowledge := &webCrawlMissingKnowledgeRepo{t: t, ds: ds}
	svc := &DataSourceService{dsRepo: &webCrawlTestDataSourceRepo{ds: ds}, webCrawlerRepo: pages, knowledgeService: &webCrawlMissingKnowledgeService{repo: knowledge}, kbService: &webCrawlTestKBService{kb: &types.KnowledgeBase{ID: ds.KnowledgeBaseID, TenantID: ds.TenantID}}}
	fixture := &webCrawlMissingFixture{svc: svc, ds: ds, pages: pages, knowledge: knowledge, baseURL: server.URL}
	fixture.addKnowledge("index-knowledge", "/docs/index.html")
	return fixture
}

func (f *webCrawlMissingFixture) scan(t *testing.T) {
	t.Helper()
	payload, err := json.Marshal(types.WebCrawlScanPayload{TenantID: f.ds.TenantID, DataSourceID: f.ds.ID, ScanID: f.pages.scan.ID})
	require.NoError(t, err)
	require.NoError(t, f.svc.ProcessWebCrawlScan(context.Background(), asynq.NewTask(types.TypeWebCrawlScan, payload)))
}

func (f *webCrawlMissingFixture) addKnowledge(id, path string) *types.Knowledge {
	knowledge := &types.Knowledge{ID: id, TenantID: f.ds.TenantID, KnowledgeBaseID: f.ds.KnowledgeBaseID, Type: "url", Source: f.baseURL + path, Title: "Guide", EnableStatus: "enabled", FileHash: "original-file-hash"}
	f.knowledge.knowledges = append(f.knowledge.knowledges, knowledge)
	return knowledge
}

func (f *webCrawlMissingFixture) addBaseline(canonicalURL, knowledgeID string) *types.WebCrawlPage {
	page := &types.WebCrawlPage{ID: "old-page", DataSourceID: f.ds.ID, CanonicalURL: canonicalURL, KnowledgeID: knowledgeID, Title: "Old guide", Status: "active", LastAppliedHash: "applied-hash", LastAppliedContent: "# Previously imported content"}
	f.pages.pages[canonicalURL] = page
	return page
}

func writeWebCrawlIndex(w http.ResponseWriter, links string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<html><body><main><h1>Guide</h1><p>Current documentation.</p>`+links+`</main></body></html>`)
}

type webCrawlMissingKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	t              *testing.T
	ds             *types.DataSource
	knowledges     []*types.Knowledge
	deletedIDs     []string
	hardDeletedIDs []string
	deleteErr      error
}

func (r *webCrawlMissingKnowledgeRepo) ListKnowledgeByKnowledgeBaseID(_ context.Context, tenantID uint64, kbID string) ([]*types.Knowledge, error) {
	require.Equal(r.t, r.ds.TenantID, tenantID)
	require.Equal(r.t, r.ds.KnowledgeBaseID, kbID)
	var matches []*types.Knowledge
	for _, knowledge := range r.knowledges {
		if knowledge.TenantID == tenantID && knowledge.KnowledgeBaseID == kbID && !knowledge.DeletedAt.Valid {
			matches = append(matches, knowledge)
		}
	}
	return matches, nil
}

func (r *webCrawlMissingKnowledgeRepo) GetKnowledgeBatch(_ context.Context, tenantID uint64, ids []string) ([]*types.Knowledge, error) {
	var matches []*types.Knowledge
	for _, knowledge := range r.knowledges {
		for _, id := range ids {
			if knowledge.ID == id && knowledge.TenantID == tenantID && !knowledge.DeletedAt.Valid {
				matches = append(matches, knowledge)
			}
		}
	}
	return matches, nil
}

func (r *webCrawlMissingKnowledgeRepo) HardDeleteKnowledge(_ context.Context, tenantID uint64, id string) error {
	require.Equal(r.t, r.ds.TenantID, tenantID)
	r.hardDeletedIDs = append(r.hardDeletedIDs, id)
	for i, knowledge := range r.knowledges {
		if knowledge.ID == id {
			r.knowledges = append(r.knowledges[:i], r.knowledges[i+1:]...)
			break
		}
	}
	return nil
}

type webCrawlMissingKnowledgeService struct {
	interfaces.KnowledgeService
	repo *webCrawlMissingKnowledgeRepo
}

func (s *webCrawlMissingKnowledgeService) GetRepository() interfaces.KnowledgeRepository {
	return s.repo
}

func (s *webCrawlMissingKnowledgeService) DeleteKnowledge(ctx context.Context, id string) error {
	require.NoError(s.repo.t, access.RequireKBWrite(ctx, &types.KnowledgeBase{ID: s.repo.ds.KnowledgeBaseID, TenantID: s.repo.ds.TenantID}))
	s.repo.deletedIDs = append(s.repo.deletedIDs, id)
	if s.repo.deleteErr != nil {
		return s.repo.deleteErr
	}
	for _, knowledge := range s.repo.knowledges {
		if knowledge.ID == id {
			knowledge.DeletedAt.Time = time.Now()
			knowledge.DeletedAt.Valid = true
		}
	}
	return nil
}

func (r *webCrawlMissingKnowledgeRepo) FindByDataSourceExternalID(ctx context.Context, tenantID uint64, kbID, dsID, externalID string) (*types.Knowledge, error) {
	require.Equal(r.t, r.ds.ID, dsID)
	knowledges, err := r.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, kbID)
	for _, knowledge := range knowledges {
		metadata := knowledge.GetMetadata()
		if metadata["datasource_id"] == dsID && metadata["external_id"] == externalID {
			return knowledge, nil
		}
	}
	return nil, err
}

func (r *webCrawlMissingKnowledgeRepo) CheckKnowledgeExists(ctx context.Context, tenantID uint64, kbID string, params *types.KnowledgeCheckParams) (bool, *types.Knowledge, error) {
	require.Equal(r.t, "url", params.Type)
	knowledges, err := r.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, kbID)
	for _, knowledge := range knowledges {
		if knowledge.Type == params.Type && knowledge.Source == params.URL {
			return true, knowledge, nil
		}
	}
	return false, nil, err
}

type webCrawlMissingApplyRepo struct {
	*webCrawlStateRepo
}

func (r *webCrawlMissingApplyRepo) ListChangesByIDs(_ context.Context, _ string, ids []string) ([]*types.WebCrawlChange, error) {
	var changes []*types.WebCrawlChange
	for _, change := range r.changes {
		for _, id := range ids {
			if change.ID == id {
				changes = append(changes, change)
				break
			}
		}
	}
	return changes, nil
}

func (r *webCrawlMissingApplyRepo) ListChanges(_ context.Context, _ string, _, _, applyStatus string, _, _ int) ([]*types.WebCrawlChange, error) {
	var changes []*types.WebCrawlChange
	for _, change := range r.changes {
		if change.ApplyStatus == applyStatus {
			changes = append(changes, change)
		}
	}
	return changes, nil
}

func (r *webCrawlMissingApplyRepo) UpdateChange(context.Context, *types.WebCrawlChange) error {
	return nil
}
