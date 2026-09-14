package retrievaltrace

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Trace is the durable, tenant-scoped audit record for one chat request.
type Trace struct {
	ID                 string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	TenantID           uint64     `gorm:"not null;index;uniqueIndex:idx_retrieval_execution_traces_tenant_request,priority:1" json:"tenant_id"`
	SessionID          string     `gorm:"type:varchar(36);not null;index" json:"session_id"`
	RequestID          string     `gorm:"type:varchar(128);not null;uniqueIndex:idx_retrieval_execution_traces_tenant_request,priority:2" json:"request_id"`
	UserMessageID      string     `gorm:"type:varchar(36);default:''" json:"user_message_id,omitempty"`
	AssistantMessageID string     `gorm:"type:varchar(36);default:''" json:"assistant_message_id,omitempty"`
	Status             string     `gorm:"type:varchar(16);not null" json:"status"`
	Snapshot           types.JSON `gorm:"column:trace;type:jsonb;not null" json:"trace"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (Trace) TableName() string { return "retrieval_execution_traces" }

type Summary struct {
	RequestID          string    `json:"request_id"`
	UserMessageID      string    `json:"user_message_id,omitempty"`
	AssistantMessageID string    `json:"assistant_message_id,omitempty"`
	Status             string    `json:"status"`
	OriginalQuery      string    `json:"original_query"`
	StepCount          int       `json:"step_count"`
	CreatedAt          time.Time `json:"created_at"`
}

type Store struct {
	db                   *gorm.DB
	knowledgeBaseService interfaces.KnowledgeBaseService
}

func NewStore(db *gorm.DB, knowledgeBaseService interfaces.KnowledgeBaseService) *Store {
	return &Store{db: db, knowledgeBaseService: knowledgeBaseService}
}

func (s *Store) Save(ctx context.Context, tenantID uint64, sessionID, requestID, userMessageID, assistantMessageID, status string, recorder *Recorder) error {
	snapshot := recorder.Snapshot()
	s.enrichKnowledgeBaseNames(ctx, &snapshot)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	row := Trace{ID: uuid.NewString(), TenantID: tenantID, SessionID: sessionID, RequestID: requestID, UserMessageID: userMessageID, AssistantMessageID: assistantMessageID, Status: status, Snapshot: types.JSON(payload)}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "request_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"session_id": sessionID, "user_message_id": userMessageID, "assistant_message_id": assistantMessageID,
			"status": status, "trace": types.JSON(payload), "updated_at": time.Now().UTC(),
		}),
	}).Create(&row).Error
}

func (s *Store) List(ctx context.Context, tenantID uint64, sessionID string) ([]Summary, error) {
	var rows []Trace
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]Summary, 0, len(rows))
	for _, row := range rows {
		var snapshot Snapshot
		_ = json.Unmarshal(row.Snapshot, &snapshot)
		result = append(result, Summary{RequestID: row.RequestID, UserMessageID: row.UserMessageID, AssistantMessageID: row.AssistantMessageID, Status: row.Status, OriginalQuery: snapshot.OriginalQuery, StepCount: len(snapshot.Steps), CreatedAt: row.CreatedAt})
	}
	return result, nil
}

func (s *Store) Get(ctx context.Context, tenantID uint64, sessionID, requestID string) (*Trace, error) {
	var row Trace
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND session_id = ? AND request_id = ?", tenantID, sessionID, requestID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteSession removes the durable audit data when its session transcript is
// explicitly removed. It is scoped by tenant even though session IDs are UUIDs.
func (s *Store) DeleteSession(ctx context.Context, tenantID uint64, sessionID string) error {
	return s.db.WithContext(ctx).Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).Delete(&Trace{}).Error
}

func (s *Store) enrichKnowledgeBaseNames(ctx context.Context, snapshot *Snapshot) {
	if s.knowledgeBaseService == nil || snapshot == nil {
		return
	}
	ids := make([]string, 0)
	seen := map[string]bool{}
	for _, step := range snapshot.Steps {
		if step.Retrieval != nil && step.Retrieval.KnowledgeBaseID != "" && !seen[step.Retrieval.KnowledgeBaseID] {
			ids, seen[step.Retrieval.KnowledgeBaseID] = append(ids, step.Retrieval.KnowledgeBaseID), true
		}
		if step.Rerank != nil {
			for _, candidate := range step.Rerank.Candidates {
				if candidate.KnowledgeBaseID != "" && !seen[candidate.KnowledgeBaseID] {
					ids, seen[candidate.KnowledgeBaseID] = append(ids, candidate.KnowledgeBaseID), true
				}
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	kbs, err := s.knowledgeBaseService.GetKnowledgeBasesByIDsOnly(ctx, ids)
	if err != nil {
		return
	}
	names := map[string]string{}
	for _, kb := range kbs {
		if kb != nil {
			names[kb.ID] = kb.Name
		}
	}
	for i := range snapshot.Steps {
		if retrieval := snapshot.Steps[i].Retrieval; retrieval != nil {
			retrieval.KnowledgeBaseName = names[retrieval.KnowledgeBaseID]
			for j := range retrieval.Candidates {
				retrieval.Candidates[j].KnowledgeBaseName = names[retrieval.Candidates[j].KnowledgeBaseID]
			}
		}
		if rerank := snapshot.Steps[i].Rerank; rerank != nil {
			for j := range rerank.Candidates {
				rerank.Candidates[j].KnowledgeBaseName = names[rerank.Candidates[j].KnowledgeBaseID]
			}
		}
	}
}
