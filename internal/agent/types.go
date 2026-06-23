package agent

import (
	"context"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/tools"
)

// EventEmitter is how the orchestrator pushes live updates to the HTTP layer.
type EventEmitter interface {
	Emit(event string, data any)
}

// RAGRetriever is the minimal RAG surface the orchestrator needs.
type RAGRetriever interface {
	Retrieve(ctx context.Context, kbID, query string, k int) ([]RetrievedChunk, error)
}

type SkillProvider interface {
	EnabledInstructions() (string, error)
}

type RetrievedChunk struct {
	ID         string
	Text       string
	DocID      string
	Filename   string
	Similarity float32 // cosine similarity in [-1, 1]; higher = more relevant, 0 when unknown
}

// Deps bundles the orchestrator's dependencies (DI).
type Deps struct {
	LLM    llm.LLMClient
	Tools  *tools.Engine
	RAG    RAGRetriever  // may be nil when RAG is off
	Skills SkillProvider // may be nil when skills are off
	// MaxIter is the tool-iteration checkpoint interval: every MaxIter tool
	// iterations the loop logs and emits a status checkpoint. It is NOT a hard
	// cap — a task keeps running until it produces a final text answer, hits
	// the MaxToolCalls cap, or the context is canceled. Defaults to 20.
	MaxIter int
	// MaxToolCalls is the hard cap on total tool executions within a single
	// Run. When reached, no further tools run and the model is nudged to give
	// a final answer from the results it already has. 0 = no cap.
	MaxToolCalls int
}
