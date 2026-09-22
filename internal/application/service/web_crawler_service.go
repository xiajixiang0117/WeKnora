package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func (s *DataSourceService) ProcessWebCrawlScan(ctx context.Context, task *asynq.Task) (processErr error) {
	var payload types.WebCrawlScanPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return err
	}
	scan, err := s.GetWebCrawlScan(ctx, payload.ScanID)
	if err != nil {
		return err
	}
	if scan.Status != types.WebCrawlScanStatusScanning &&
		!(scan.Status == types.WebCrawlScanStatusPartialFailed && scan.ErrorMessage != "") {
		if payload.AutoApply && (scan.Status == types.WebCrawlScanStatusReviewReady ||
			scan.Status == types.WebCrawlScanStatusApplying || scan.Status == types.WebCrawlScanStatusPartialFailed) {
			return s.autoApplyWebCrawlChanges(ctx, &payload)
		}
		return nil
	}
	defer func() {
		if processErr == nil || scan == nil || scan.Status != types.WebCrawlScanStatusScanning {
			return
		}
		scan.Status = types.WebCrawlScanStatusPartialFailed
		attempt, _ := asynq.GetRetryCount(ctx)
		maximum, _ := asynq.GetMaxRetry(ctx)
		if payload.AutoApply && attempt < maximum {
			scan.Status = types.WebCrawlScanStatusScanning
		}
		scan.ErrorMessage = processErr.Error()
		scan.FinishedAt = timePtr(time.Now().UTC())
		scan.UpdatedAt = time.Now().UTC()
		if err := s.webCrawlerRepo.UpdateScan(ctx, scan); err != nil {
			logger.Errorf(ctx, "failed to mark web crawl scan %s as failed: %v", scan.ID, err)
		}
	}()
	ds, err := s.GetDataSource(ctx, payload.DataSourceID)
	if err != nil {
		return err
	}
	if payload.AutoApply && (ds.Status != types.DataSourceStatusActive || ds.SyncSchedule == "") {
		scan.Status = types.WebCrawlScanStatusCanceled
		scan.FinishedAt = timePtr(time.Now().UTC())
		return s.webCrawlerRepo.UpdateScan(ctx, scan)
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return err
	}
	ctx, err = s.webCrawlTaskContext(ctx, ds, scan, payload.TenantID)
	if err != nil {
		return err
	}
	if resumed, err := s.resumeInterruptedWebCrawlDeletions(ctx, ds, scan); err != nil || resumed {
		if err == nil && payload.AutoApply {
			return s.autoApplyWebCrawlChanges(ctx, &payload)
		}
		return err
	}
	pages, failures, crawlErr := webcrawler.NewConnector().Crawl(ctx, config)
	if crawlErr != nil {
		return crawlErr
	}
	cfg, err := webcrawler.ParseConfig(config)
	if err != nil {
		return err
	}
	baseline, err := s.webCrawlBaseline(ctx, ds, cfg, pages)
	if err != nil {
		return err
	}
	checked := make(map[string]bool, len(pages)+len(failures))
	for _, page := range pages {
		checked[page.CanonicalURL] = true
	}
	for _, failure := range failures {
		checked[failure.URL] = true
	}
	var recheckURLs []string
	for _, page := range baseline {
		if page.Status != "deleted" && (page.KnowledgeID != "" || page.LastAppliedHash != "") &&
			!checked[page.CanonicalURL] && webcrawler.AllowedURL(page.CanonicalURL, cfg) {
			recheckURLs = append(recheckURLs, page.CanonicalURL)
		}
	}
	if len(recheckURLs) > 0 {
		recheckedPages, recheckFailures, err := webcrawler.NewConnector().FetchPages(ctx, config, recheckURLs)
		if err != nil {
			return err
		}
		pages = append(pages, recheckedPages...)
		failures = append(failures, recheckFailures...)
		webcrawler.AssignFolderPaths(pages, cfg)
	}
	scan.ItemsTotal = len(pages) + len(failures)
	for _, page := range pages {
		now := time.Now().UTC()
		existing, findErr := s.webCrawlerRepo.FindPage(ctx, ds.ID, page.CanonicalURL)
		if findErr != nil {
			return findErr
		}
		if existing == nil {
			existing = &types.WebCrawlPage{DataSourceID: ds.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, Status: "active", LastSeenScanID: scan.ID, LastSeenAt: &now, ETag: page.ETag, LastModified: page.LastModified}
			knowledge, knowledgeErr := s.findWebCrawlKnowledge(ctx, ds, page.CanonicalURL)
			if knowledgeErr != nil {
				return knowledgeErr
			}
			if knowledge != nil {
				existing.KnowledgeID = knowledge.ID
				if !webCrawlKnowledgeNeedsRefresh(knowledge, page) {
					existing.LastAppliedHash = page.ContentHash
					existing.LastAppliedContent = page.Content
					existing.LastAppliedAt = &now
					if err := s.webCrawlerRepo.CreatePage(ctx, existing); err != nil {
						return err
					}
					scan.ItemsSkipped++
					continue
				}
			}
			if err := s.webCrawlerRepo.CreatePage(ctx, existing); err != nil {
				return err
			}
			if knowledge != nil {
				if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: existing.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, FolderPath: page.FolderPath, ChangeType: types.WebCrawlChangeUpdated, NewHash: page.ContentHash, NewContent: page.Content, Summary: "source metadata changed", SourceStatus: page.StatusCode}); err != nil {
					return err
				}
				scan.ItemsUpdated++
				continue
			}
			if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: existing.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, FolderPath: page.FolderPath, ChangeType: types.WebCrawlChangeAdded, NewHash: page.ContentHash, NewContent: page.Content, Summary: "new page", SourceStatus: page.StatusCode}); err != nil {
				return err
			}
			scan.ItemsAdded++
			continue
		}
		if existing.LastAppliedHash == "" && existing.KnowledgeID == "" {
			if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: existing.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, FolderPath: page.FolderPath, ChangeType: types.WebCrawlChangeAdded, NewHash: page.ContentHash, NewContent: page.Content, Summary: "new page", SourceStatus: page.StatusCode}); err != nil {
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
		needsMetadataRefresh, refreshErr := s.webCrawlKnowledgeNeedsRefresh(ctx, ds, page)
		if refreshErr != nil {
			return refreshErr
		}
		if existing.LastAppliedHash == page.ContentHash && !needsMetadataRefresh {
			scan.ItemsSkipped++
			continue
		}
		summary := "content changed"
		if existing.LastAppliedHash == page.ContentHash {
			summary = "source metadata changed"
		}
		if err := s.webCrawlerRepo.CreateChange(ctx, &types.WebCrawlChange{ScanID: scan.ID, PageID: existing.ID, CanonicalURL: page.CanonicalURL, Title: page.Title, FolderPath: page.FolderPath, ChangeType: types.WebCrawlChangeUpdated, OldHash: existing.LastAppliedHash, NewHash: page.ContentHash, PreviousContent: existing.LastAppliedContent, NewContent: page.Content, Summary: summary, SourceStatus: page.StatusCode}); err != nil {
			return err
		}
		scan.ItemsUpdated++
	}
	deletionsFailed := false
	for _, failure := range failures {
		change := &types.WebCrawlChange{ScanID: scan.ID, CanonicalURL: failure.URL, ChangeType: types.WebCrawlChangeFailed, SourceStatus: failure.SourceStatus, ErrorMessage: failure.Error(), Summary: "fetch failed"}
		if page := baseline[failure.URL]; page != nil {
			change.PageID = page.ID
			change.Title = page.Title
			if page.Status != "deleted" && (page.KnowledgeID != "" || page.LastAppliedHash != "") &&
				(failure.SourceStatus == http.StatusNotFound || failure.SourceStatus == http.StatusGone) {
				change.ChangeType = types.WebCrawlChangeMissing
				change.OldHash = page.LastAppliedHash
				change.PreviousContent = page.LastAppliedContent
				change.Summary = fmt.Sprintf("source page is unavailable (HTTP %d)", failure.SourceStatus)
				change.Action = "delete"
				change.Decision = types.WebCrawlDecisionApply
				change.ApplyStatus = types.WebCrawlApplyQueued
			}
		}
		if err := s.webCrawlerRepo.CreateChange(ctx, change); err != nil {
			return err
		}
		if change.ChangeType == types.WebCrawlChangeMissing {
			scan.ItemsMissing++
			// The scan has confirmed removal at the source. Persist the action
			// before deleting so both success and retryable failure stay visible.
			if err := s.finishAutomaticWebCrawlDeletion(ctx, ds, change); err != nil {
				return err
			}
			if change.ApplyStatus == types.WebCrawlApplyFailed {
				deletionsFailed = true
			} else {
				scan.ItemsApplied++
			}
		} else {
			scan.ItemsFailed++
		}
	}
	scan.Status = types.WebCrawlScanStatusReviewReady
	if scan.ItemsFailed > 0 || deletionsFailed {
		scan.Status = types.WebCrawlScanStatusPartialFailed
	} else if scan.ItemsAdded == 0 && scan.ItemsUpdated == 0 {
		scan.Status = types.WebCrawlScanStatusCompleted
	}
	scan.ErrorMessage = ""
	scan.FinishedAt = timePtr(time.Now().UTC())
	scan.UpdatedAt = time.Now().UTC()
	if err := s.webCrawlerRepo.UpdateScan(ctx, scan); err != nil {
		return err
	}
	if payload.AutoApply {
		return s.autoApplyWebCrawlChanges(ctx, &payload)
	}
	return nil
}

// Apply batches in this task so a failed enqueue cannot strand queued changes.
// Retried scans resume from persisted changes and never reapply successful ones.
func (s *DataSourceService) autoApplyWebCrawlChanges(ctx context.Context, payload *types.WebCrawlScanPayload) (applyErr error) {
	ds, err := s.GetDataSource(ctx, payload.DataSourceID)
	if err != nil {
		return err
	}
	scan, err := s.GetWebCrawlScan(ctx, payload.ScanID)
	if err != nil {
		return err
	}
	if _, err := s.webCrawlTaskContext(ctx, ds, scan, payload.TenantID); err != nil {
		return err
	}
	if ds.Status != types.DataSourceStatusActive || ds.SyncSchedule == "" {
		scan.Status = types.WebCrawlScanStatusCanceled
		scan.FinishedAt = timePtr(time.Now().UTC())
		return s.webCrawlerRepo.UpdateScan(ctx, scan)
	}
	defer func() {
		if applyErr != nil {
			if latest, err := s.GetWebCrawlScan(ctx, payload.ScanID); err == nil && latest != nil {
				scan = latest
			}
			scan.Status = types.WebCrawlScanStatusPartialFailed
			attempt, _ := asynq.GetRetryCount(ctx)
			maximum, _ := asynq.GetMaxRetry(ctx)
			if attempt < maximum {
				scan.Status = types.WebCrawlScanStatusApplying
			}
			// An empty scan error distinguishes apply retries from a failed crawl.
			scan.ErrorMessage = ""
			scan.FinishedAt = timePtr(time.Now().UTC())
			_ = s.webCrawlerRepo.UpdateScan(ctx, scan)
		}
	}()
	scan.Status = types.WebCrawlScanStatusApplying
	if err := s.webCrawlerRepo.UpdateScan(ctx, scan); err != nil {
		return err
	}
	var ids []string
	for offset := 0; ; offset += 1000 {
		changes, err := s.webCrawlerRepo.ListChanges(ctx, scan.ID, "", "", "", 1000, offset)
		if err != nil {
			return err
		}
		for _, change := range changes {
			if change.ApplyStatus == types.WebCrawlApplyApplied || change.Decision == types.WebCrawlDecisionIgnore {
				continue
			}
			confirmedDeletion := change.ChangeType == types.WebCrawlChangeMissing && change.Action == "delete" &&
				(change.SourceStatus == http.StatusNotFound || change.SourceStatus == http.StatusGone)
			if change.ChangeType != types.WebCrawlChangeAdded && change.ChangeType != types.WebCrawlChangeUpdated && !confirmedDeletion {
				continue
			}
			change.Decision = types.WebCrawlDecisionApply
			change.ApplyStatus = types.WebCrawlApplyQueued
			if err := s.webCrawlerRepo.UpdateChange(ctx, change); err != nil {
				return err
			}
			ids = append(ids, change.ID)
		}
		if len(changes) < 1000 {
			break
		}
	}
	// An empty batch also reconciles the scan's terminal status.
	for start := 0; start == 0 || start < len(ids); start += webCrawlApplyBatchSize {
		end := start + webCrawlApplyBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		data, _ := json.Marshal(&types.WebCrawlApplyPayload{TenantID: ds.TenantID, DataSourceID: ds.ID,
			ScanID: scan.ID, ChangeIDs: ids[start:end]})
		if err := s.ProcessWebCrawlApply(ctx, asynq.NewTask(types.TypeWebCrawlApply, data)); err != nil {
			return err
		}
	}
	failed, err := s.webCrawlerRepo.ListChanges(ctx, scan.ID, "", "", types.WebCrawlApplyFailed, 1, 0)
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("web crawl has failed changes")
	}
	return nil
}

func (s *DataSourceService) finishAutomaticWebCrawlDeletion(ctx context.Context, ds *types.DataSource, change *types.WebCrawlChange) error {
	if err := s.applyWebCrawlChange(ctx, ds, change, nil); err != nil {
		change.ApplyStatus = types.WebCrawlApplyFailed
		change.ErrorMessage = fmt.Sprintf("delete source knowledge: %v", err)
	} else {
		change.ApplyStatus = types.WebCrawlApplyApplied
		change.ErrorMessage = ""
		change.AppliedAt = timePtr(time.Now().UTC())
	}
	change.UpdatedAt = time.Now().UTC()
	return s.webCrawlerRepo.UpdateChange(ctx, change)
}

// A result-save error can leave a queued deletion without an apply task. Resume
// these persisted actions on scan redelivery instead of duplicating the scan's
// snapshots. Keep the scan failure visible: URLs after the interruption may
// still need a fresh check, even when every recorded deletion is now complete.
func (s *DataSourceService) resumeInterruptedWebCrawlDeletions(ctx context.Context, ds *types.DataSource, scan *types.WebCrawlScan) (bool, error) {
	if scan.Status != types.WebCrawlScanStatusPartialFailed || scan.ErrorMessage == "" {
		return false, nil
	}
	changes, err := s.webCrawlerRepo.ListChanges(ctx, scan.ID, "", "", "", 10000, 0)
	if err != nil {
		return false, err
	}
	resumed := false
	applied := 0
	for _, change := range changes {
		if change.ChangeType == types.WebCrawlChangeMissing && change.Action == "delete" &&
			change.Decision == types.WebCrawlDecisionApply &&
			(change.SourceStatus == http.StatusNotFound || change.SourceStatus == http.StatusGone) {
			resumed = true
			if change.ApplyStatus == types.WebCrawlApplyQueued {
				if err := s.finishAutomaticWebCrawlDeletion(ctx, ds, change); err != nil {
					return true, err
				}
			}
		}
		if change.ApplyStatus == types.WebCrawlApplyApplied && change.Decision == types.WebCrawlDecisionApply {
			applied++
		}
	}
	if resumed {
		scan.ItemsApplied = applied
		scan.UpdatedAt = time.Now().UTC()
		return true, s.webCrawlerRepo.UpdateScan(ctx, scan)
	}
	return false, nil
}

// webCrawlBaseline includes older URL imports even if their links have already
// disappeared from the site. Reconcile knowledge IDs here as re-importing a URL
// can replace its knowledge row without changing the crawler's page identity.
// Only crawler bookkeeping is updated; knowledge and applied content stay intact.
func (s *DataSourceService) webCrawlBaseline(ctx context.Context, ds *types.DataSource, cfg webcrawler.Config, discovered []webcrawler.Page) (map[string]*types.WebCrawlPage, error) {
	pages, err := s.webCrawlerRepo.ListPages(ctx, ds.ID)
	if err != nil {
		return nil, err
	}
	baseline := make(map[string]*types.WebCrawlPage, len(pages))
	for _, page := range pages {
		baseline[page.CanonicalURL] = page
	}
	knowledges, err := s.knowledgeService.GetRepository().ListKnowledgeByKnowledgeBaseID(ctx, ds.TenantID, ds.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	byURL := make(map[string]*types.Knowledge)
	for _, knowledge := range knowledges {
		if knowledge == nil || knowledge.DeletedAt.Valid || knowledge.ParseStatus == types.ParseStatusDeleting {
			continue
		}
		metadata := knowledge.GetMetadata()
		owner := metadata["datasource_id"]
		if owner != "" && owner != ds.ID {
			continue
		}
		rawURL := knowledge.Source
		if knowledge.Type != "url" {
			if owner != ds.ID {
				continue
			}
			rawURL = metadata["external_id"]
		}
		canonical := webcrawler.CanonicalURL(rawURL)
		if canonical == "" || !webcrawler.AllowedURL(canonical, cfg) {
			continue
		}
		if existing := byURL[canonical]; existing == nil || (existing.GetMetadata()["datasource_id"] == "" && owner == ds.ID) {
			byURL[canonical] = knowledge
		}
	}
	discoveredURLs := make(map[string]bool, len(discovered))
	for _, page := range discovered {
		discoveredURLs[page.CanonicalURL] = true
	}
	for canonical, knowledge := range byURL {
		if page := baseline[canonical]; page != nil {
			if page.KnowledgeID != knowledge.ID {
				page.KnowledgeID = knowledge.ID
				if page.Status == "deleted" {
					page.Status = "active"
				}
				page.UpdatedAt = time.Now().UTC()
				if err := s.webCrawlerRepo.UpdatePage(ctx, page); err != nil {
					return nil, err
				}
			}
			continue
		}
		// Successful discoveries use the normal adoption path below so they
		// can establish a content baseline when no prior hash was recorded.
		if discoveredURLs[canonical] {
			continue
		}
		page := &types.WebCrawlPage{DataSourceID: ds.ID, CanonicalURL: canonical, KnowledgeID: knowledge.ID, Title: knowledge.Title, Status: "active", LastAppliedHash: knowledge.GetMetadata()["content_hash"]}
		if knowledge.EnableStatus == "disabled" {
			page.Status = "disabled"
		}
		if err := s.webCrawlerRepo.CreatePage(ctx, page); err != nil {
			return nil, err
		}
		baseline[canonical] = page
	}
	return baseline, nil
}

func (s *DataSourceService) webCrawlKnowledgeNeedsRefresh(ctx context.Context, ds *types.DataSource, page webcrawler.Page) (bool, error) {
	knowledge, err := s.findWebCrawlKnowledge(ctx, ds, page.CanonicalURL)
	if err != nil {
		return false, err
	}
	return webCrawlKnowledgeNeedsRefresh(knowledge, page), nil
}

// findWebCrawlKnowledge resolves a crawled page to its existing knowledge item.
// Older URL imports do not have crawler metadata, so fall back to the URL's
// normal duplicate identity, always scoped to the same tenant and knowledge base.
func (s *DataSourceService) findWebCrawlKnowledge(ctx context.Context, ds *types.DataSource, canonicalURL string) (*types.Knowledge, error) {
	repo := s.knowledgeService.GetRepository()
	knowledge, err := repo.FindByDataSourceExternalID(ctx, ds.TenantID, ds.KnowledgeBaseID, ds.ID, canonicalURL)
	if err != nil || knowledge != nil {
		return knowledge, err
	}
	exists, knowledge, err := repo.CheckKnowledgeExists(ctx, ds.TenantID, ds.KnowledgeBaseID, &types.KnowledgeCheckParams{
		Type: "url",
		URL:  canonicalURL,
	})
	if err != nil || !exists {
		return nil, err
	}
	return knowledge, nil
}

func webCrawlKnowledgeNeedsRefresh(knowledge *types.Knowledge, page webcrawler.Page) bool {
	return knowledge == nil ||
		knowledge.Type != "url" ||
		knowledge.Source != page.CanonicalURL ||
		knowledge.FolderPath != page.FolderPath ||
		(strings.TrimSpace(page.Title) != "" &&
			strings.HasSuffix(strings.ToLower(strings.TrimSpace(knowledge.Title)), ".md") &&
			strings.TrimSpace(knowledge.Title) != strings.TrimSpace(page.Title))
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
	ctx, err = s.webCrawlTaskContext(ctx, ds, scan, payload.TenantID)
	if err != nil {
		return err
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
			change.ErrorMessage = ""
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
		if len(failedChanges) > 0 || scan.ItemsFailed > 0 || scan.ErrorMessage != "" {
			scan.Status = types.WebCrawlScanStatusPartialFailed
		} else {
			scan.Status = types.WebCrawlScanStatusCompleted
			pendingChanges, err := s.webCrawlerRepo.ListChanges(ctx, payload.ScanID, "", "", types.WebCrawlApplyPending, 10000, 0)
			if err != nil {
				return err
			}
			for _, pending := range pendingChanges {
				if pending.ChangeType != types.WebCrawlChangeFailed && pending.Decision != types.WebCrawlDecisionIgnore {
					scan.Status = types.WebCrawlScanStatusReviewReady
					break
				}
			}
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
			return s.deleteWebCrawlPage(ctx, ds, change)
		default:
			return fmt.Errorf("invalid missing action %q", action)
		}
	}
	if change.NewContent == "" {
		return errors.New("change has no captured content")
	}
	fileName := webcrawler.FileNameForPage(webcrawler.Page{Title: change.Title, FolderPath: change.FolderPath})
	item := &types.FetchedItem{ExternalID: change.CanonicalURL, Title: change.Title, Content: []byte(change.NewContent), ContentType: "text/markdown", FileName: fileName, URL: change.CanonicalURL, Metadata: map[string]string{"channel": types.ChannelWeb, "source_type": "url", "content_hash": change.NewHash}}
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
	if knowledge, findErr := s.findWebCrawlKnowledge(ctx, ds, change.CanonicalURL); findErr == nil && knowledge != nil {
		page.KnowledgeID = knowledge.ID
	}
	page.UpdatedAt = time.Now().UTC()
	return s.webCrawlerRepo.UpdatePage(ctx, page)
}
