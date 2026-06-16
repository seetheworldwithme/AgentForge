// internal/rag/chunker/tokenizer.go
package chunker

import "unicode"

func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var tokens int
	var cjkCount, wordChars int
	flushWord := func() {
		if wordChars > 0 {
			tokens += wordChars/4 + 1
			wordChars = 0
		}
	}
	flushCJK := func() {
		if cjkCount > 0 {
			tokens += int(float64(cjkCount) * 1.5)
			cjkCount = 0
		}
	}
	for _, r := range text {
		if isCJK(r) {
			flushWord()
			cjkCount++
		} else if unicode.IsSpace(r) {
			flushWord()
			flushCJK()
		} else {
			flushCJK()
			wordChars++
		}
	}
	flushWord()
	flushCJK()
	return tokens
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x3040 && r <= 0x30FF)
}
