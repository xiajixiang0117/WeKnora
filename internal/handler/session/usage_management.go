package session

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

const sessionUsageMessagePageSize = 200

// SessionUsageSummary is the token-usage projection shown in the tenant's
// session-management list. Usage contains only the persisted main-chat LLM
// usage on assistant messages; retrieval, embedding, rerank, and title calls
// are intentionally outside this product-level accounting scope.
type SessionUsageSummary struct {
	SessionID          string              `json:"session_id"`
	Title              string              `json:"title"`
	Channel            string              `json:"channel"`
	ChannelID          string              `json:"channel_id,omitempty"`
	UpdatedAt          time.Time           `json:"updated_at"`
	AnswerCount        int                 `json:"answer_count"`
	TrackedAnswerCount int                 `json:"tracked_answer_count"`
	Agents             []SessionUsageAgent `json:"agents"`
	Usage              types.TokenUsage    `json:"usage"`
}

// SessionUsageAgent identifies an agent that actually produced one or more
// assistant messages in the session. Name stays empty for deleted/legacy
// agents, while ID remains available for auditability.
type SessionUsageAgent struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// SessionUsageDetail is one assistant response and its persisted aggregate
// across the agent's internal rounds for that response.
type SessionUsageDetail struct {
	MessageID string            `json:"message_id"`
	RequestID string            `json:"request_id"`
	CreatedAt time.Time         `json:"created_at"`
	AgentID   string            `json:"agent_id,omitempty"`
	AgentName string            `json:"agent_name,omitempty"`
	ModelID   string            `json:"model_id,omitempty"`
	HasUsage  bool              `json:"has_usage"`
	Usage     *types.TokenUsage `json:"usage,omitempty"`
}

// ListSessionUsage returns tenant-wide, paginated chat-model usage grouped by
// session. The route is Admin+ guarded; the context marker gives message reads
// a deliberately narrow, read-only tenant-wide scope.
func (h *Handler) ListSessionUsage(c *gin.Context) {
	ctx := types.WithSessionManagementRead(c.Request.Context())
	var pagination types.Pagination
	if err := c.ShouldBindQuery(&pagination); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	result, err := h.sessionService.ListSessions(ctx, &types.SessionListQuery{
		TenantWide: true,
		Keyword:    c.Query("keyword"),
		Source:     c.Query("source"),
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
	})
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	items, ok := result.Data.([]*types.SessionListItem)
	if !ok {
		logger.Errorf(ctx, "Unexpected session usage list result type: %T", result.Data)
		c.Error(errors.NewInternalServerError("unexpected session list result"))
		return
	}
	agentNames, err := h.sessionUsageAgentNames(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	summaries := make([]SessionUsageSummary, 0, len(items))
	for _, item := range items {
		summary, _, err := h.buildSessionUsage(ctx, item, agentNames, false)
		if err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{"session_id": item.ID})
			c.Error(errors.NewInternalServerError(err.Error()))
			return
		}
		summaries = append(summaries, summary)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      summaries,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}

// GetSessionUsage returns the per-answer usage details for one tenant session.
func (h *Handler) GetSessionUsage(c *gin.Context) {
	ctx := types.WithSessionManagementRead(c.Request.Context())
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		c.Error(errors.NewBadRequestError(errors.ErrInvalidSessionID.Error()))
		return
	}

	row, err := h.sessionService.GetSession(ctx, sessionID)
	if err != nil {
		c.Error(errors.NewNotFoundError(errors.ErrSessionNotFound.Error()))
		return
	}
	agentNames, err := h.sessionUsageAgentNames(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	summary, details, err := h.buildSessionUsage(ctx, &types.SessionListItem{
		Session:    *row,
		IMPlatform: row.IMPlatform,
	}, agentNames, true)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"session_id": sessionID})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"summary": summary,
			"details": details,
		},
	})
}

func (h *Handler) sessionUsageAgentNames(ctx context.Context) (map[string]string, error) {
	agents, err := h.customAgentService.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(agents))
	for _, agent := range agents {
		if agent != nil && agent.ID != "" {
			names[agent.ID] = agent.Name
		}
	}
	return names, nil
}

func (h *Handler) buildSessionUsage(
	ctx context.Context,
	session *types.SessionListItem,
	agentNames map[string]string,
	includeDetails bool,
) (SessionUsageSummary, []SessionUsageDetail, error) {
	summary := SessionUsageSummary{
		SessionID: session.ID,
		Title:     session.Title,
		Channel:   sessionUsageChannel(session),
		ChannelID: sessionUsageChannelID(session),
		UpdatedAt: session.UpdatedAt,
		Agents:    make([]SessionUsageAgent, 0),
	}
	agents := make(map[string]SessionUsageAgent)
	details := make([]SessionUsageDetail, 0)

	for page := 1; ; page++ {
		messages, err := h.messageService.GetMessagesBySession(
			ctx, session.ID, page, sessionUsageMessagePageSize,
		)
		if err != nil {
			return SessionUsageSummary{}, nil, err
		}
		for _, message := range messages {
			if message == nil || message.Role != "assistant" {
				continue
			}
			summary.AnswerCount++
			if message.AgentID != "" {
				agents[message.AgentID] = SessionUsageAgent{
					ID:   message.AgentID,
					Name: agentNames[message.AgentID],
				}
			}

			detail := SessionUsageDetail{
				MessageID: message.ID,
				RequestID: message.RequestID,
				CreatedAt: message.CreatedAt,
				AgentID:   message.AgentID,
				AgentName: agentNames[message.AgentID],
				ModelID:   message.ModelID,
			}
			if message.Usage != nil {
				usage := normalizedUsage(*message.Usage)
				detail.HasUsage = true
				detail.Usage = &usage
				summary.TrackedAnswerCount++
				summary.Usage.Accumulate(usage)
			}
			if includeDetails {
				details = append(details, detail)
			}
		}
		if len(messages) < sessionUsageMessagePageSize {
			break
		}
	}

	for _, agent := range agents {
		summary.Agents = append(summary.Agents, agent)
	}
	return summary, details, nil
}

func normalizedUsage(usage types.TokenUsage) types.TokenUsage {
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func sessionUsageChannel(session *types.SessionListItem) string {
	if platform := strings.TrimSpace(session.IMPlatform); platform != "" {
		return strings.ToLower(platform)
	}
	if strings.HasPrefix(session.Description, types.EmbedSessionMarkerPrefix) {
		return "embed"
	}
	if types.IsAPISessionOwnerID(session.UserID) {
		return types.SessionSourceAPI
	}
	return types.SessionSourceWeb
}

func sessionUsageChannelID(session *types.SessionListItem) string {
	if strings.HasPrefix(session.Description, types.EmbedSessionMarkerPrefix) {
		return strings.TrimPrefix(session.Description, types.EmbedSessionMarkerPrefix)
	}
	return session.IMChannelID
}
