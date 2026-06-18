package parser

import (
	"io"
	"strings"
)

type Txt struct{}

func (Txt) Supports(mime, fn string) bool {
	return strings.HasPrefix(mime, "text/plain") || strings.HasSuffix(fn, ".txt") || mime == ""
}
func (Txt) Parse(r io.Reader) (ParseResult, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return ParseResult{}, err
	}
	return ParseResult{Text: string(b)}, nil
}

type Markdown struct{}

func (Markdown) Supports(mime, fn string) bool {
	return strings.HasSuffix(fn, ".md") || mime == "text/markdown"
}
func (Markdown) Parse(r io.Reader) (ParseResult, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return ParseResult{}, err
	}
	// MVP: return raw markdown; chunking handles size.
	return ParseResult{Text: string(b)}, nil
}
