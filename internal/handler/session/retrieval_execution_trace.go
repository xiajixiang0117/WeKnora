package session

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// ListRetrievalExecutionTraces returns the request-level RAG audit timeline
// for a session. Routes guard this Admin+, while the context marker permits
// the intentionally narrow tenant-wide session read in the service layer.
func (h *Handler) ListRetrievalExecutionTraces(c *gin.Context) {
	ctx := types.WithSessionManagementRead(c.Request.Context())
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		c.Error(errors.NewBadRequestError(errors.ErrInvalidSessionID.Error()))
		return
	}
	if h.traceStore == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []interface{}{}})
		return
	}
	row, err := h.sessionService.GetSession(ctx, sessionID)
	if err != nil {
		c.Error(errors.NewNotFoundError(errors.ErrSessionNotFound.Error()))
		return
	}
	traces, err := h.traceStore.List(ctx, row.TenantID, sessionID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"session_id": sessionID})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": traces})
}

// GetRetrievalExecutionTrace returns one full trace snapshot. The request ID
// is scoped to both the authenticated tenant and the session to prevent a
// guessed ID from crossing either boundary.
func (h *Handler) GetRetrievalExecutionTrace(c *gin.Context) {
	ctx := types.WithSessionManagementRead(c.Request.Context())
	sessionID := strings.TrimSpace(c.Param("id"))
	requestID := strings.TrimSpace(c.Param("request_id"))
	if sessionID == "" || requestID == "" {
		c.Error(errors.NewBadRequestError("session id and request id are required"))
		return
	}
	if h.traceStore == nil {
		c.Error(errors.NewNotFoundError("retrieval execution trace not found"))
		return
	}
	row, err := h.sessionService.GetSession(ctx, sessionID)
	if err != nil {
		c.Error(errors.NewNotFoundError(errors.ErrSessionNotFound.Error()))
		return
	}
	trace, err := h.traceStore.Get(ctx, row.TenantID, sessionID, requestID)
	if err != nil {
		c.Error(errors.NewNotFoundError("retrieval execution trace not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": trace})
}
