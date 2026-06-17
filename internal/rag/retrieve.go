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

type SearchHit struct {
	ChunkID    string
	DocumentID string
	Filename   string
	Ordinal    int
	Text       string
	Distance   float32
}

func (r *Retriever) Retrieve(ctx context.Context, kbID, query string, k int) ([]agent.RetrievedChunk, error) {
	hits, err := r.Search(ctx, kbID, query, k)
	if err != nil {
		return nil, err
	}
	out := make([]agent.RetrievedChunk, 0, len(hits))
	for _, h := range hits {
		out = append(out, agent.RetrievedChunk{
			ID: h.ChunkID, Text: h.Text, DocID: h.DocumentID, Filename: h.Filename,
		})
	}
	return out, nil
}

func (r *Retriever) Search(ctx context.Context, kbID, query string, k int) ([]SearchHit, error) {
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
	dist := make(map[string]float32, len(hits))
	for _, h := range hits {
		dist[h.ID] = h.Distance
	}
	// attach filename via document lookup
	out := make([]SearchHit, 0, len(chunks))
	for _, c := range chunks {
		doc, _ := r.DB.GetDocument(c.DocID)
		out = append(out, SearchHit{
			ChunkID: c.ID, DocumentID: c.DocID, Filename: doc.Filename,
			Ordinal: c.Ordinal, Text: c.Text, Distance: dist[c.ID],
		})
	}
	return out, nil
}
