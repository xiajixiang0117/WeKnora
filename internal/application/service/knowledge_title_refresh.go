package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/Tencent/WeKnora/internal/webtitle"
	"github.com/hibiken/asynq"
)

const titleRefreshInterval = 250 * time.Millisecond

// ProcessKnowledgeListTitleRefresh updates only generated URL titles. The
// worker never reparses, rechunks, or re-embeds the selected knowledge.
func (s *knowledgeService) ProcessKnowledgeListTitleRefresh(ctx context.Context, t *asynq.Task) error {
	var payload types.KnowledgeListTitleRefreshPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal knowledge title refresh payload: %w", err)
	}

	ctx = payload.Initiator.Apply(ctx)
	taskID, _ := asynq.GetTaskID(ctx)
	ctx = withKBActivityTask(ctx, taskID, kbActivityTrigger(ctx))

	ctx, ids, err := s.reparseTaskScope(ctx, types.KnowledgeListReparsePayload{
		KnowledgeBaseID: payload.KnowledgeBaseID,
		TenantID:        payload.TenantID,
		KnowledgeIDs:    payload.KnowledgeIDs,
		Initiator:       payload.Initiator,
	})
	if err != nil || len(ids) == 0 {
		return err
	}

	tenant, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		return fmt.Errorf("get title refresh tenant %d: %w", payload.TenantID, err)
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)

	var failures []error
	updated := 0
	for index, id := range ids {
		if index > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(titleRefreshInterval):
			}
		}
		changed, refreshErr := s.refreshURLKnowledgeTitle(ctx, id)
		if refreshErr != nil {
			failures = append(failures, fmt.Errorf("knowledge %s: %w", secutils.SanitizeForLog(id), refreshErr))
			continue
		}
		if changed {
			updated++
		}
	}

	logger.Infof(ctx, "Knowledge URL title refresh finished: %d selected, %d updated, %d failed", len(ids), updated, len(failures))
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("%w: title refresh updated %d item(s) and failed %d: %w",
		asynq.SkipRetry, updated, len(failures), errors.Join(failures...))
}

func (s *knowledgeService) refreshURLKnowledgeTitle(ctx context.Context, knowledgeID string) (bool, error) {
	knowledge, _, err := loadKnowledgeWrite(ctx, s.repo, s.kbService, knowledgeID)
	if err != nil {
		return false, err
	}
	if knowledge.Type != "url" || knowledge.Source == "" || !shouldRefreshURLKnowledgeTitle(knowledge.Title, knowledge.Source) {
		return false, nil
	}

	title, err := webtitle.Fetch(ctx, knowledge.Source)
	if err != nil {
		return false, err
	}
	if title == knowledge.Title {
		return false, nil
	}
	knowledge.Title = title
	knowledge.UpdatedAt = time.Now()
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		return false, err
	}
	return true, nil
}
