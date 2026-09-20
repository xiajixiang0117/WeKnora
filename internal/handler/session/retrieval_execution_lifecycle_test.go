package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/retrievaltrace"
	"github.com/Tencent/WeKnora/internal/stream"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type traceSessionService struct {
	interfaces.SessionService
	run func(context.Context, *event.EventBus) error
}

func (s *traceSessionService) KnowledgeQA(ctx context.Context, _ *types.QARequest, bus *event.EventBus) error {
	return s.run(ctx, bus)
}
func (*traceSessionService) UpdateSessionLastRequestState(context.Context, string, *types.SessionLastRequestState) error {
	return nil
}

type traceMessageService struct{ interfaces.MessageService }

func (*traceMessageService) UpdateMessage(context.Context, *types.Message) error              { return nil }
func (*traceMessageService) IndexMessageToKB(context.Context, string, string, string, string) {}
func (*traceMessageService) ClearSessionMessages(context.Context, string) error               { return nil }

func TestExecuteQAPersistsTraceAtTerminalEvent(t *testing.T) {
	for _, outcome := range []string{"completed", "failed", "cancelled", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "trace.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&retrievaltrace.Trace{}))
			store := retrievaltrace.NewStore(db, nil)
			var bus *event.EventBus
			var runCtx context.Context
			svc := &traceSessionService{run: func(ctx context.Context, events *event.EventBus) error {
				runCtx, bus = ctx, events
				if outcome == "failed" {
					return errors.New("model unavailable")
				}
				if outcome == "panic" {
					panic("model panic")
				}
				return nil
			}}
			h := &Handler{sessionService: svc, messageService: &traceMessageService{}, streamManager: stream.NewMemoryStreamManager(), traceStore: store}
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(42))
			req := &qaRequestContext{ctx: ctx, skipSSE: true, sessionID: "session", requestID: "request", query: "question", userMessageID: "user", session: &types.Session{ID: "session", TenantID: 42}, assistantMessage: &types.Message{ID: "answer", SessionID: "session", Role: "assistant"}}
			h.executeQA(req, qaModeNormal, false)
			require.NotNil(t, retrievaltrace.FromContext(runCtx))
			if outcome == "completed" || outcome == "cancelled" {
				rows, err := store.List(ctx, 42, "session")
				require.NoError(t, err)
				require.Empty(t, rows, "service return must not prematurely save background streaming")
				require.NoError(t, bus.Emit(runCtx, event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "partial answer"}}))
				if outcome == "completed" {
					require.NoError(t, bus.Emit(runCtx, event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Done: true}}))
				} else {
					require.NoError(t, bus.Emit(runCtx, event.Event{Type: event.EventStop}))
				}
			}
			row, err := store.Get(ctx, 42, "session", "request")
			require.NoError(t, err)
			expected := outcome
			if outcome == "panic" {
				expected = "failed"
			}
			require.Equal(t, expected, row.Status)
			require.Equal(t, "answer", row.AssistantMessageID)
			if outcome == "completed" {
				require.Contains(t, string(row.Snapshot), "partial answer")
			}
			require.NoError(t, store.Save(ctx, 99, "session", "other", "", "", "completed", retrievaltrace.NewRecorder("other tenant")))
			c := newSteerWiringGinContext()
			c.Request = c.Request.WithContext(ctx)
			c.AddParam("id", "session")
			h.ClearSessionMessages(c)
			rows, err := store.List(ctx, 42, "session")
			require.NoError(t, err)
			require.Empty(t, rows)
			rows, err = store.List(ctx, 99, "session")
			require.NoError(t, err)
			require.Len(t, rows, 1)
		})
	}
}
