package rag

import (
	"bytes"
	"context"
	"fmt"
	"log"

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

// IngestFile parses, chunks, embeds (batched), and stores a document. Every
// failure path marks the document "failed" with a reason and logs it, so an
// ingest that dies mid-way never leaves the document stuck on "processing".
func (ing *Ingestor) IngestFile(ctx context.Context, docID, filename, mimeType string, raw []byte) error {
	log.Printf("ingest: doc=%s file=%q start", docID, filename)
	p := ing.Parser
	if p == nil {
		p = parser.Txt{}
	}
	text, err := p.Parse(bytes.NewReader(raw))
	if err != nil {
		ing.fail(docID, "parse", 0, err)
		return err
	}
	chunks := Chunk(text, ing.chunkSize(), ing.overlap())
	log.Printf("ingest: doc=%s parsed %d chars -> %d chunks", docID, len(text), len(chunks))

	// Ensure the vec0 table exists lazily, once we know the embedding dim.
	dim := 0
	const batch = 64
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		vecs, err := ing.Embed.Embed(ctx, chunks[i:end])
		if err != nil {
			ing.fail(docID, "embed", i, err)
			return err
		}
		if len(vecs) != end-i {
			err := fmt.Errorf("embed returned %d vectors for %d chunks", len(vecs), end-i)
			ing.fail(docID, "embed", i, err)
			return err
		}
		for j, vec := range vecs {
			idx := i + j
			chunkID := "chunk_" + ulid.Make().String()
			if err := ing.DB.CreateChunk(store.Chunk{
				ID: chunkID, DocID: docID, KBID: ing.KBID, Ordinal: idx, Text: chunks[idx],
			}); err != nil {
				ing.fail(docID, "store", idx, err)
				return err
			}
			if dim == 0 {
				dim = len(vec)
				if err := ing.DB.EnsureVecTable(vecTable(ing.KBID), dim); err != nil {
					ing.fail(docID, "vec-table", idx, err)
					return err
				}
			}
			if err := ing.DB.InsertVector(vecTable(ing.KBID), chunkID, vec); err != nil {
				ing.fail(docID, "store", idx, err)
				return err
			}
		}
		log.Printf("ingest: doc=%s embedded %d/%d chunks", docID, end, len(chunks))
	}

	_ = ing.DB.SetDocumentStatus(docID, "ready", len(chunks), "")
	log.Printf("ingest: doc=%s ready (%d chunks)", docID, len(chunks))
	return nil
}

// fail marks the document failed and logs the stage + cause. `done` is the
// number of chunks already written, surfaced as chunk_count for debugging.
func (ing *Ingestor) fail(docID, stage string, done int, err error) {
	log.Printf("ingest: doc=%s FAILED at %s: %v", docID, stage, err)
	_ = ing.DB.SetDocumentStatus(docID, "failed", done, stage+": "+err.Error())
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
