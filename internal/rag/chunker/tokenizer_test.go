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

// TestEstimateTokens_LatinAndMixed 守护 latin（wordChars/4+1）与 CJK↔latin
// 切换时的 flush 逻辑——这是该算法最易出 bug 的地方：若误删 flushWord/flushCJK，
// 会重复计数或丢字符。此测试会捕获这类回归。
func TestEstimateTokens_LatinAndMixed(t *testing.T) {
	// 纯英文：两个 "hello" 词，各 5 字符 → 5/4+1=2 each，合计 4
	if got := EstimateTokens("hello hello"); got != 4 {
		t.Errorf("latin 'hello hello': expected 4, got %d", got)
	}
	// 混合：2 中文字(×1.5=3) + 5 latin(2) = 5。验证切换不丢不重。
	if got := EstimateTokens("你好hello"); got != 5 {
		t.Errorf("mixed '你好hello': expected 5, got %d", got)
	}
}
