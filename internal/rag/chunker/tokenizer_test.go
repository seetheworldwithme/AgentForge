// internal/rag/chunker/tokenizer_test.go
package chunker

import "testing"

func TestEstimateTokens_Chinese(t *testing.T) {
	got := EstimateTokens("你好世界你好世界你好世界") // 10 中文字
	if got < 12 || got > 20 {
		t.Errorf("chinese 10 chars: expected 12-20, got %d", got)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("empty: expected 0, got %d", got)
	}
}
