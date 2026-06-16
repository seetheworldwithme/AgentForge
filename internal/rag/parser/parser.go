package parser

import "io"

// Parser parses a document into plain text suitable for chunking+embedding.
type Parser interface {
	Supports(mimeType, filename string) bool
	Parse(r io.Reader) (string, error)
}
