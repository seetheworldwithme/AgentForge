package rag

import (
	"context"
	"fmt"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

// Retriever implements agent.RAGRetriever over SQLite + sqlite-vec. It resolves
// the embed client per knowledge base (matching the model used at ingest time,
// so query and document vectors share a dimension); EmbedClient is the fallback
// for KBs that haven't bound a provider.
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

// CosineSimilarity converts an L2 distance reported by a vec0 index (on
// normalized embeddings) to a cosine similarity in [-1, 1]: similarity = 1 - d²/2.
// L2 and cosine are monotonic on unit vectors, so ranking is unchanged; the
// number is just on the intuitive "higher = more relevant" scale.
func CosineSimilarity(l2 float32) float32 { return 1 - l2*l2/2 }

func (r *Retriever) Retrieve(ctx context.Context, kbID, query string, k int) ([]agent.RetrievedChunk, error) {
	hits, err := r.Search(ctx, kbID, query, k)
	if err != nil {
		return nil, err
	}
	out := make([]agent.RetrievedChunk, 0, len(hits))
	for _, h := range hits {
		out = append(out, agent.RetrievedChunk{
			ID: h.ChunkID, Text: h.Text, DocID: h.DocumentID, Filename: h.Filename,
			Similarity: CosineSimilarity(h.Distance),
		})
	}
	return out, nil
}

// embedClientForKB returns the embed client bound to the KB so retrieval uses
// the same model (and dimension) as ingest, falling back to the default.
func (r *Retriever) embedClientForKB(kbID string) llm.LLMClient {
	if kb, err := r.DB.GetKB(kbID); err == nil && kb.EmbedProviderID != "" {
		if p, err := r.DB.GetProvider(kb.EmbedProviderID); err == nil && p.EmbedModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.EmbedModel,
			})
		}
	}
	return r.EmbedClient
}

func (r *Retriever) Search(ctx context.Context, kbID, query string, k int) ([]SearchHit, error) {
	if k <= 0 {
		k = 5
	}
	client := r.embedClientForKB(kbID)
	if client == nil {
		return nil, fmt.Errorf("no embed provider configured for knowledge base")
	}
	vecs, err := client.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed query returned no vectors")
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
	// index chunks by id so results keep SearchVectors' similarity-descending
	// order (WHERE id IN (...) does not preserve row order).
	byID := make(map[string]store.Chunk, len(chunks))
	for _, c := range chunks {
		byID[c.ID] = c
	}
	out := make([]SearchHit, 0, len(hits))
	for _, h := range hits {
		c := byID[h.ID]
		doc, _ := r.DB.GetDocument(c.DocID)
		out = append(out, SearchHit{
			ChunkID: c.ID, DocumentID: c.DocID, Filename: doc.Filename,
			Ordinal: c.Ordinal, Text: c.Text, Distance: h.Distance,
		})
	}
	return out, nil
}
