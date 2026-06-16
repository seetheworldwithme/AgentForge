package rag

import (
	"bytes"
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/rag/parser"
	"github.com/agent-rust/core/internal/store"
)

// Ingestor parses, chunks, embeds (batched), and stores a document.
type Ingestor struct {
	DB      *store.DB
	Embed   llm.LLMClient
	KBID    string
	ChunkSz int
	Overlap int
	Parser  parser.Parser // the chosen parser for the file
}

// IngestFile parses, chunks, embeds (batched), and stores a document.
func (ing *Ingestor) IngestFile(ctx context.Context, docID, filename, mimeType string, raw []byte) error {
	p := ing.Parser
	if p == nil {
		p = parser.Txt{}
	}
	text, err := p.Parse(bytes.NewReader(raw))
	if err != nil {
		_ = ing.DB.SetDocumentStatus(docID, "failed", 0, "parse: "+err.Error())
		return err
	}
	chunks := Chunk(text, ing.chunkSize(), ing.overlap())

	// ensure vec table exists lazily once we know the embedding dimension
	dim := 0
	for i := 0; i < len(chunks); i += 64 {
		end := i + 64
		if end > len(chunks) {
			end = len(chunks)
		}
		vecs, err := ing.Embed.Embed(ctx, chunks[i:end])
		if err != nil {
			_ = ing.DB.SetDocumentStatus(docID, "failed", i, "embed: "+err.Error())
			return err
		}
		for j, vec := range vecs {
			idx := i + j
			chunkID := "chunk_" + ulid.Make().String()
			if err := ing.DB.CreateChunk(store.Chunk{
				ID: chunkID, DocID: docID, KBID: ing.KBID, Ordinal: idx, Text: chunks[idx],
			}); err != nil {
				return err
			}
			if dim == 0 {
				dim = len(vec)
				if err := ing.DB.EnsureVecTable(vecTable(ing.KBID), dim); err != nil {
					return err
				}
			}
			if err := ing.DB.InsertVector(vecTable(ing.KBID), chunkID, vec); err != nil {
				return err
			}
		}
	}

	_ = ing.DB.SetDocumentStatus(docID, "ready", len(chunks), "")
	return nil
}

func (ing *Ingestor) chunkSize() int {
	if ing.ChunkSz > 0 {
		return ing.ChunkSz
	}
	return 800
}
func (ing *Ingestor) overlap() int {
	if ing.Overlap > 0 {
		return ing.Overlap
	}
	return 100
}

func vecTable(kbID string) string { return store.SanitizeTableName(kbID) }
