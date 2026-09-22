package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/webcrawler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type autoCrawlRepo struct{ *webCrawlMissingApplyRepo }

func (r *autoCrawlRepo) ListChanges(_ context.Context, _ string, kind, decision, status string, limit, offset int) ([]*types.WebCrawlChange, error) {
	var out []*types.WebCrawlChange
	for _, c := range r.changes {
		if (kind == "" || c.ChangeType == kind) && (decision == "" || c.Decision == decision) && (status == "" || c.ApplyStatus == status) {
			out = append(out, c)
		}
	}
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func TestAutomaticWebCrawlAppliesBatchesAndResumesFailures(t *testing.T) {
	ds := &types.DataSource{ID: "website", TenantID: 1, KnowledgeBaseID: "kb", Type: types.ConnectorTypeWebCrawler,
		Status: types.DataSourceStatusActive, SyncSchedule: "0 2 * * *"}
	state := &webCrawlStateRepo{scan: &types.WebCrawlScan{ID: "scan", DataSourceID: ds.ID, TenantID: ds.TenantID,
		Status: types.WebCrawlScanStatusReviewReady}, pages: map[string]*types.WebCrawlPage{}}
	for i := 0; i < 51; i++ {
		url := fmt.Sprintf("https://example.com/%d", i)
		kind := types.WebCrawlChangeAdded
		if i%2 == 0 {
			kind = types.WebCrawlChangeUpdated
		}
		state.pages[url] = &types.WebCrawlPage{ID: fmt.Sprint(i), CanonicalURL: url}
		state.changes = append(state.changes, &types.WebCrawlChange{ID: fmt.Sprint(i), ScanID: "scan", CanonicalURL: url,
			ChangeType: kind, NewContent: "content", NewHash: "new-hash", Title: fmt.Sprint(i), ApplyStatus: types.WebCrawlApplyPending})
	}
	state.changes[0].NewContent = ""
	repo := &autoCrawlRepo{&webCrawlMissingApplyRepo{state}}
	knowledge := &webCrawlApplyKnowledgeService{repo: &webCrawlApplyKnowledgeRepo{live: map[string]*types.Knowledge{}}}
	svc := &DataSourceService{dsRepo: &webCrawlTestDataSourceRepo{ds: ds}, webCrawlerRepo: repo,
		kbService: &webCrawlTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}}, knowledgeService: knowledge}
	data, err := json.Marshal(types.WebCrawlScanPayload{TenantID: 1, DataSourceID: ds.ID, ScanID: "scan", AutoApply: true})
	require.NoError(t, err)
	task := asynq.NewTask(types.TypeWebCrawlScan, data)
	require.Error(t, svc.ProcessWebCrawlScan(context.Background(), task))
	require.Len(t, knowledge.events, 50)
	require.Equal(t, types.WebCrawlScanStatusPartialFailed, state.scan.Status)
	state.changes[0].NewContent = "recovered content"
	require.NoError(t, svc.ProcessWebCrawlScan(context.Background(), task))
	require.Len(t, knowledge.events, 51, "only the failed change is retried")
	require.Equal(t, types.WebCrawlScanStatusCompleted, state.scan.Status)
	for _, page := range state.pages {
		require.Equal(t, "new-hash", page.LastAppliedHash)
	}
	require.NoError(t, svc.ProcessWebCrawlScan(context.Background(), task))
	require.Len(t, knowledge.events, 51)
}

func TestAutomaticWebCrawlPreservesDeletionRules(t *testing.T) {
	for _, status := range []int{200, 404, 410, 403, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newWebCrawlMissingFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/docs/old.html" && status != 200 {
					w.WriteHeader(status)
					return
				}
				writeWebCrawlIndex(w, "")
			}, nil)
			f.ds.Status = types.DataSourceStatusActive
			f.ds.SyncSchedule = "0 2 * * *"
			old := f.addKnowledge("old", "/docs/old.html")
			if status == 200 {
				config, err := f.ds.ParseConfig()
				require.NoError(t, err)
				pages, _, err := webcrawler.NewConnector().FetchPages(context.Background(), config, []string{old.Source})
				require.NoError(t, err)
				require.Len(t, pages, 1)
				baseline := f.addBaseline(old.Source, old.ID)
				baseline.LastAppliedHash = pages[0].ContentHash
				old.FolderPath = pages[0].FolderPath
			}
			f.svc.webCrawlerRepo = &autoCrawlRepo{&webCrawlMissingApplyRepo{f.pages}}
			data, err := json.Marshal(types.WebCrawlScanPayload{TenantID: f.ds.TenantID, DataSourceID: f.ds.ID, ScanID: f.pages.scan.ID, AutoApply: true})
			require.NoError(t, err)
			require.NoError(t, f.svc.ProcessWebCrawlScan(context.Background(), asynq.NewTask(types.TypeWebCrawlScan, data)))
			if status == 404 || status == 410 {
				require.Equal(t, []string{old.ID}, f.knowledge.hardDeletedIDs)
			} else {
				require.Empty(t, f.knowledge.hardDeletedIDs)
			}
			if status == 200 {
				require.Equal(t, 2, f.pages.scan.ItemsSkipped)
			}
		})
	}
}

func TestDataSourceRejectsInvalidScheduleBeforePersistence(t *testing.T) {
	svc := &DataSourceService{}
	_, err := svc.CreateDataSource(context.Background(), &types.DataSource{SyncSchedule: "invalid"})
	require.ErrorContains(t, err, "cron")
	_, err = svc.UpdateDataSource(context.Background(), &types.DataSource{ID: "source", SyncSchedule: "0 9 31 2 *"})
	require.ErrorContains(t, err, "no future execution")
}
