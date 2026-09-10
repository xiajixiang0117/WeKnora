package agent

import (
	"testing"

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
