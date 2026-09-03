package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// WebCrawlerRepository provides persistence for reviewable website scans.
type WebCrawlerRepository struct {
	db *gorm.DB
}

func NewWebCrawlerRepository(db *gorm.DB) interfaces.WebCrawlerRepository {
	return &WebCrawlerRepository{db: db}
}

func (r *WebCrawlerRepository) CreatePage(ctx context.Context, page *types.WebCrawlPage) error {
	if page == nil {
		return errors.New("web crawl page is nil")
	}
	return r.db.WithContext(ctx).Create(page).Error
}

func (r *WebCrawlerRepository) FindPage(ctx context.Context, dataSourceID, canonicalURL string) (*types.WebCrawlPage, error) {
	if dataSourceID == "" || canonicalURL == "" {
		return nil, errors.New("data source id and canonical url are required")
	}
	var page types.WebCrawlPage
	err := r.db.WithContext(ctx).
		Where("data_source_id = ? AND canonical_url = ?", dataSourceID, canonicalURL).
		First(&page).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &page, err
}

func (r *WebCrawlerRepository) ListPages(ctx context.Context, dataSourceID string) ([]*types.WebCrawlPage, error) {
	if dataSourceID == "" {
		return nil, errors.New("data source id is required")
	}
	var pages []*types.WebCrawlPage
	err := r.db.WithContext(ctx).
		Where("data_source_id = ?", dataSourceID).
		Order("id ASC").
		Find(&pages).Error
	return pages, err
}

func (r *WebCrawlerRepository) UpdatePage(ctx context.Context, page *types.WebCrawlPage) error {
	if page == nil || page.ID == "" {
		return errors.New("web crawl page and id are required")
	}
	return r.db.WithContext(ctx).
		Model(&types.WebCrawlPage{}).
		Where("id = ?", page.ID).
		Updates(map[string]interface{}{
			"knowledge_id":         page.KnowledgeID,
			"title":                page.Title,
			"last_applied_hash":    page.LastAppliedHash,
			"last_applied_content": page.LastAppliedContent,
			"last_applied_at":      page.LastAppliedAt,
			"last_seen_scan_id":    page.LastSeenScanID,
			"last_seen_at":         page.LastSeenAt,
			"etag":                 page.ETag,
			"last_modified":        page.LastModified,
			"status":               page.Status,
			"updated_at":           page.UpdatedAt,
		}).Error
}

func (r *WebCrawlerRepository) CreateScan(ctx context.Context, scan *types.WebCrawlScan) error {
	if scan == nil {
		return errors.New("web crawl scan is nil")
	}
	return r.db.WithContext(ctx).Create(scan).Error
}

func (r *WebCrawlerRepository) FindScan(ctx context.Context, id string) (*types.WebCrawlScan, error) {
	if id == "" {
		return nil, errors.New("scan id is required")
	}
	var scan types.WebCrawlScan
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&scan).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &scan, err
}

func (r *WebCrawlerRepository) ListScans(ctx context.Context, dataSourceID string, limit, offset int) ([]*types.WebCrawlScan, error) {
	if dataSourceID == "" {
		return nil, errors.New("data source id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	var scans []*types.WebCrawlScan
	err := r.db.WithContext(ctx).
		Where("data_source_id = ?", dataSourceID).
		Order("started_at DESC").
		Limit(limit).Offset(offset).
		Find(&scans).Error
	return scans, err
}

func (r *WebCrawlerRepository) HasRunningScan(ctx context.Context, dataSourceID string) (bool, error) {
	if dataSourceID == "" {
		return false, errors.New("data source id is required")
	}
	var count int64
	err := r.db.WithContext(ctx).
		Model(&types.WebCrawlScan{}).
		Where("data_source_id = ? AND status IN ?", dataSourceID,
			[]string{types.WebCrawlScanStatusScanning, types.WebCrawlScanStatusApplying}).
		Count(&count).Error
	return count > 0, err
}

func (r *WebCrawlerRepository) UpdateScan(ctx context.Context, scan *types.WebCrawlScan) error {
	if scan == nil || scan.ID == "" {
		return errors.New("web crawl scan and id are required")
	}
	return r.db.WithContext(ctx).
		Model(&types.WebCrawlScan{}).
		Where("id = ?", scan.ID).
		Updates(map[string]interface{}{
			"status":        scan.Status,
			"finished_at":   scan.FinishedAt,
			"items_total":   scan.ItemsTotal,
			"items_added":   scan.ItemsAdded,
			"items_updated": scan.ItemsUpdated,
			"items_missing": scan.ItemsMissing,
			"items_failed":  scan.ItemsFailed,
			"items_skipped": scan.ItemsSkipped,
			"items_applied": scan.ItemsApplied,
			"items_ignored": scan.ItemsIgnored,
			"error_message": scan.ErrorMessage,
			"updated_at":    scan.UpdatedAt,
		}).Error
}

func (r *WebCrawlerRepository) CreateChange(ctx context.Context, change *types.WebCrawlChange) error {
	if change == nil {
		return errors.New("web crawl change is nil")
	}
	return r.db.WithContext(ctx).Create(change).Error
}

func (r *WebCrawlerRepository) FindChange(ctx context.Context, id string) (*types.WebCrawlChange, error) {
	if id == "" {
		return nil, errors.New("change id is required")
	}
	var change types.WebCrawlChange
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&change).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &change, err
}

func (r *WebCrawlerRepository) ListChanges(ctx context.Context, scanID, changeType, decision, applyStatus string, limit, offset int) ([]*types.WebCrawlChange, error) {
	if scanID == "" {
		return nil, errors.New("scan id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 10000 {
		limit = 10000
	}
	if offset < 0 {
		offset = 0
	}
	query := r.db.WithContext(ctx).Where("scan_id = ?", scanID)
	if changeType != "" {
		query = query.Where("change_type = ?", changeType)
	}
	if decision != "" {
		query = query.Where("decision = ?", decision)
	}
	if applyStatus != "" {
		query = query.Where("apply_status = ?", applyStatus)
	}
	var changes []*types.WebCrawlChange
	err := query.Order("created_at ASC").Limit(limit).Offset(offset).Find(&changes).Error
	return changes, err
}

func (r *WebCrawlerRepository) ListChangesByIDs(ctx context.Context, scanID string, ids []string) ([]*types.WebCrawlChange, error) {
	if scanID == "" || len(ids) == 0 {
		return []*types.WebCrawlChange{}, nil
	}
	var changes []*types.WebCrawlChange
	err := r.db.WithContext(ctx).
		Where("scan_id = ? AND id IN ?", scanID, ids).
		Order("created_at ASC").
		Find(&changes).Error
	return changes, err
}

func (r *WebCrawlerRepository) UpdateChange(ctx context.Context, change *types.WebCrawlChange) error {
	if change == nil || change.ID == "" {
		return errors.New("web crawl change and id are required")
	}
	// Locking is performed by the service before application. Keeping a
	// conditional update here makes retries idempotent if another worker wins.
	return r.db.WithContext(ctx).
		Model(&types.WebCrawlChange{}).
		Where("id = ?", change.ID).
		Updates(map[string]interface{}{
			"decision":      change.Decision,
			"action":        change.Action,
			"apply_status":  change.ApplyStatus,
			"error_message": change.ErrorMessage,
			"applied_at":    change.AppliedAt,
			"updated_at":    change.UpdatedAt,
		}).Error
}
