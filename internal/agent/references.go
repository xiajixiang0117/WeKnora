package agent

import (
	"encoding/json"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

// collectKnowledgeReferences rebuilds the durable reference list from the
// structured results of every completed Agent tool call. Tool result output is
// model-facing and may be compacted, but Data retains the stable chunk IDs
// needed by the message's source drawer after the stream has finished.
func collectKnowledgeReferences(state *types.AgentState) []*types.SearchResult {
	if state == nil {
		return nil
	}

	references := make([]*types.SearchResult, 0, len(state.KnowledgeRefs))
	seen := make(map[string]struct{})
	appendReference := func(ref *types.SearchResult) {
		if ref == nil || ref.ID == "" {
			return
		}
		if _, exists := seen[ref.ID]; exists {
			return
		}
		seen[ref.ID] = struct{}{}
		references = append(references, ref)
	}

	for _, ref := range state.KnowledgeRefs {
		appendReference(ref)
	}
	for _, step := range state.RoundSteps {
		for _, toolCall := range step.ToolCalls {
			for _, ref := range referencesFromToolResult(toolCall.Name, toolCall.Result) {
				appendReference(ref)
			}
		}
	}
	return references
}

// referencesFromToolResult restores references only from built-in retrieval
// tools whose structured payload is part of the source protocol. Dynamic MCP
// results are deliberately excluded: matching field names alone must not make
// an external tool's arbitrary data look like trusted knowledge provenance.
func referencesFromToolResult(toolName string, result *types.ToolResult) []*types.SearchResult {
	if result == nil || !result.Success || result.Data == nil {
		return nil
	}

	displayType, _ := result.Data["display_type"].(string)
	var rows interface{}
	switch {
	case toolName == agenttools.ToolKnowledgeSearch && displayType == "search_results":
		rows = result.Data["results"]
	case toolName == agenttools.ToolGrepChunks && displayType == "grep_results":
		rows = result.Data["chunk_results"]
	default:
		return nil
	}

	return searchResultsFromRows(rows)
}

func searchResultsFromRows(value interface{}) []*types.SearchResult {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(encoded, &rows); err != nil {
		return nil
	}

	references := make([]*types.SearchResult, 0, len(rows))
	for _, row := range rows {
		chunkID := firstReferenceString(row, "chunk_id", "faq_id", "id")
		if chunkID == "" {
			continue
		}
		chunkType := referenceString(row, "chunk_type")
		if referenceString(row, "faq_id") != "" && chunkType == "" {
			chunkType = string(types.ChunkTypeFAQ)
		}
		chunkIndex := referenceInt(row, "chunk_index")
		if chunkIndex == 0 {
			chunkIndex = referenceInt(row, "index")
		}
		references = append(references, &types.SearchResult{
			ID:                chunkID,
			Content:           referenceString(row, "content"),
			KnowledgeID:       referenceString(row, "knowledge_id"),
			KnowledgeBaseID:   firstReferenceString(row, "knowledge_base_id", "knowledge_base"),
			KnowledgeTitle:    firstReferenceString(row, "knowledge_title", "title", "faq_question"),
			ChunkIndex:        chunkIndex,
			ChunkType:         chunkType,
			MatchedContent:    firstReferenceString(row, "matched_content", "match_snippet"),
			KnowledgeFilename: referenceString(row, "knowledge_filename"),
			KnowledgeSource:   referenceString(row, "knowledge_source"),
			KnowledgeType:     referenceString(row, "knowledge_type"),
		})
	}
	return references
}

func firstReferenceString(row map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := referenceString(row, key); value != "" {
			return value
		}
	}
	return ""
}

func referenceString(row map[string]interface{}, key string) string {
	value, _ := row[key].(string)
	return value
}

func referenceInt(row map[string]interface{}, key string) int {
	switch value := row[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
