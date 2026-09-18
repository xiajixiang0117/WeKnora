package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/retrievaltrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLegacyTraceRestoresOnlyItsOwnSavedAnswer(t *testing.T) {
	trace := &retrievaltrace.Trace{SessionID: "session", RequestID: "request", AssistantMessageID: "assistant", Status: "completed", Snapshot: types.JSON(`{"trace_version":1,"original_query":"question","steps":[{"retrieval":{"candidates":[{"chunk_id":"one","knowledge_id":"doc"},{"chunk_id":"missing"},{"chunk_id":"mismatch","knowledge_id":"original"}]}}]}`)}
	message := &types.Message{SessionID: "session", RequestID: "request", Role: "assistant", IsCompleted: true, Content: `answer <kb chunk_id="one"/>`, KnowledgeReferences: types.References{{ID: "one", KnowledgeID: "doc", Content: "saved evidence"}, {ID: "mismatch", KnowledgeID: "different", Content: "wrong document"}}}
	h := &Handler{messageService: &stubMessageServiceForArtifacts{getMessage: func(ctx context.Context, sessionID, id string) (*types.Message, error) {
		require.True(t, types.IsSessionManagementRead(ctx))
		require.Equal(t, "session", sessionID)
		require.Equal(t, "assistant", id)
		return message, nil
	}}}
	h.enrichTraceAnswer(types.WithSessionManagementRead(context.Background()), trace)
	var snapshot retrievaltrace.Snapshot
	require.NoError(t, json.Unmarshal(trace.Snapshot, &snapshot))
	require.Equal(t, message.Content, snapshot.Answer.Content)
	require.Len(t, snapshot.Answer.CitedCandidates, 1)
	require.Equal(t, "answer_reference", snapshot.Answer.CitedCandidates[0].ContentSource)
	require.Equal(t, "saved evidence", snapshot.Steps[0].Retrieval.Candidates[0].Content)
	require.Empty(t, snapshot.Steps[0].Retrieval.Candidates[1].Content)
	require.Empty(t, snapshot.Steps[0].Retrieval.Candidates[2].Content)
	require.Nil(t, snapshot.Steps[0].Context)
}

func TestLegacyTraceDoesNotUseUnrelatedOrUnavailableMessage(t *testing.T) {
	for _, test := range []struct {
		name    string
		message *types.Message
		err     error
	}{
		{name: "missing", err: errors.New("deleted")},
		{name: "different session", message: &types.Message{SessionID: "other", RequestID: "request", Role: "assistant", Content: "private"}},
		{name: "different request", message: &types.Message{SessionID: "session", RequestID: "other", Role: "assistant", Content: "private"}},
		{name: "user message", message: &types.Message{SessionID: "session", RequestID: "request", Role: "user", Content: "input"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := types.JSON(`{"trace_version":1,"steps":[]}`)
			trace := &retrievaltrace.Trace{SessionID: "session", RequestID: "request", AssistantMessageID: "assistant", Snapshot: original}
			h := &Handler{messageService: &stubMessageServiceForArtifacts{getMessage: func(context.Context, string, string) (*types.Message, error) { return test.message, test.err }}}
			h.enrichTraceAnswer(context.Background(), trace)
			require.Equal(t, original, trace.Snapshot)
		})
	}
}

func TestTraceAnswerSnapshotIsNotReplacedByCurrentMessage(t *testing.T) {
	original := types.JSON(`{"trace_version":2,"steps":[],"answer":{"content":"original","cited_candidates":[]}}`)
	trace := &retrievaltrace.Trace{AssistantMessageID: "assistant", Snapshot: original}
	h := &Handler{messageService: &stubMessageServiceForArtifacts{getMessage: func(context.Context, string, string) (*types.Message, error) {
		t.Fatal("must not fetch newer text")
		return nil, nil
	}}}
	h.enrichTraceAnswer(context.Background(), trace)
	require.Equal(t, original, trace.Snapshot)
}
