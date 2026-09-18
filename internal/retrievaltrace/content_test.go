package retrievaltrace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestContentSnapshotsSeparateRecallContextAndCitations(t *testing.T) {
	ctx := WithRecorder(context.Background(), NewRecorder("question"))
	raw := &types.SearchResult{ID: "one", KnowledgeID: "doc", Content: "original recall", Score: 0.7}
	ignored := &types.SearchResult{ID: "two", Content: "not cited"}
	RecordRetrieval(ctx, "rag", "query", "kb", []*types.SearchResult{raw, ignored}, nil)
	raw.Content = "merged generation content"
	RecordGenerationContext(ctx, "rag", []*types.SearchResult{raw, ignored})
	answer := `Answer <kb chunk_id="one"/> <kb chunk_id="one"/>`
	RecordAnswer(ctx, answer, types.References{raw, ignored, raw, nil}, true, false)
	raw.Content = "later edit"
	snapshot := FromContext(ctx).Snapshot()
	require.Equal(t, "original recall", snapshot.Steps[0].Retrieval.Candidates[0].Content)
	require.Equal(t, "merged generation content", snapshot.Steps[1].Context.Candidates[0].Content)
	require.Equal(t, answer, snapshot.Answer.Content)
	require.True(t, snapshot.Answer.IsCompleted)
	require.Equal(t, 1, snapshot.Answer.CitedCount)
	require.Equal(t, "one", snapshot.Answer.CitedCandidates[0].ChunkID)
	require.Equal(t, "merged generation content", snapshot.Answer.CitedCandidates[0].Content)
	snapshot.Steps[0].Retrieval.Candidates[0].Content = "external mutation"
	snapshot.Answer.CitedCandidates[0].Content = "external mutation"
	require.Equal(t, "original recall", FromContext(ctx).Snapshot().Steps[0].Retrieval.Candidates[0].Content)
	require.Equal(t, "merged generation content", FromContext(ctx).Snapshot().Answer.CitedCandidates[0].Content)
}

func TestAnswerWithoutCitationsDoesNotTreatContextAsAdopted(t *testing.T) {
	ctx := WithRecorder(context.Background(), NewRecorder("question"))
	RecordAnswer(ctx, "partial answer", types.References{{ID: "candidate", Content: "evidence"}}, false, true)
	answer := FromContext(ctx).Snapshot().Answer
	require.Empty(t, answer.CitedCandidates)
	require.False(t, answer.IsCompleted)
	require.True(t, answer.IsFallback)
	data, err := json.Marshal(answer)
	require.NoError(t, err)
	require.Contains(t, string(data), `"cited_candidates":[]`)
}

func TestAnswerCitationsSupportStoredWebKnowledgeAndBounds(t *testing.T) {
	ctx := WithRecorder(context.Background(), NewRecorder("question"))
	refs := types.References{{ID: "url-chunk", KnowledgeType: "url", KnowledgeSource: "https://example.com/page#section", Content: "page content"}}
	RecordAnswer(ctx, `<web url="https://example.com/page"/>`, refs, true, false)
	require.Equal(t, "url-chunk", FromContext(ctx).Snapshot().Answer.CitedCandidates[0].ChunkID)
	var answer strings.Builder
	refs = nil
	for i := 0; i <= MaxCandidates; i++ {
		id := fmt.Sprintf("chunk-%d", i)
		refs = append(refs, &types.SearchResult{ID: id, Content: "完整内容"})
		fmt.Fprintf(&answer, `<kb chunk_id="%s"/>`, id)
	}
	RecordGenerationContext(ctx, "rag", refs)
	RecordAnswer(ctx, answer.String(), refs, true, false)
	snapshot := FromContext(ctx).Snapshot()
	require.True(t, snapshot.Steps[0].Context.Truncated)
	require.True(t, snapshot.Answer.Truncated)
	require.Equal(t, MaxCandidates+1, snapshot.Answer.CitedCount)
	require.Len(t, snapshot.Answer.CitedCandidates, MaxCandidates)
}

func TestModelChunkCitationsDoNotAdoptEveryChunkOfTheSameURL(t *testing.T) {
	ctx := WithRecorder(context.Background(), NewRecorder("question"))
	registry := modelcontext.NewRegistry(true)
	refs := types.References{
		{ID: "one", Content: "first passage", KnowledgeType: "url", KnowledgeSource: "https://example.com/doc"},
		{ID: "two", Content: "second passage", KnowledgeType: "url", KnowledgeSource: "https://example.com/doc"},
	}
	registry.RegisterSearchResults(refs)
	// The public answer identifies only the URL, but the private protocol still
	// tells us exactly which of the two chunks the LLM cited.
	raw := `answer <ref id="c2"/>`
	RecordModelCitations(ctx, registry, raw)
	RecordAnswer(ctx, registry.DecodeOutputText(raw), refs, true, false)
	answer := FromContext(ctx).Snapshot().Answer
	require.Equal(t, 1, answer.CitedCount)
	require.Equal(t, "two", answer.CitedCandidates[0].ChunkID)
	require.Equal(t, "chunk", answer.CitedCandidates[0].CitationMatch)
	// The next model round replaces citations from an intermediate preamble.
	RecordModelCitations(ctx, registry, "answer without citations")
	RecordAnswer(ctx, "answer without citations", refs, true, false)
	require.Empty(t, FromContext(ctx).Snapshot().Answer.CitedCandidates)
}
