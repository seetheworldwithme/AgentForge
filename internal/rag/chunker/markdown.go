// internal/rag/chunker/markdown.go
package chunker

import "github.com/agentforge/agentforge/internal/rag"

type TextChunker struct{ opts Options }

func (c *TextChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	return nil, nil // T21 实现
}
