package rag

import (
	"context"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

// Retriever implements agent.RAGRetriever over SQLite + sqlite-vec.
type Retriever struct {
	DB          *store.DB
	EmbedClient llm.LLMClient
}

func (r *Retriever) Retrieve(ctx context.Context, kbID, query string, k int) ([]agent.RetrievedChunk, error) {
	if k <= 0 {
		k = 5
	}
	vecs, err := r.EmbedClient.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, err
	}
	hits, err := r.DB.SearchVectors(store.SanitizeTableName(kbID), vecs[0], k)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	chunks, err := r.DB.GetChunksByIDs(ids)
	if err != nil {
		return nil, err
	}
	// attach filename via document lookup
	out := make([]agent.RetrievedChunk, 0, len(chunks))
	for _, c := range chunks {
		doc, _ := r.DB.GetDocument(c.DocID)
		out = append(out, agent.RetrievedChunk{
			ID: c.ID, Text: c.Text, DocID: c.DocID, Filename: doc.Filename,
		})
	}
	return out, nil
}
