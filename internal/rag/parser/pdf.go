package parser

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

type PDF struct{}

func (PDF) Supports(mime, fn string) bool {
	return mime == "application/pdf" || strings.HasSuffix(strings.ToLower(fn), ".pdf")
}

func (PDF) Parse(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	text := extractPDFLiteralText(string(b))
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no extractable PDF text found")
	}
	return text, nil
}

func extractPDFLiteralText(raw string) string {
	re := regexp.MustCompile(`\((?:\\.|[^\\)])*\)`)
	matches := re.FindAllString(raw, -1)
	if len(matches) == 0 {
		return ""
	}
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		s := strings.TrimSuffix(strings.TrimPrefix(m, "("), ")")
		s = strings.ReplaceAll(s, `\(`, "(")
		s = strings.ReplaceAll(s, `\)`, ")")
		s = strings.ReplaceAll(s, `\\`, `\`)
		s = strings.ReplaceAll(s, `\n`, "\n")
		s = strings.ReplaceAll(s, `\r`, "\n")
		s = strings.ReplaceAll(s, `\t`, "\t")
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}
