package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestValidateSchedule(t *testing.T) {
	for _, spec := range []string{"", "*/15 * * * *", "0 0 */6 * * *", "CRON_TZ=Asia/Shanghai 0 2 * * *", "@daily"} {
		require.NoError(t, ValidateSchedule(spec), spec)
	}
	for _, spec := range []string{"invalid", "60 * * * *", "0 25 * * *", "0 9 31 2 *", "CRON_TZ=Invalid/Zone 0 2 * * *"} {
		require.Error(t, ValidateSchedule(spec), spec)
	}
}

type scheduledCrawlRepo struct {
	interfaces.WebCrawlerRepository
	scan    *types.WebCrawlScan
	running bool
	readErr error
}

func (r *scheduledCrawlRepo) HasRunningScan(context.Context, string) (bool, error) {
	return r.running, r.readErr
}
func (r *scheduledCrawlRepo) CreateScan(_ context.Context, scan *types.WebCrawlScan) error {
	if r.scan != nil && r.scan.ID == scan.ID {
		return errors.New("duplicate scan")
	}
	r.scan = scan
	return nil
}
func (r *scheduledCrawlRepo) UpdateScan(_ context.Context, scan *types.WebCrawlScan) error {
	r.scan = scan
	return nil
}

type scheduledCrawlEnqueuer struct {
	tasks []*asynq.Task
	err   error
}

func (e *scheduledCrawlEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	e.tasks = append(e.tasks, task)
	return &asynq.TaskInfo{}, e.err
}

func TestSchedulerWebCrawlDispatchAndDeduplication(t *testing.T) {
	repo := newFakeDataSourceRepo()
	ds := &types.DataSource{ID: "website", TenantID: 1, Type: types.ConnectorTypeWebCrawler,
		Status: types.DataSourceStatusActive, SyncSchedule: "0 2 * * *"}
	require.NoError(t, repo.Create(context.Background(), ds))
	web := &scheduledCrawlRepo{}
	queue := &scheduledCrawlEnqueuer{}
	s := NewSchedulerWithWebCrawler(repo, newFakeSyncLogRepo(), queue, web)
	s.triggerSync(ds.ID, ds.TenantID)
	s.triggerSync(ds.ID, ds.TenantID)
	require.Len(t, queue.tasks, 1)
	require.Equal(t, types.TypeWebCrawlScan, queue.tasks[0].Type())
	var payload types.WebCrawlScanPayload
	require.NoError(t, json.Unmarshal(queue.tasks[0].Payload(), &payload))
	require.True(t, payload.AutoApply)
	require.Equal(t, web.scan.ID, payload.ScanID)
	require.Equal(t, ds.TenantID, payload.TenantID)
}

func TestSchedulerWebCrawlSkipsOverlapAndReadFailures(t *testing.T) {
	for _, web := range []*scheduledCrawlRepo{{running: true}, {readErr: errors.New("database unavailable")}} {
		queue := &scheduledCrawlEnqueuer{}
		s := NewSchedulerWithWebCrawler(newFakeDataSourceRepo(), newFakeSyncLogRepo(), queue, web)
		s.triggerWebCrawl(context.Background(), &types.DataSource{ID: "website"})
		require.Empty(t, queue.tasks)
		require.Nil(t, web.scan)
	}
}

func TestSchedulerWebCrawlEnqueueFailureClosesScan(t *testing.T) {
	web := &scheduledCrawlRepo{}
	queue := &scheduledCrawlEnqueuer{err: errors.New("queue unavailable")}
	s := NewSchedulerWithWebCrawler(newFakeDataSourceRepo(), newFakeSyncLogRepo(), queue, web)
	s.triggerWebCrawl(context.Background(), &types.DataSource{ID: "website"})
	require.Equal(t, types.WebCrawlScanStatusCanceled, web.scan.Status)
	require.NotNil(t, web.scan.FinishedAt)
}
