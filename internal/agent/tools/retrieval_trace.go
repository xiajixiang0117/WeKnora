package tools

import (
	"context"

	"github.com/Tencent/WeKnora/internal/retrievaltrace"
	"github.com/Tencent/WeKnora/internal/types"
)

func unwrapSearchResults(results []*searchResultWithMeta) []*types.SearchResult {
	plain := make([]*types.SearchResult, 0, len(results))
	for _, result := range results {
		if result != nil && result.SearchResult != nil {
			snapshot := *result.SearchResult
			plain = append(plain, &snapshot)
		}
	}
	return plain
}

func recordAgentRetrievals(ctx context.Context, queries []string, targets types.SearchTargets, results []*searchResultWithMeta) {
	byQueryAndKB := make(map[string][]*types.SearchResult)
	for _, result := range results {
		if result == nil || result.SearchResult == nil {
			continue
		}
		key := result.SourceQuery + "\x00" + result.KnowledgeBaseID
		byQueryAndKB[key] = append(byQueryAndKB[key], result.SearchResult)
	}
	for _, query := range queries {
		for _, target := range targets {
			if target == nil || target.KnowledgeBaseID == "" {
				continue
			}
			key := query + "\x00" + target.KnowledgeBaseID
			retrievaltrace.RecordRetrieval(ctx, "agent", query, target.KnowledgeBaseID, byQueryAndKB[key], nil)
		}
	}
}
