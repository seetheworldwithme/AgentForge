package rag

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkSplitsBySizeWithOverlap(t *testing.T) {
	text := strings.Repeat("a", 2000) // 2000 chars
	chunks := Chunk(text, 800, 100)
	if len(chunks) < 3 {
		t.Fatalf("len=%d, want >=3", len(chunks))
	}
	for i, c := range chunks {
		if len(c) == 0 {
			t.Errorf("chunk %d empty", i)
		}
		if rc := utf8.RuneCountInString(c); rc > 800 {
			t.Errorf("chunk %d runes=%d > 800", i, rc)
		}
	}
}

// TestChunkOverlapSharesLeaves 验证 overlap 在叶子边界生效：相邻 chunk 应共享
// 至少一个完整句子（而非旧的固定字符滑窗重叠）。
func TestChunkOverlapSharesLeaves(t *testing.T) {
	sentences := []string{
		"这是第一句话用于测试切分。",
		"这是第二句话内容继续展开。",
		"这是第三句话不断向前推进。",
		"这是第四句话快要接近结尾。",
		"这是第五句话作为最后一句。",
	}
	text := strings.Join(sentences, "")
	chunks := Chunk(text, 50, 20)
	if len(chunks) < 2 {
		t.Fatalf("len=%d, want >=2", len(chunks))
	}
	shared := false
	for i := 1; i < len(chunks); i++ {
		for _, s := range sentences {
			if strings.Contains(chunks[i-1], s) && strings.Contains(chunks[i], s) {
				shared = true
			}
		}
	}
	if !shared {
		t.Errorf("overlap=20 but no sentence shared between adjacent chunks: %v", chunks)
	}
}

// TestChunkChineseNoBrokenRunes 防止 🔴 回归：纯中文无标点长文本走滑窗兜底，
// 绝不能切出半截汉字（旧实现的 byte-slice bug）。
func TestChunkChineseNoBrokenRunes(t *testing.T) {
	text := strings.Repeat("中文字符测试切片安全", 50) // 每字 3 字节，强制字节边界错位
	chunks := Chunk(text, 10, 2)
	if len(chunks) < 2 {
		t.Fatalf("len=%d, want >=2", len(chunks))
	}
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Errorf("chunk %d is invalid UTF-8 (broken rune): %q", i, c)
		}
	}
}

// TestChunkRespectsSentenceBoundary 验证在中文句末标点（。）边界切分，且不破坏 UTF-8。
func TestChunkRespectsSentenceBoundary(t *testing.T) {
	text := "这是一段完整的句子。这是另一段完整的句子。还有第三段句子内容。"
	chunks := Chunk(text, 15, 3)
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "。") {
		t.Errorf("sentences lost: %q", joined)
	}
	for i, c := range chunks {
		if !utf8.ValidString(c) {
			t.Errorf("chunk %d invalid utf8: %q", i, c)
		}
	}
}

// TestChunkCodeBlockIntact 验证 ``` 围栏代码块作为整体保护，不被内部换行/标点拆散。
func TestChunkCodeBlockIntact(t *testing.T) {
	code := "```go\nfunc main() {\n\tprintln(\"a.b.c\")\n}\n```"
	text := "前言说明。\n" + code + "\n后续说明。"
	chunks := Chunk(text, 30, 5)
	found := false
	for _, c := range chunks {
		if strings.Contains(c, "```go") && strings.Contains(c, "func main()") {
			found = true
		}
	}
	if !found {
		t.Errorf("code block not kept intact in any chunk: %v", chunks)
	}
}

func TestChunkSmallTextReturnsOne(t *testing.T) {
	got := Chunk("hello", 800, 100)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got=%+v", got)
	}
}

func TestChunkEmptyReturnsNil(t *testing.T) {
	got := Chunk("", 800, 100)
	if got != nil {
		t.Errorf("got=%+v, want nil", got)
	}
}
