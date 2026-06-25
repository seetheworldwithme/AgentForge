package agent

import (
	"testing"

	"github.com/agent-rust/core/internal/llm"
)

func TestEstimateTokens(t *testing.T) {
	// "hello world" 11 个 ASCII 字符 → 11/4 = 2
	if got := EstimateTokens("hello world"); got != 2 {
		t.Fatalf(`EstimateTokens("hello world") = %d, want 2`, got)
	}
	// "你好世界" 4 个 CJK 字符 → 4
	if got := EstimateTokens("你好世界"); got != 4 {
		t.Fatalf(`EstimateTokens("你好世界") = %d, want 4`, got)
	}
	// 空串 → 0
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf(`EstimateTokens("") = %d, want 0`, got)
	}

	// 中英混合：cjk(你好=2) + other/4(hello=5 → 1) = 3
	mixed := "你好hello"
	want := 2 + 5/4
	if got := EstimateTokens(mixed); got != want {
		t.Fatalf(`EstimateTokens(%q) = %d, want %d`, mixed, got, want)
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	msg := llm.Message{
		Role:    llm.RoleAssistant,
		Content: "你好世界", // 4
		ToolCalls: []llm.ToolCall{
			{
				ID:   "call_1",
				Name: "search",
				Args: `{"q":"你好","limit":2}`, // 由 EstimateTokens 计算
			},
		},
		Images: []llm.ImageRef{
			{DataURL: "data:image/png;base64," + repeat('x', 1500)}, // 1500/750 = 2
		},
	}

	want := EstimateTokens(msg.Content) +
		EstimateTokens(msg.ToolCalls[0].Args) +
		len(msg.Images[0].DataURL)/750

	if got := EstimateMessageTokens(msg); got != want {
		t.Fatalf("EstimateMessageTokens = %d, want %d", got, want)
	}

	// 明确关键分量，防止估算逻辑被无意改动
	if want != 4+EstimateTokens(msg.ToolCalls[0].Args)+2 {
		t.Fatalf("分量校验失败: got want=%d", want)
	}
}

// repeat 返回由 n 个 r 组成的字符串，仅供测试构造 DataURL。
func repeat(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
