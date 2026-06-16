package parser

import (
	"io"
	"strings"
)

type Txt struct{}

func (Txt) Supports(mime, fn string) bool {
	return strings.HasPrefix(mime, "text/plain") || strings.HasSuffix(fn, ".txt") || mime == ""
}
func (Txt) Parse(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type Markdown struct{}

func (Markdown) Supports(mime, fn string) bool {
	return strings.HasSuffix(fn, ".md") || mime == "text/markdown"
}
func (Markdown) Parse(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	// MVP: return raw markdown; chunking handles size.
	return string(b), nil
}
