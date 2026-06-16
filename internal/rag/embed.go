package rag

import (
	"context"

	"github.com/agent-rust/core/internal/llm"
)

// Embedder calls an LLM client's embedding endpoint.
type Embedder struct {
	Client llm.LLMClient
}

func (e Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.Client.Embed(ctx, texts)
}
