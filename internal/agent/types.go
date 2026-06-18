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

type RetrievedChunk struct {
	ID         string
	Text       string
	DocID      string
	Filename   string
	Similarity float32 // cosine similarity in [-1, 1]; higher = more relevant, 0 when unknown
}

// Deps bundles the orchestrator's dependencies (DI).
type Deps struct {
	LLM     llm.LLMClient
	Tools   *tools.Engine
	RAG     RAGRetriever // may be nil when RAG is off
	MaxIter int          // safety cap, default 20
}
