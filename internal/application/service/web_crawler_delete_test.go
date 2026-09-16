package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type webCrawlDeletePageRepo struct {
	interfaces.WebCrawlerRepository
	page        *types.WebCrawlPage
	updateError error
	updates     int
}

func (r *webCrawlDeletePageRepo) FindPage(context.Context, string, string) (*types.WebCrawlPage, error) {
	return r.page, nil
}

func (r *webCrawlDeletePageRepo) UpdatePage(_ context.Context, page *types.WebCrawlPage) error {
	r.updates++
	if r.updateError != nil {
		return r.updateError
	}
	r.page = page
	return nil
}

type webCrawlDeleteKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	events          []string
	hardDeleteError error
}

func (r *webCrawlDeleteKnowledgeRepo) DeleteKnowledgeList(ctx context.Context, tenant uint64, ids []string) error {
	r.events = append(r.events, "cascade")
	return r.KnowledgeRepository.DeleteKnowledgeList(ctx, tenant, ids)
}

func (r *webCrawlDeleteKnowledgeRepo) HardDeleteKnowledge(ctx context.Context, tenant uint64, id string) error {
	r.events = append(r.events, "hard-delete")
	if r.hardDeleteError != nil {
		return r.hardDeleteError
	}
	return r.KnowledgeRepository.HardDeleteKnowledge(ctx, tenant, id)
}

type webCrawlDeleteFixture struct {
	document *documentWriteFixture
	svc      *DataSourceService
	ds       *types.DataSource
	pages    *webCrawlDeletePageRepo
	repo     *webCrawlDeleteKnowledgeRepo
	change   *types.WebCrawlChange
}

func newWebCrawlDeleteFixture(t *testing.T) *webCrawlDeleteFixture {
	t.Helper()
	document := newDocumentWriteFixture(t)
	const source = "https://example.com/docs/removed.html"
	knowledge, err := document.repo.GetKnowledgeByID(document.ctx, 7, "doc")
	require.NoError(t, err)
	knowledge.Type = "url"
	knowledge.Source = source
	knowledge.ParseStatus = types.ParseStatusCompleted
	require.NoError(t, document.repo.UpdateKnowledge(document.ctx, knowledge))
	repo := &webCrawlDeleteKnowledgeRepo{KnowledgeRepository: document.repo}
	document.svc.repo = repo
	ds := &types.DataSource{ID: "ds", Type: types.ConnectorTypeWebCrawler, TenantID: 7, KnowledgeBaseID: "kb"}
	pages := &webCrawlDeletePageRepo{page: &types.WebCrawlPage{
		ID: "page", DataSourceID: ds.ID, CanonicalURL: source, KnowledgeID: "doc", Status: "active",
	}}
	svc := &DataSourceService{webCrawlerRepo: pages, knowledgeService: document.svc}
	change := &types.WebCrawlChange{PageID: "page", CanonicalURL: source, ChangeType: types.WebCrawlChangeMissing}
	return &webCrawlDeleteFixture{document: document, svc: svc, ds: ds, pages: pages, repo: repo, change: change}
}

func TestDeleteWebCrawlPageCascadesBeforeRemovingBaselineReference(t *testing.T) {
	f := newWebCrawlDeleteFixture(t)
	original := *f.pages.page
	require.NoError(t, f.svc.deleteWebCrawlPage(f.document.ctx, f.ds, f.change))
	require.Equal(t, []string{"cascade", "hard-delete"}, f.repo.events)
	require.Equal(t, "deleted", f.pages.page.Status)
	require.Empty(t, f.pages.page.KnowledgeID)
	require.Equal(t, original.LastAppliedHash, f.pages.page.LastAppliedHash)
	var count int64
	require.NoError(t, f.document.db.Unscoped().Model(&types.Knowledge{}).Where("id = ?", "doc").Count(&count).Error)
	require.Zero(t, count, "the sync tombstone must not prevent later reimport")
	require.NoError(t, f.document.db.Model(&types.Chunk{}).Where("knowledge_id = ?", "doc").Count(&count).Error)
	require.Zero(t, count, "deletion must clean chunks through the knowledge service")
	require.Equal(t, 1, f.document.graph.calls)
	require.NoError(t, f.document.db.Model(&types.Knowledge{}).Where("id = ?", "other-doc").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.svc.deleteWebCrawlPage(f.document.ctx, f.ds, f.change))
	require.Len(t, f.repo.events, 2, "a completed deletion must be idempotent")
	require.Equal(t, 1, f.pages.updates)
}

func TestDeleteWebCrawlPageRetriesAfterSoftDelete(t *testing.T) {
	for _, stage := range []string{"hard delete", "page update"} {
		t.Run(stage, func(t *testing.T) {
			f := newWebCrawlDeleteFixture(t)
			before := *f.pages.page
			failure := errors.New("temporary deletion failure")
			if stage == "hard delete" {
				f.repo.hardDeleteError = failure
			} else {
				f.pages.updateError = failure
			}
			require.ErrorIs(t, f.svc.deleteWebCrawlPage(f.document.ctx, f.ds, f.change), failure)
			require.Equal(t, before, *f.pages.page, "failure must retain the persisted knowledge reference")
			_, err := f.document.repo.GetKnowledgeByID(f.document.ctx, 7, "doc")
			require.ErrorIs(t, err, repository.ErrKnowledgeNotFound)
			f.repo.hardDeleteError = nil
			f.pages.updateError = nil
			require.NoError(t, f.svc.deleteWebCrawlPage(f.document.ctx, f.ds, f.change))
			require.Equal(t, []string{"cascade", "hard-delete", "hard-delete"}, f.repo.events)
			require.Equal(t, 1, f.document.graph.calls, "retry must not repeat the completed cleanup cascade")
			require.Equal(t, []int64{-5}, f.document.tenants.adjustments, "storage accounting must not run twice")
			require.Equal(t, "deleted", f.pages.page.Status)
			require.Empty(t, f.pages.page.KnowledgeID)
		})
	}
}

func TestDeleteWebCrawlPageRejectsChangedBindings(t *testing.T) {
	for _, scenario := range []string{
		"other KB", "other data source", "other source URL", "unknown knowledge type", "unowned file", "invalid metadata",
		"other page data source", "other page ID", "other page URL", "missing page",
	} {
		t.Run(scenario, func(t *testing.T) {
			f := newWebCrawlDeleteFixture(t)
			knowledge, err := f.document.repo.GetKnowledgeByID(f.document.ctx, 7, "doc")
			require.NoError(t, err)
			switch scenario {
			case "other KB":
				knowledge.KnowledgeBaseID = "other"
			case "other data source":
				knowledge.Metadata = types.JSON(`{"datasource_id":"other-ds"}`)
			case "other source URL":
				knowledge.Source = "https://example.com/docs/still-valid.html"
			case "unknown knowledge type":
				knowledge.Type = types.KnowledgeTypeManual
			case "unowned file":
				knowledge.Type = "file"
				knowledge.Metadata = types.JSON(`{"external_id":"` + f.change.CanonicalURL + `"}`)
			case "invalid metadata":
				knowledge.Metadata = types.JSON(`[]`)
			case "other page data source":
				f.pages.page.DataSourceID = "other-ds"
			case "other page ID":
				f.pages.page.ID = "other-page"
			case "other page URL":
				f.pages.page.CanonicalURL = "https://example.com/docs/still-valid.html"
			case "missing page":
				f.pages.page = nil
			}
			require.NoError(t, f.document.repo.UpdateKnowledge(f.document.ctx, knowledge))
			require.Error(t, f.svc.deleteWebCrawlPage(f.document.ctx, f.ds, f.change))
			require.Empty(t, f.repo.events)
			require.Zero(t, f.pages.updates)
			require.Zero(t, f.document.graph.calls)
		})
	}
}

func TestDeleteWebCrawlPageAcceptsOwnedFileAndLegacyURL(t *testing.T) {
	for _, knowledgeType := range []string{"url", "file"} {
		t.Run(knowledgeType, func(t *testing.T) {
			f := newWebCrawlDeleteFixture(t)
			knowledge, err := f.document.repo.GetKnowledgeByID(f.document.ctx, 7, "doc")
			require.NoError(t, err)
			knowledge.Type = knowledgeType
			if knowledgeType == "file" {
				knowledge.Source = ""
				knowledge.Metadata = types.JSON(`{"datasource_id":"ds","external_id":"` + f.change.CanonicalURL + `"}`)
			}
			require.NoError(t, f.document.repo.UpdateKnowledge(f.document.ctx, knowledge))
			require.NoError(t, f.svc.deleteWebCrawlPage(f.document.ctx, f.ds, f.change))
			require.Equal(t, "deleted", f.pages.page.Status)
		})
	}
}

func TestDeleteWebCrawlPageKeepsReferenceWhenCascadeFails(t *testing.T) {
	f := newWebCrawlDeleteFixture(t)
	failure := errors.New("graph unavailable")
	f.document.graph.err = failure
	before := *f.pages.page
	require.ErrorIs(t, f.svc.deleteWebCrawlPage(f.document.ctx, f.ds, f.change), failure)
	require.Equal(t, before, *f.pages.page)
	require.Empty(t, f.repo.events, "hard deletion cannot bypass failed resource cleanup")
	require.Zero(t, f.pages.updates)
}

func TestWebCrawlTaskContextGrantsOnlyPersistedKB(t *testing.T) {
	ds := &types.DataSource{ID: "ds", Type: types.ConnectorTypeWebCrawler, TenantID: 7, KnowledgeBaseID: "kb"}
	scan := &types.WebCrawlScan{ID: "scan", DataSourceID: ds.ID, TenantID: 7, InitiatorID: "initiator"}
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	otherKB := &types.KnowledgeBase{ID: "other", TenantID: 7}
	base, err := access.WithKBTaskWrite(context.Background(), otherKB, 7)
	require.NoError(t, err)
	base = context.WithValue(base, types.TenantInfoContextKey, &types.Tenant{ID: 99})
	svc := &DataSourceService{
		kbService:  &processSyncKBService{kb: kb},
		tenantRepo: &processSyncTenantRepo{tenant: &types.Tenant{ID: 7}},
	}
	ctx, err := svc.webCrawlTaskContext(base, ds, scan, 7)
	require.NoError(t, err)
	require.NoError(t, access.RequireKBWrite(ctx, kb))
	require.ErrorIs(t, access.RequireKBWrite(ctx, otherKB), access.ErrForbidden)
	require.NoError(t, access.RequireKBWrite(base, otherKB), "the parent context must remain unchanged")
	require.Equal(t, uint64(7), types.MustTenantIDFromContext(ctx))
	tenant, ok := types.TenantInfoFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, uint64(7), tenant.ID)
	require.Equal(t, "initiator", types.TaskInitiatorFromContext(ctx).UserID)
	require.False(t, types.IsSystemAdminFromContext(ctx))
	require.Equal(t, types.TenantRoleViewer, types.TenantRoleFromContext(ctx))
	require.Zero(t, types.CallerFromContext(ctx).TenantID, "execution must not impersonate the tenant owner")
}

func TestWebCrawlTaskContextRejectsMismatchedScope(t *testing.T) {
	for _, scenario := range []string{
		"nil data source", "missing data source ID", "wrong connector", "missing tenant", "missing KB",
		"nil scan", "missing scan ID", "scan data source", "scan tenant", "payload tenant", "empty payload tenant",
		"missing KB service", "missing KB row", "KB ID", "KB tenant", "missing tenant row", "tenant row ID",
	} {
		t.Run(scenario, func(t *testing.T) {
			ds := &types.DataSource{ID: "ds", Type: types.ConnectorTypeWebCrawler, TenantID: 7, KnowledgeBaseID: "kb"}
			scan := &types.WebCrawlScan{ID: "scan", DataSourceID: "ds", TenantID: 7}
			kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
			tenants := &processSyncTenantRepo{tenant: &types.Tenant{ID: 7}}
			svc := &DataSourceService{kbService: &processSyncKBService{kb: kb}, tenantRepo: tenants}
			payloadTenant := uint64(7)
			switch scenario {
			case "nil data source":
				ds = nil
			case "missing data source ID":
				ds.ID = ""
			case "wrong connector":
				ds.Type = "other"
			case "missing tenant":
				ds.TenantID = 0
			case "missing KB":
				ds.KnowledgeBaseID = ""
			case "nil scan":
				scan = nil
			case "missing scan ID":
				scan.ID = ""
			case "scan data source":
				scan.DataSourceID = "other"
			case "scan tenant":
				scan.TenantID = 8
			case "payload tenant":
				payloadTenant = 8
			case "empty payload tenant":
				payloadTenant = 0
			case "missing KB service":
				svc.kbService = nil
			case "missing KB row":
				svc.kbService = &processSyncKBService{}
			case "KB ID":
				kb.ID = "other"
			case "KB tenant":
				kb.TenantID = 8
			case "missing tenant row":
				tenants.tenant = nil
			case "tenant row ID":
				tenants.tenant.ID = 8
			}
			_, err := svc.webCrawlTaskContext(context.Background(), ds, scan, payloadTenant)
			require.Error(t, err)
		})
	}
}
