// Package retrievaltrace records the non-content metadata needed to audit one
// RAG request. It is intentionally independent of messages.AgentSteps: this
// data is for tenant administrators only and must never flow through history.
package retrievaltrace

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	SnapshotVersion = 1
	MaxCandidates   = 500
)

type contextKey struct{}

// Snapshot is the stable, versioned JSON payload persisted per user request.
type Snapshot struct {
	TraceVersion  int         `json:"trace_version"`
	OriginalQuery string      `json:"original_query"`
	Steps         []TraceStep `json:"steps"`
}

type TraceStep struct {
	Sequence         int        `json:"sequence"`
	Kind             string     `json:"kind"`
	Source           string     `json:"source,omitempty"`
	Status           string     `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	Query            string     `json:"query,omitempty"`
	RewrittenQuery   string     `json:"rewritten_query,omitempty"`
	ExpansionQueries []string   `json:"expansion_queries,omitempty"`
	Retrieval        *Retrieval `json:"retrieval,omitempty"`
	Rerank           *Rerank    `json:"rerank,omitempty"`
	ErrorSummary     string     `json:"error_summary,omitempty"`
}

type Retrieval struct {
	KnowledgeBaseID   string      `json:"knowledge_base_id,omitempty"`
	KnowledgeBaseName string      `json:"knowledge_base_name,omitempty"`
	ReturnedCount     int         `json:"returned_count"`
	CapturedCount     int         `json:"captured_count"`
	Truncated         bool        `json:"truncated,omitempty"`
	Candidates        []Candidate `json:"candidates,omitempty"`
}

type Rerank struct {
	ModelID        string      `json:"model_id,omitempty"`
	ModelName      string      `json:"model_name,omitempty"`
	Threshold      float64     `json:"threshold"`
	CandidateCount int         `json:"candidate_count"`
	CapturedCount  int         `json:"captured_count"`
	Truncated      bool        `json:"truncated,omitempty"`
	Candidates     []Candidate `json:"candidates,omitempty"`
}

type Candidate struct {
	KnowledgeBaseID   string   `json:"knowledge_base_id,omitempty"`
	KnowledgeBaseName string   `json:"knowledge_base_name,omitempty"`
	KnowledgeID       string   `json:"knowledge_id,omitempty"`
	KnowledgeName     string   `json:"knowledge_name,omitempty"`
	ChunkID           string   `json:"chunk_id"`
	RetrievalScore    float64  `json:"retrieval_score"`
	ModelScore        *float64 `json:"model_score,omitempty"`
	FinalScore        *float64 `json:"final_score,omitempty"`
	Selected          bool     `json:"selected"`
}

// Recorder safely accepts writes from concurrent retrieval workers.
type Recorder struct {
	mu       sync.Mutex
	snapshot Snapshot
	sequence int
}

func NewRecorder(originalQuery string) *Recorder {
	return &Recorder{snapshot: Snapshot{TraceVersion: SnapshotVersion, OriginalQuery: originalQuery}}
}

func WithRecorder(ctx context.Context, recorder *Recorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, recorder)
}

func FromContext(ctx context.Context) *Recorder {
	recorder, _ := ctx.Value(contextKey{}).(*Recorder)
	return recorder
}

func (r *Recorder) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{TraceVersion: SnapshotVersion}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := r.snapshot
	copy.Steps = append([]TraceStep(nil), r.snapshot.Steps...)
	return copy
}

func (r *Recorder) append(step TraceStep) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence++
	step.Sequence = r.sequence
	step.CreatedAt = time.Now().UTC()
	r.snapshot.Steps = append(r.snapshot.Steps, step)
}

func RecordRewrite(ctx context.Context, original, rewritten string) {
	if recorder := FromContext(ctx); recorder != nil {
		recorder.append(TraceStep{Kind: "query_rewrite", Source: "rag", Status: "completed", Query: original, RewrittenQuery: rewritten})
	}
}

func RecordExpansion(ctx context.Context, original string, expansions []string) {
	if recorder := FromContext(ctx); recorder != nil {
		recorder.append(TraceStep{Kind: "query_expansion", Source: "rag", Status: "completed", Query: original, ExpansionQueries: append([]string(nil), expansions...)})
	}
}

func RecordRetrieval(ctx context.Context, source, query, kbID string, results []*types.SearchResult, err error) {
	recorder := FromContext(ctx)
	if recorder == nil {
		return
	}
	retrieval := Retrieval{KnowledgeBaseID: kbID, ReturnedCount: len(results)}
	retrieval.Candidates, retrieval.Truncated = candidates(results, nil, nil)
	retrieval.CapturedCount = len(retrieval.Candidates)
	status := "completed"
	if err != nil {
		status = "failed"
	}
	if err == nil && len(results) == 0 {
		status = "empty"
	}
	recorder.append(TraceStep{Kind: "retrieval", Source: source, Status: status, Query: query, Retrieval: &retrieval, ErrorSummary: safeError(err)})
}

// RecordRerank records every input candidate even when the reranker falls
// back to the raw retrieval result. modelScores and selected are keyed by
// chunk ID; final scores are read from selected SearchResults.
func RecordRerank(
	ctx context.Context, source, query, modelID, modelName string, threshold float64,
	candidatesIn []*types.SearchResult, modelScores map[string]float64, selectedResults []*types.SearchResult,
	status string, err error,
) {
	recorder := FromContext(ctx)
	if recorder == nil {
		return
	}
	if status == "" {
		status = "completed"
	}
	selected := make(map[string]*types.SearchResult, len(selectedResults))
	for _, result := range selectedResults {
		if result != nil && result.ID != "" {
			selected[result.ID] = result
		}
	}
	rows, truncated := candidates(candidatesIn, modelScores, selected)
	rerank := Rerank{ModelID: modelID, ModelName: modelName, Threshold: threshold, CandidateCount: len(candidatesIn), CapturedCount: len(rows), Truncated: truncated, Candidates: rows}
	recorder.append(TraceStep{Kind: "rerank", Source: source, Status: status, Query: query, Rerank: &rerank, ErrorSummary: safeError(err)})
}

func candidates(results []*types.SearchResult, modelScores map[string]float64, selected map[string]*types.SearchResult) ([]Candidate, bool) {
	limit := len(results)
	truncated := false
	if limit > MaxCandidates {
		limit = MaxCandidates
		truncated = true
	}
	rows := make([]Candidate, 0, limit)
	for _, result := range results[:limit] {
		if result == nil {
			continue
		}
		score := result.Score
		if raw := result.Metadata["base_score"]; raw != "" {
			if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
				score = parsed
			}
		}
		row := Candidate{KnowledgeBaseID: result.KnowledgeBaseID, KnowledgeID: result.KnowledgeID, KnowledgeName: knowledgeName(result), ChunkID: result.ID, RetrievalScore: score}
		if modelScores != nil {
			if modelScore, ok := modelScores[result.ID]; ok {
				row.ModelScore = &modelScore
			}
		}
		if final, ok := selected[result.ID]; ok {
			finalScore := final.Score
			row.FinalScore = &finalScore
			row.Selected = true
		}
		rows = append(rows, row)
	}
	return rows, truncated
}

func knowledgeName(result *types.SearchResult) string {
	if result == nil {
		return ""
	}
	if strings.TrimSpace(result.KnowledgeTitle) != "" {
		return result.KnowledgeTitle
	}
	return result.KnowledgeFilename
}

var secretValue = regexp.MustCompile(`(?i)((?:api[_-]?key|authorization|token|password)\s*(?:=|:)\s*)([^\s,;]+)`)

func safeError(err error) string {
	if err == nil {
		return ""
	}
	value := secretValue.ReplaceAllString(err.Error(), "$1[redacted]")
	value = strings.TrimSpace(value)
	if len(value) > 256 {
		return fmt.Sprintf("%s…", value[:255])
	}
	return value
}
