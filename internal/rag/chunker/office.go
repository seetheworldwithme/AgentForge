// internal/rag/chunker/office.go
package chunker

import (
	"fmt"

	"github.com/agentforge/agentforge/internal/rag"
)

type OfficeChunker struct{ opts Options }

func (c *OfficeChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	return nil, fmt.Errorf("Office chunker not implemented yet (T34)")
}
