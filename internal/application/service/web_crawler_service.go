package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/webcrawler"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

const webCrawlApplyBatchSize = 50

func (s *DataSourceService) CreateWebCrawlScan(ctx context.Context, dsID, initiatorID string) (*types.WebCrawlScan, error) {
	if s.webCrawlerRepo == nil {
		return nil, fmt.Errorf("web crawler repository is not configured")
	}
	if s.taskEnqueuer == nil {
		return nil, fmt.Errorf("task enqueuer is not configured")
	}
	ds, err := s.GetDataSource(ctx, dsID)
	if err != nil {
		return nil, err
	}
	if ds.Type != types.ConnectorTypeWebCrawler {
		return nil, fmt.Errorf("data source is not a web crawler")
	}
	if running, err := s.webCrawlerRepo.HasRunningScan(ctx, dsID); err != nil {
		return nil, err
	} else if running {
		return nil, fmt.Errorf("a web crawl scan is already running")
	}
	if initiatorID == "" {
		initiatorID = types.TaskInitiatorFromContext(ctx).UserID
	}
	scan := &types.WebCrawlScan{DataSourceID: dsID, TenantID: ds.TenantID, InitiatorID: initiatorID, Status: types.WebCrawlScanStatusScanning, StartedAt: time.Now().UTC()}
	if err := s.webCrawlerRepo.CreateScan(ctx, scan); err != nil {
		return nil, err
	}
	payload := &types.WebCrawlScanPayload{TenantID: ds.TenantID, DataSourceID: dsID, ScanID: scan.ID, InitiatorID: initiatorID}
	data, _ := json.Marshal(payload)
	task := asynq.NewTask(types.TypeWebCrawlScan, data, asynq.Queue(types.QueueSync), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
	if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
		scan.Status = types.WebCrawlScanStatusCanceled
		scan.ErrorMessage = err.Error()
		scan.FinishedAt = timePtr(time.Now().UTC())
		_ = s.webCrawlerRepo.UpdateScan(ctx, scan)
		return nil, err
	}
	return scan, nil
}

func (s *DataSourceService) ListWebCrawlScans(ctx context.Context, dsID string, limit, offset int) ([]*types.WebCrawlScan, error) {
	if s.webCrawlerRepo == nil {
		return nil, fmt.Errorf("web crawler repository is not configured")
	}
	return s.webCrawlerRepo.ListScans(ctx, dsID, limit, offset)
}

func (s *DataSourceService) GetWebCrawlScan(ctx context.Context, scanID string) (*types.WebCrawlScan, error) {
	if s.webCrawlerRepo == nil {
		return nil, fmt.Errorf("web crawler repository is not configured")
	}
	scan, err := s.webCrawlerRepo.FindScan(ctx, scanID)
	if err != nil {
		return nil, err
	}
	if scan == nil {
		return nil, fmt.Errorf("web crawl scan not found")
	}
	return scan, nil
}

func (s *DataSourceService) ListWebCrawlChanges(ctx context.Context, scanID, changeType, decision, applyStatus string, limit, offset int) ([]*types.WebCrawlChange, error) {
	if s.webCrawlerRepo == nil {
		return nil, fmt.Errorf("web crawler repository is not configured")
	}
	return s.webCrawlerRepo.ListChanges(ctx, scanID, changeType, decision, applyStatus, limit, offset)
}

func (s *DataSourceService) ApplyWebCrawlChanges(ctx context.Context, scanID string, changeIDs []string, missingActions map[string]string) error {
	if s.webCrawlerRepo == nil {
		return fmt.Errorf("web crawler repository is not configured")
	}
	if s.taskEnqueuer == nil {
		return fmt.Errorf("task enqueuer is not configured")
	}
	scan, err := s.GetWebCrawlScan(ctx, scanID)
	if err != nil {
		return err
	}
	if scan.Status != types.WebCrawlScanStatusReviewReady && scan.Status != types.WebCrawlScanStatusPartialFailed {
		return fmt.Errorf("scan is not ready for applying changes")
	}
	if len(changeIDs) == 0 {
		return fmt.Errorf("at least one change is required")
	}
	ds, err := s.GetDataSource(ctx, scan.DataSourceID)
	if err != nil {
		return err
	}
	changes, err := s.webCrawlerRepo.ListChangesByIDs(ctx, scanID, changeIDs)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return fmt.Errorf("no changes found")
	}
	queued := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.ApplyStatus == types.WebCrawlApplyApplied || change.ApplyStatus == types.WebCrawlApplyQueued {
			continue
		}
		if change.ChangeType == types.WebCrawlChangeMissing {
			action := strings.ToLower(strings.TrimSpace(missingActions[change.ID]))
			if action == "" {
				action = "keep"
			}
			if action != "keep" && action != "disable" && action != "delete" {
				return fmt.Errorf("invalid missing action %q", action)
			}
			change.Action = action
		}
		change.Decision = types.WebCrawlDecisionApply
		change.ApplyStatus = types.WebCrawlApplyQueued
		change.UpdatedAt = time.Now().UTC()
		if err := s.webCrawlerRepo.UpdateChange(ctx, change); err != nil {
			return err
		}
		queued = append(queued, change.ID)
	}
	if len(queued) == 0 {
		return nil
	}
	for start := 0; start < len(queued); start += webCrawlApplyBatchSize {
		end := start + webCrawlApplyBatchSize
		if end > len(queued) {
			end = len(queued)
		}
		payload := &types.WebCrawlApplyPayload{TenantID: ds.TenantID, DataSourceID: ds.ID, ScanID: scanID, ChangeIDs: queued[start:end], MissingActions: missingActions}
		data, _ := json.Marshal(payload)
		task := asynq.NewTask(types.TypeWebCrawlApply, data, asynq.Queue(types.QueueSync), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
		if _, err := s.taskEnqueuer.Enqueue(task); err != nil {
			return err
		}
	}
	scan.Status = types.WebCrawlScanStatusApplying
	scan.UpdatedAt = time.Now().UTC()
	return s.webCrawlerRepo.UpdateScan(ctx, scan)
}

func (s *DataSourceService) RetryWebCrawlChanges(ctx context.Context, scanID string, changeIDs []string) error {
	if s.webCrawlerRepo == nil {
		return fmt.Errorf("web crawler repository is not configured")
	}
	changes, err := s.webCrawlerRepo.ListChangesByIDs(ctx, scanID, changeIDs)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(changes))
	actions := make(map[string]string)
	for _, change := range changes {
		if change.ApplyStatus == types.WebCrawlApplyFailed {
			ids = append(ids, change.ID)
			if change.Action != "" {
				actions[change.ID] = change.Action
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return s.ApplyWebCrawlChanges(ctx, scanID, ids, actions)
}

func (s *DataSourceService) IgnoreWebCrawlChanges(ctx context.Context, scanID string, changeIDs []string) error {
	if s.webCrawlerRepo == nil {
		return fmt.Errorf("web crawler repository is not configured")
	}
	changes, err := s.webCrawlerRepo.ListChangesByIDs(ctx, scanID, changeIDs)
	if err != nil {
		return err
	}
	for _, change := range changes {
		change.Decision = types.WebCrawlDecisionIgnore
		change.ApplyStatus = types.WebCrawlApplyApplied
		change.UpdatedAt = time.Now().UTC()
		if err := s.webCrawlerRepo.UpdateChange(ctx, change); err != nil {
			return err
		}
	}
	return nil
}

func (s *DataSourceService) ProcessWebCrawlScan(ctx context.Context, task *asynq.Task) error {
	var payload types.WebCrawlScanPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	scan, err := s.GetWebCrawlScan(ctx, payload.ScanID)
	if err != nil {
		return err
	}
	ds, err := s.GetDataSource(ctx, payload.DataSourceID)
	if err != nil {
		return err
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return err
	}
	pages, failures, crawlErr := webcrawler.NewConnector().Crawl(ctx, config)
	if crawlErr != nil {
		scan.Status = types.WebCrawlScanStatusPartialFailed
		scan.ErrorMessage = crawlErr.Error()
		scan.FinishedAt = timePtr(time.Now().UTC())
		_ = s.webCrawlerRepo.UpdateScan(ctx, scan)
		return crawlErr
	}
	baseline, err := s.webCrawlerRepo.ListPages(ctx, ds.ID)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(pages))
	for _, page := range pages {
		seen[page.CanonicalURL] = struct{}{}
		now := time.Now().UTC()
		existing, findErr := s.webCrawlerRepo.FindPage(ctx, ds.ID, page.CanonicalURL)
		if findErr != nil {
			return findErr
		}
		if existing == nil {
			existing = &types.WebCrawlPage{DataSourceID: ds.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, Status: "active", LastSeenScanID: scan.ID, LastSeenAt: &now, ETag: page.ETag, LastModified: page.LastModified}
			if err := s.webCrawlerRepo.CreatePage(ctx, existing); err != nil {
				return err
			}
			if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: existing.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, ChangeType: types.WebCrawlChangeAdded, NewHash: page.ContentHash, NewContent: page.Content, Summary: "new page", SourceStatus: page.StatusCode}); err != nil {
				return err
			}
			scan.ItemsAdded++
			continue
		}
		if existing.LastAppliedHash == "" && existing.KnowledgeID == "" {
			if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: existing.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, ChangeType: types.WebCrawlChangeAdded, NewHash: page.ContentHash, NewContent: page.Content, Summary: "new page", SourceStatus: page.StatusCode}); err != nil {
				return err
			}
			scan.ItemsAdded++
			existing.Title = page.Title
			existing.LastSeenScanID = scan.ID
			existing.LastSeenAt = &now
			existing.ETag = page.ETag
			existing.LastModified = page.LastModified
			existing.UpdatedAt = now
			if err := s.webCrawlerRepo.UpdatePage(ctx, existing); err != nil {
				return err
			}
			continue
		}
		existing.Title = page.Title
		existing.LastSeenScanID = scan.ID
		existing.LastSeenAt = &now
		existing.ETag = page.ETag
		existing.LastModified = page.LastModified
		existing.UpdatedAt = now
		if err := s.webCrawlerRepo.UpdatePage(ctx, existing); err != nil {
			return err
		}
		if existing.LastAppliedHash == page.ContentHash {
			scan.ItemsSkipped++
			continue
		}
		if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: existing.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, ChangeType: types.WebCrawlChangeUpdated, OldHash: existing.LastAppliedHash, NewHash: page.ContentHash, PreviousContent: existing.LastAppliedContent, NewContent: page.Content, Summary: "content changed", SourceStatus: page.StatusCode}); err != nil {
			return err
		}
		scan.ItemsUpdated++
	}
	failedURLs := make(map[string]struct{}, len(failures))
	for _, failure := range failures {
		failedURLs[failure.URL] = struct{}{}
		if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, CanonicalURL: failure.URL, ChangeType: types.WebCrawlChangeFailed, SourceStatus: failure.SourceStatus, ErrorMessage: failure.Error(), Summary: "fetch failed"}); err != nil {
			return err
		}
		scan.ItemsFailed++
	}
	for _, page := range baseline {
		if page.Status != "active" {
			continue
		}
		if _, ok := seen[page.CanonicalURL]; ok {
			continue
		}
		if _, failed := failedURLs[page.CanonicalURL]; failed {
			continue
		}
		if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: page.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, ChangeType: types.WebCrawlChangeMissing, OldHash: page.LastAppliedHash, PreviousContent: page.LastAppliedContent, Summary: "page was not found in the scan"}); err != nil {
			return err
		}
		scan.ItemsMissing++
	}
	scan.ItemsTotal = len(pages) + len(failures)
	scan.Status = types.WebCrawlScanStatusReviewReady
	scan.FinishedAt = timePtr(time.Now().UTC())
	scan.UpdatedAt = time.Now().UTC()
	return s.webCrawlerRepo.UpdateScan(ctx, scan)
}

func (s *DataSourceService) ProcessWebCrawlApply(ctx context.Context, task *asynq.Task) error {
	var payload types.WebCrawlApplyPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	scan, err := s.GetWebCrawlScan(ctx, payload.ScanID)
	if err != nil {
		return err
	}
	ds, err := s.GetDataSource(ctx, payload.DataSourceID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, ds.TenantID)
	if s.tenantRepo != nil {
		tenant, tenantErr := s.tenantRepo.GetTenantByID(ctx, ds.TenantID)
		if tenantErr != nil {
			return tenantErr
		}
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	}
	changes, err := s.webCrawlerRepo.ListChangesByIDs(ctx, payload.ScanID, payload.ChangeIDs)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if change.ApplyStatus == types.WebCrawlApplyApplied {
			continue
		}
		if applyErr := s.applyWebCrawlChange(ctx, ds, change, payload.MissingActions); applyErr != nil {
			change.ApplyStatus = types.WebCrawlApplyFailed
			change.ErrorMessage = applyErr.Error()
			scan.ItemsIgnored++
			logger.Warnf(ctx, "web crawl change %s failed: %v", change.ID, applyErr)
		} else {
			change.ApplyStatus = types.WebCrawlApplyApplied
			change.AppliedAt = timePtr(time.Now().UTC())
			scan.ItemsApplied++
		}
		change.UpdatedAt = time.Now().UTC()
		if err := s.webCrawlerRepo.UpdateChange(ctx, change); err != nil {
			return err
		}
	}
	allChanges, listErr := s.webCrawlerRepo.ListChanges(ctx, payload.ScanID, "", "", types.WebCrawlApplyQueued, 10000, 0)
	if listErr != nil {
		return listErr
	}
	if len(allChanges) == 0 {
		failedChanges, listErr := s.webCrawlerRepo.ListChanges(ctx, payload.ScanID, "", "", types.WebCrawlApplyFailed, 10000, 0)
		if listErr != nil {
			return listErr
		}
		if len(failedChanges) > 0 {
			scan.Status = types.WebCrawlScanStatusPartialFailed
		} else {
			scan.Status = types.WebCrawlScanStatusCompleted
		}
		scan.FinishedAt = timePtr(time.Now().UTC())
	}
	scan.UpdatedAt = time.Now().UTC()
	return s.webCrawlerRepo.UpdateScan(ctx, scan)
}

func (s *DataSourceService) applyWebCrawlChange(ctx context.Context, ds *types.DataSource, change *types.WebCrawlChange, missingActions map[string]string) error {
	if change.ChangeType == types.WebCrawlChangeMissing {
		action := change.Action
		if action == "" {
			action = missingActions[change.ID]
		}
		switch action {
		case "keep", "":
			return nil
		case "disable":
			if change.PageID == "" {
				return nil
			}
			page, err := s.webCrawlerRepo.FindPage(ctx, ds.ID, change.CanonicalURL)
			if err != nil || page == nil || page.KnowledgeID == "" {
				return err
			}
			if err := s.knowledgeService.GetRepository().UpdateKnowledgeColumn(ctx, page.KnowledgeID, "enable_status", "disabled"); err != nil {
				return err
			}
			page.Status = "disabled"
			page.UpdatedAt = time.Now().UTC()
			return s.webCrawlerRepo.UpdatePage(ctx, page)
		case "delete":
			page, err := s.webCrawlerRepo.FindPage(ctx, ds.ID, change.CanonicalURL)
			if err != nil || page == nil || page.KnowledgeID == "" {
				return err
			}
			if err := s.knowledgeService.DeleteKnowledge(ctx, page.KnowledgeID); err != nil {
				return err
			}
			if err := s.knowledgeService.GetRepository().HardDeleteKnowledge(ctx, ds.TenantID, page.KnowledgeID); err != nil {
				return err
			}
			page.Status = "deleted"
			page.KnowledgeID = ""
			page.UpdatedAt = time.Now().UTC()
			return s.webCrawlerRepo.UpdatePage(ctx, page)
		default:
			return fmt.Errorf("invalid missing action %q", action)
		}
	}
	if change.NewContent == "" {
		return errors.New("change has no captured content")
	}
	item := &types.FetchedItem{ExternalID: change.CanonicalURL, Title: change.Title, Content: []byte(change.NewContent), ContentType: "text/markdown", FileName: change.Title + ".md", URL: change.CanonicalURL, Metadata: map[string]string{"channel": types.ChannelWeb, "content_hash": change.NewHash}}
	_, err := s.ingestItem(withKBActivitySuppressed(ctx), ds, item, nil)
	if err != nil {
		return err
	}
	page, err := s.webCrawlerRepo.FindPage(ctx, ds.ID, change.CanonicalURL)
	if err != nil || page == nil {
		return err
	}
	page.LastAppliedHash = change.NewHash
	page.LastAppliedContent = change.NewContent
	page.LastAppliedAt = timePtr(time.Now().UTC())
	page.Status = "active"
	if knowledge, findErr := s.knowledgeService.GetRepository().FindByDataSourceExternalID(ctx, ds.TenantID, ds.KnowledgeBaseID, ds.ID, change.CanonicalURL); findErr == nil && knowledge != nil {
		page.KnowledgeID = knowledge.ID
	}
	page.UpdatedAt = time.Now().UTC()
	return s.webCrawlerRepo.UpdatePage(ctx, page)
}
