// internal/rag/chunker/chunker.go
package chunker

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/agentforge/agentforge/internal/rag"
)

type Chunker interface {
	Chunk(doc rag.RawDocument) ([]rag.Chunk, error)
}

type Options struct {
	ChunkSize int
	Overlap   int
}

func New(filePath string, opts Options) (Chunker, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 512
	}
	if opts.Overlap < 0 {
		opts.Overlap = 50
	}
	switch ext {
	case ".md", ".markdown", ".txt":
		return &TextChunker{opts: opts}, nil
	case ".pdf":
		return &PDFChunker{opts: opts}, nil
	case ".docx", ".pptx", ".xlsx":
		return &OfficeChunker{opts: opts}, nil
	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}
}
