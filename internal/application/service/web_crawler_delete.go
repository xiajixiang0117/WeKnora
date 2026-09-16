package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/webcrawler"
	"github.com/Tencent/WeKnora/internal/types"
)

// webCrawlTaskContext restores the admitted scan's actor and grants its worker
// write access to exactly the persisted data source's knowledge base.
func (s *DataSourceService) webCrawlTaskContext(
	ctx context.Context,
	ds *types.DataSource,
	scan *types.WebCrawlScan,
	payloadTenantID uint64,
) (context.Context, error) {
	if ds == nil || ds.ID == "" || ds.Type != types.ConnectorTypeWebCrawler ||
		ds.KnowledgeBaseID == "" || ds.TenantID == 0 || scan == nil || scan.ID == "" ||
		scan.DataSourceID != ds.ID || scan.TenantID != ds.TenantID || payloadTenantID != ds.TenantID {
		return ctx, fmt.Errorf("invalid web crawl task scope")
	}
	if s.kbService == nil {
		return ctx, fmt.Errorf("knowledge base service is not configured")
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, ds.KnowledgeBaseID)
	if err != nil {
		return ctx, err
	}
	if kb == nil || kb.ID != ds.KnowledgeBaseID || kb.TenantID != ds.TenantID {
		return ctx, fmt.Errorf("web crawl knowledge base binding changed")
	}
	ctx = (types.TaskInitiator{UserID: scan.InitiatorID}).Apply(ctx)
	ctx, err = access.WithKBTaskWrite(ctx, kb, ds.TenantID)
	if err != nil {
		return ctx, err
	}
	if s.tenantRepo != nil {
		tenant, err := s.tenantRepo.GetTenantByID(ctx, ds.TenantID)
		if err != nil {
			return ctx, err
		}
		if tenant == nil || tenant.ID != ds.TenantID {
			return ctx, fmt.Errorf("web crawl tenant binding changed")
		}
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	}
	return ctx, nil
}

// deleteWebCrawlPage only follows the crawler's persisted knowledge reference.
// Cleanup scope makes retry safe after the cascade has already soft-deleted
// the row, while pinning any still-live row to the original knowledge base.
func (s *DataSourceService) deleteWebCrawlPage(ctx context.Context, ds *types.DataSource, change *types.WebCrawlChange) error {
	if ds == nil || ds.ID == "" || ds.Type != types.ConnectorTypeWebCrawler ||
		ds.TenantID == 0 || ds.KnowledgeBaseID == "" || change == nil || change.PageID == "" ||
		change.CanonicalURL == "" || webcrawler.CanonicalURL(change.CanonicalURL) != change.CanonicalURL {
		return fmt.Errorf("invalid web crawl deletion scope")
	}
	if s.webCrawlerRepo == nil {
		return fmt.Errorf("web crawler repository is not configured")
	}
	page, err := s.webCrawlerRepo.FindPage(ctx, ds.ID, change.CanonicalURL)
	if err != nil {
		return err
	}
	if page == nil {
		return fmt.Errorf("web crawl page not found")
	}
	if page.DataSourceID != ds.ID || page.ID != change.PageID || page.CanonicalURL != change.CanonicalURL {
		return fmt.Errorf("web crawl page binding changed")
	}
	if page.Status == "deleted" && page.KnowledgeID == "" {
		return nil
	}
	if page.KnowledgeID != "" {
		if s.knowledgeService == nil || s.knowledgeService.GetRepository() == nil {
			return fmt.Errorf("knowledge service is not configured")
		}
		repo := s.knowledgeService.GetRepository()
		rows, err := repo.GetKnowledgeBatch(ctx, ds.TenantID, []string{page.KnowledgeID})
		if err != nil {
			return err
		}
		if len(rows) > 1 {
			return fmt.Errorf("ambiguous web crawl knowledge binding")
		}
		for _, knowledge := range rows {
			if knowledge == nil || knowledge.ID != page.KnowledgeID || knowledge.TenantID != ds.TenantID ||
				knowledge.KnowledgeBaseID != ds.KnowledgeBaseID {
				return fmt.Errorf("web crawl knowledge binding changed")
			}
			metadata := knowledge.GetMetadata()
			if metadata == nil {
				return fmt.Errorf("invalid web crawl knowledge metadata")
			}
			owner := metadata["datasource_id"]
			if owner != "" && owner != ds.ID {
				return fmt.Errorf("web crawl knowledge belongs to another data source")
			}
			var sourceURL string
			switch {
			case knowledge.Type == "url":
				sourceURL = knowledge.Source
			case knowledge.Type == "file" && owner == ds.ID:
				sourceURL = metadata["external_id"]
			}
			if webcrawler.CanonicalURL(sourceURL) != page.CanonicalURL {
				return fmt.Errorf("web crawl knowledge source binding changed")
			}
		}
		cleanupCtx := withKnowledgeCleanup(ctx, ds.TenantID, map[string]string{page.KnowledgeID: ds.KnowledgeBaseID})
		if err := s.knowledgeService.DeleteKnowledge(cleanupCtx, page.KnowledgeID); err != nil {
			return err
		}
		if err := repo.HardDeleteKnowledge(cleanupCtx, ds.TenantID, page.KnowledgeID); err != nil {
			return err
		}
	}
	// Do not mutate the loaded object until persistence succeeds: repositories
	// may retain it, and a failed update must preserve the reference for retry.
	updated := *page
	updated.Status = "deleted"
	updated.KnowledgeID = ""
	updated.UpdatedAt = time.Now().UTC()
	return s.webCrawlerRepo.UpdatePage(ctx, &updated)
}
