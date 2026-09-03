package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

// DataSourceService defines the interface for data source management operations
type DataSourceService interface {
	// CreateDataSource creates a new data source configuration
	CreateDataSource(ctx context.Context, ds *types.DataSource) (*types.DataSource, error)

	// GetDataSource retrieves a data source by ID
	GetDataSource(ctx context.Context, id string) (*types.DataSource, error)

	// ListDataSources lists all data sources for a knowledge base
	ListDataSources(ctx context.Context, kbID string) ([]*types.DataSource, error)

	// UpdateDataSource updates an existing data source
	UpdateDataSource(ctx context.Context, ds *types.DataSource) (*types.DataSource, error)

	// DeleteDataSource deletes a data source (soft delete)
	DeleteDataSource(ctx context.Context, id string) error

	// UpdateDataSourceCredentials replaces the connector credential map.
	// DataSource credentials are per-connector atomic — there is no
	// individual-field PUT, the whole map gets replaced. Returns the updated
	// data source so the caller can re-fetch the redacted shape.
	UpdateDataSourceCredentials(
		ctx context.Context, id string, credentials map[string]interface{},
	) (*types.DataSource, error)

	// ClearDataSourceCredentials wipes the connector credential map.
	// Idempotent on already-empty credentials.
	ClearDataSourceCredentials(ctx context.Context, id string) error

	// ValidateConnection tests the connection to an external data source
	ValidateConnection(ctx context.Context, dsID string) error

	// ValidateCredentials tests connectivity using raw credentials without persisting anything.
	// This is used by the frontend "Test Connection" button before creating a data source.
	ValidateCredentials(ctx context.Context, connectorType string, credentials map[string]interface{}) error

	// ListAvailableResources lists resources available for sync in the external system.
	// parentID enables lazy loading: "" lists the top level, a resource ExternalID lists its children.
	ListAvailableResources(ctx context.Context, dsID string, parentID string) ([]types.Resource, error)

	// ResolveResourceAncestors returns the deduplicated ExternalIDs of every
	// ancestor that must be expanded to reveal the given (possibly deeply nested)
	// resources in a lazily-loaded picker. Used to restore an existing selection
	// when editing a data source.
	ResolveResourceAncestors(ctx context.Context, dsID string, resourceIDs []string) ([]string, error)

	// ManualSync triggers an immediate sync for a data source
	ManualSync(ctx context.Context, dsID string) (*types.SyncLog, error)

	// PauseDataSource pauses a data source's scheduled syncs
	PauseDataSource(ctx context.Context, id string) error

	// ResumeDataSource resumes a paused data source
	ResumeDataSource(ctx context.Context, id string) error

	// GetSyncLogs retrieves sync history for a data source
	GetSyncLogs(ctx context.Context, dsID string, limit int, offset int) ([]*types.SyncLog, error)

	// GetSyncLog retrieves a specific sync log entry
	GetSyncLog(ctx context.Context, syncLogID string) (*types.SyncLog, error)

	// ProcessSync handles the actual sync operation (called by asynq task)
	ProcessSync(ctx context.Context, task *asynq.Task) error

	// CreateWebCrawlScan starts a review-only website discovery run.
	CreateWebCrawlScan(ctx context.Context, dsID, initiatorID string) (*types.WebCrawlScan, error)
	// ListWebCrawlScans lists reviewable website scans for a data source.
	ListWebCrawlScans(ctx context.Context, dsID string, limit, offset int) ([]*types.WebCrawlScan, error)
	GetWebCrawlScan(ctx context.Context, scanID string) (*types.WebCrawlScan, error)
	ListWebCrawlChanges(ctx context.Context, scanID, changeType, decision, applyStatus string, limit, offset int) ([]*types.WebCrawlChange, error)
	ApplyWebCrawlChanges(ctx context.Context, scanID string, changeIDs []string, missingActions map[string]string) error
	IgnoreWebCrawlChanges(ctx context.Context, scanID string, changeIDs []string) error
	RetryWebCrawlChanges(ctx context.Context, scanID string, changeIDs []string) error
	ProcessWebCrawlScan(ctx context.Context, task *asynq.Task) error
	ProcessWebCrawlApply(ctx context.Context, task *asynq.Task) error
}

// DataSourceRepository defines database access patterns for data sources
type DataSourceRepository interface {
	// Create inserts a new data source record
	Create(ctx context.Context, ds *types.DataSource) error

	// FindByID retrieves a data source by ID
	FindByID(ctx context.Context, id string) (*types.DataSource, error)

	// FindByKnowledgeBase lists all data sources for a knowledge base
	FindByKnowledgeBase(ctx context.Context, kbID string) ([]*types.DataSource, error)

	// Update updates an existing data source
	Update(ctx context.Context, ds *types.DataSource) error

	// UpdateSyncState updates only fields produced by a sync run.
	UpdateSyncState(ctx context.Context, ds *types.DataSource) error

	// Delete performs a soft delete
	Delete(ctx context.Context, id string) error

	// FindActive retrieves all active data sources (used for scheduling)
	FindActive(ctx context.Context) ([]*types.DataSource, error)
}

// SyncLogRepository defines database access patterns for sync logs
type SyncLogRepository interface {
	// Create inserts a new sync log entry
	Create(ctx context.Context, log *types.SyncLog) error

	// FindByID retrieves a sync log by ID
	FindByID(ctx context.Context, id string) (*types.SyncLog, error)

	// FindByDataSource lists sync logs for a data source with pagination
	FindByDataSource(ctx context.Context, dsID string, limit int, offset int) ([]*types.SyncLog, error)

	// FindLatest retrieves the most recent sync log for a data source
	FindLatest(ctx context.Context, dsID string) (*types.SyncLog, error)

	// HasRunningSync checks if a data source has any sync currently in "running" status.
	// Used to prevent overlapping sync executions.
	HasRunningSync(ctx context.Context, dsID string) (bool, error)

	// Update updates an existing sync log entry
	Update(ctx context.Context, log *types.SyncLog) error

	// UpdateResult updates only fields produced by a sync run.
	UpdateResult(ctx context.Context, log *types.SyncLog) error

	// CancelPendingByDataSource marks all non-terminal sync logs for a data source as canceled.
	CancelPendingByDataSource(ctx context.Context, dsID string) error

	// CleanupOldLogs deletes sync logs older than the retention period
	CleanupOldLogs(ctx context.Context, retentionDays int) error
}

// WebCrawlerRepository stores website crawl baselines, review batches and
// immutable change snapshots.
type WebCrawlerRepository interface {
	CreatePage(ctx context.Context, page *types.WebCrawlPage) error
	FindPage(ctx context.Context, dataSourceID, canonicalURL string) (*types.WebCrawlPage, error)
	ListPages(ctx context.Context, dataSourceID string) ([]*types.WebCrawlPage, error)
	UpdatePage(ctx context.Context, page *types.WebCrawlPage) error

	CreateScan(ctx context.Context, scan *types.WebCrawlScan) error
	FindScan(ctx context.Context, id string) (*types.WebCrawlScan, error)
	ListScans(ctx context.Context, dataSourceID string, limit, offset int) ([]*types.WebCrawlScan, error)
	HasRunningScan(ctx context.Context, dataSourceID string) (bool, error)
	UpdateScan(ctx context.Context, scan *types.WebCrawlScan) error

	CreateChange(ctx context.Context, change *types.WebCrawlChange) error
	FindChange(ctx context.Context, id string) (*types.WebCrawlChange, error)
	ListChanges(ctx context.Context, scanID, changeType, decision, applyStatus string, limit, offset int) ([]*types.WebCrawlChange, error)
	ListChangesByIDs(ctx context.Context, scanID string, ids []string) ([]*types.WebCrawlChange, error)
	UpdateChange(ctx context.Context, change *types.WebCrawlChange) error
}
