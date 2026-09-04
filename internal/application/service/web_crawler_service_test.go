package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
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
