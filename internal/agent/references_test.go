package agent

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/retrievaltrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCollectKnowledgeReferencesKeepsOnlyFinalAnswerCitations(t *testing.T) {
	state := &types.AgentState{
		FinalAnswer: `
The cited document.<kb doc="Cited PDF" chunk_id="chunk-cited" kb_id="kb-1" />
The cited web page.<web url="https://example.com/cited#claim" title="Cited page" />`,
		KnowledgeRefs: []*types.SearchResult{
			{ID: "chunk-not-cited", KnowledgeID: "doc-not-cited"},
			{ID: "chunk-cited", KnowledgeID: "doc-cited"},
			{ID: "web-chunk-1", KnowledgeID: "web-cited", KnowledgeType: "url", KnowledgeSource: "https://example.com/cited"},
			{ID: "web-chunk-2", KnowledgeID: "web-cited", KnowledgeType: "url", KnowledgeSource: "https://example.com/cited#other"},
			{ID: "web-not-cited", KnowledgeID: "web-not-cited", KnowledgeType: "url", KnowledgeSource: "https://example.com/not-cited"},
		},
	}

	references := collectKnowledgeReferences(state)
	require.Equal(t, []string{"chunk-cited", "web-chunk-1", "web-chunk-2"}, referenceIDs(references))
}

func TestCollectKnowledgeReferencesReturnsNoSourcesWithoutFinalCitations(t *testing.T) {
	state := &types.AgentState{
		FinalAnswer:   "A direct answer without citations.",
		KnowledgeRefs: []*types.SearchResult{{ID: "candidate", KnowledgeID: "candidate-doc"}},
	}

	require.Empty(t, collectKnowledgeReferences(state))
}

func TestCollectKnowledgeReferencesMatchesWebSearchMetadataURL(t *testing.T) {
	state := &types.AgentState{
		FinalAnswer: `<web url="https://example.com/result" title="Result" />`,
		KnowledgeRefs: []*types.SearchResult{{
			ID:       "web-result",
			Metadata: map[string]string{"url": "https://example.com/result"},
		}},
	}

	require.Equal(t, []string{"web-result"}, referenceIDs(collectKnowledgeReferences(state)))
}

func referenceIDs(references []*types.SearchResult) []string {
	ids := make([]string, 0, len(references))
	for _, reference := range references {
		ids = append(ids, reference.ID)
	}
	return ids
}

func TestToolContextRecordsRoundsButOnlyCitedChunksBecomeReferences(t *testing.T) {
	ctx := retrievaltrace.WithRecorder(context.Background(), retrievaltrace.NewRecorder("question"))
	search := types.AgentStep{ToolCalls: []types.ToolCall{{Name: "knowledge_search", Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
		"display_type": "search_results", "results": []map[string]interface{}{{"chunk_id": "found", "content": "search text"}},
	}}}}}
	read := types.AgentStep{ToolCalls: []types.ToolCall{{Name: "list_knowledge_chunks", Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
		"display_type": "knowledge_chunks_list", "knowledge_title": "Document", "knowledge_type": "file", "chunks": []map[string]interface{}{{"chunk_id": "read", "content": "deep read text", "knowledge_id": "doc"}},
	}}}}}
	recordToolGenerationContext(ctx, search)
	recordToolGenerationContext(ctx, read)
	snapshot := retrievaltrace.FromContext(ctx).Snapshot()
	require.Len(t, snapshot.Steps, 2)
	require.Equal(t, "search text", snapshot.Steps[0].Context.Candidates[0].Content)
	require.Equal(t, "deep read text", snapshot.Steps[1].Context.Candidates[0].Content)
	references := collectKnowledgeReferences(&types.AgentState{FinalAnswer: `answer <kb chunk_id="read"/>`, RoundSteps: []types.AgentStep{search, read}})
	require.Len(t, references, 1)
	require.Equal(t, "read", references[0].ID)
	require.Equal(t, "Document", references[0].KnowledgeTitle)
	// Untrusted tools using the same data shape must not produce provenance.
	search.ToolCalls[0].Name = "external_search"
	recordToolGenerationContext(ctx, search)
	require.Len(t, retrievaltrace.FromContext(ctx).Snapshot().Steps, 2)
}
