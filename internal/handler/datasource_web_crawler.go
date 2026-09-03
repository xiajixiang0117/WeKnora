package handler

import (
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type webCrawlApplyRequest struct {
	ChangeIDs      []string          `json:"change_ids" binding:"required"`
	MissingActions map[string]string `json:"missing_actions,omitempty"`
}

type webCrawlRetryRequest struct {
	ChangeIDs []string `json:"change_ids" binding:"required"`
}

type webCrawlIgnoreRequest struct {
	ChangeIDs []string `json:"change_ids" binding:"required"`
}

func (h *DataSourceHandler) CreateWebCrawlScan(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	dsID := c.Param("id")
	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, dsID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	scan, err := h.service.CreateWebCrawlScan(ctx, dsID, c.GetString(types.UserIDContextKey.String()))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, scan)
}

func (h *DataSourceHandler) ListWebCrawlScans(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	dsID := c.Param("id")
	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, dsID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	scans, err := h.service.ListWebCrawlScans(ctx, dsID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, scans)
}

func (h *DataSourceHandler) ListWebCrawlChanges(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	scanID := c.Param("scan_id")
	scan, err := h.service.GetWebCrawlScan(ctx, scanID)
	if err != nil || scan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scan not found"})
		return
	}
	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, scan.DataSourceID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	changes, err := h.service.ListWebCrawlChanges(ctx, scanID, c.Query("change_type"), c.Query("decision"), c.Query("apply_status"), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, change := range changes {
		if len(change.PreviousContent) > 20000 {
			change.PreviousContent = change.PreviousContent[:20000] + "\n…"
		}
		if len(change.NewContent) > 20000 {
			change.NewContent = change.NewContent[:20000] + "\n…"
		}
	}
	c.JSON(http.StatusOK, changes)
}

func (h *DataSourceHandler) ApplyWebCrawlChanges(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req webCrawlApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	scanID := c.Param("scan_id")
	scan, err := h.service.GetWebCrawlScan(ctx, scanID)
	if err != nil || scan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scan not found"})
		return
	}
	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, scan.DataSourceID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	if err := h.service.ApplyWebCrawlChanges(ctx, scanID, req.ChangeIDs, req.MissingActions); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": types.WebCrawlScanStatusApplying})
}

func (h *DataSourceHandler) RetryWebCrawlChanges(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req webCrawlRetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	scanID := c.Param("scan_id")
	scan, err := h.service.GetWebCrawlScan(ctx, scanID)
	if err != nil || scan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scan not found"})
		return
	}
	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, scan.DataSourceID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	if err := h.service.RetryWebCrawlChanges(ctx, scanID, req.ChangeIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": types.WebCrawlScanStatusApplying})
}

func (h *DataSourceHandler) IgnoreWebCrawlChanges(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req webCrawlIgnoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	scanID := c.Param("scan_id")
	scan, err := h.service.GetWebCrawlScan(ctx, scanID)
	if err != nil || scan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scan not found"})
		return
	}
	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, scan.DataSourceID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	if err := h.service.IgnoreWebCrawlChanges(ctx, scanID, req.ChangeIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": types.WebCrawlDecisionIgnore})
}
