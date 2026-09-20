package session

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/retrievaltrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func (h *Handler) deleteExecutionTraces(ctx context.Context, sessionID string) {
	if h.traceStore == nil {
		return
	}
	tenantID, ok := ctx.Value(types.TenantIDContextKey).(uint64)
	if !ok {
		return
	}
	if err := h.traceStore.DeleteSession(ctx, tenantID, sessionID); err != nil {
		logger.Warnf(ctx, "Failed to delete execution traces for session %s: %v", sessionID, err)
	}
}

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
	h.enrichTraceAnswer(ctx, trace)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": trace})
}

// Older traces have no response/content snapshot. Recover only data already
// persisted with this exact answer, never today's possibly edited chunk text.
// Missing/deleted messages must not prevent viewing the original trace.
func (h *Handler) enrichTraceAnswer(ctx context.Context, trace *retrievaltrace.Trace) {
	var snapshot retrievaltrace.Snapshot
	if json.Unmarshal(trace.Snapshot, &snapshot) != nil || snapshot.Answer != nil || h.messageService == nil || trace.AssistantMessageID == "" {
		return
	}
	message, err := h.messageService.GetMessage(ctx, trace.SessionID, trace.AssistantMessageID)
	if err != nil || message == nil || message.Role != "assistant" || message.SessionID != trace.SessionID || message.RequestID != trace.RequestID {
		return
	}
	recorder := retrievaltrace.NewRecorder(snapshot.OriginalQuery)
	retrievaltrace.RecordAnswer(retrievaltrace.WithRecorder(ctx, recorder), message.Content,
		message.KnowledgeReferences, message.IsCompleted && trace.Status == "completed", message.IsFallback)
	snapshot.Answer = recorder.Snapshot().Answer
	for i := range snapshot.Answer.CitedCandidates {
		snapshot.Answer.CitedCandidates[i].ContentSource = "answer_reference"
	}
	refs := make(map[string]*types.SearchResult)
	for _, ref := range message.KnowledgeReferences {
		if ref != nil {
			refs[ref.ID] = ref
		}
	}
	for _, group := range snapshot.CandidateGroups() {
		for i := range group {
			candidate := &group[i]
			ref := refs[candidate.ChunkID]
			if candidate.Content != "" || ref == nil || ref.Content == "" {
				continue
			}
			// Keep the original candidate identity. A merged answer reference
			// is supplementary evidence, not an original retrieval snapshot.
			if candidate.KnowledgeID != "" && candidate.KnowledgeID != ref.KnowledgeID {
				continue
			}
			candidate.Content, candidate.ContentSource = ref.Content, "answer_reference"
		}
	}
	if encoded, err := json.Marshal(snapshot); err == nil {
		trace.Snapshot = types.JSON(encoded)
	}
}
