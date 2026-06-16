// internal/rag/chunker/pdf.go
package chunker

import (
	"fmt"

	"github.com/agentforge/agentforge/internal/rag"
)

type PDFChunker struct{ opts Options }

func (c *PDFChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	return nil, fmt.Errorf("PDF chunker not implemented yet (T33)")
}
