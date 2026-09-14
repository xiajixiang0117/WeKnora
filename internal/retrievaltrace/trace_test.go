package retrievaltrace

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecorderCapturesAndBoundsCandidates(t *testing.T) {
	ctx := WithRecorder(context.Background(), NewRecorder("original question"))
	results := make([]*types.SearchResult, MaxCandidates+1)
	for i := range results {
		results[i] = &types.SearchResult{ID: fmt.Sprintf("chunk-%d", i), KnowledgeID: "doc", KnowledgeBaseID: "kb", KnowledgeTitle: "document", Score: float64(i) / 100}
	}
	RecordRewrite(ctx, "original question", "rewritten question")
	RecordRetrieval(ctx, "rag", "rewritten question", "kb", results, nil)

	snapshot := FromContext(ctx).Snapshot()
	require.Equal(t, SnapshotVersion, snapshot.TraceVersion)
	require.Len(t, snapshot.Steps, 2)
	require.Equal(t, "rewritten question", snapshot.Steps[0].RewrittenQuery)
	require.True(t, snapshot.Steps[1].Retrieval.Truncated)
	require.Equal(t, MaxCandidates+1, snapshot.Steps[1].Retrieval.ReturnedCount)
	require.Len(t, snapshot.Steps[1].Retrieval.Candidates, MaxCandidates)

	selected := []*types.SearchResult{results[0]}
	RecordRerank(ctx, "rag", "rewritten question", "rerank", "model", 0.3, results[:2], map[string]float64{"chunk-0": 0.99}, selected, "completed", nil)
	snapshot = FromContext(ctx).Snapshot()
	require.True(t, snapshot.Steps[2].Rerank.Candidates[0].Selected)
	require.Equal(t, 0.99, *snapshot.Steps[2].Rerank.Candidates[0].ModelScore)
}

func TestRecorderAssignsUniqueSequenceForConcurrentWorkers(t *testing.T) {
	ctx := WithRecorder(context.Background(), NewRecorder("question"))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			RecordRetrieval(ctx, "rag", fmt.Sprintf("q-%d", i), "kb", nil, nil)
		}(i)
	}
	wg.Wait()

	snapshot := FromContext(ctx).Snapshot()
	require.Len(t, snapshot.Steps, 20)
	for i, step := range snapshot.Steps {
		require.Equal(t, i+1, step.Sequence)
	}
}

func TestStoreScopesTraceByTenantAndSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Trace{}))
	store := NewStore(db, nil)
	recorder := NewRecorder("question")
	recorder.append(TraceStep{Kind: "retrieval", Status: "empty"})

	require.NoError(t, store.Save(context.Background(), 7, "session-a", "request-a", "user-a", "assistant-a", "empty", recorder))
	summaries, err := store.List(context.Background(), 7, "session-a")
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "question", summaries[0].OriginalQuery)

	trace, err := store.Get(context.Background(), 7, "session-a", "request-a")
	require.NoError(t, err)
	require.Equal(t, "empty", trace.Status)
	_, err = store.Get(context.Background(), 8, "session-a", "request-a")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, store.DeleteSession(context.Background(), 7, "session-a"))
	summaries, err = store.List(context.Background(), 7, "session-a")
	require.NoError(t, err)
	require.Empty(t, summaries)
}
