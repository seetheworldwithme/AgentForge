package server

import (
	"testing"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

// TestStoreMsgToLLMRestoresToolCalls 验证从数据库加载历史时，assistant 的
// tool_calls(JSON 字符串) 被正确还原成 []llm.ToolCall。之前 storeMsgToLLM
// 丢弃了 ToolCalls，导致跨请求续聊时上下文断裂。
func TestStoreMsgToLLMRestoresToolCalls(t *testing.T) {
	m := store.Message{
		Role:      "assistant",
		Content:   "let me read the file",
		ToolCalls: `[{"id":"call_1","name":"file_read","args":"{\"path\":\"/a/b.txt\"}"}]`,
	}
	got := storeMsgToLLM(m, false)
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call restored, got %d", len(got.ToolCalls))
	}
	tc := got.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "file_read" || tc.Args != `{"path":"/a/b.txt"}` {
		t.Errorf("restored tool call = %+v", tc)
	}
	if got.Role != llm.RoleAssistant {
		t.Errorf("role = %q, want %q", got.Role, llm.RoleAssistant)
	}
	if got.Content != m.Content {
		t.Errorf("content mismatch")
	}
}

// TestStoreMsgToLLMEmptyToolCalls 验证无 tool_calls 的消息（user/tool/system）
// 不会因空字符串报错，也不会产生幽灵 tool call。
func TestStoreMsgToLLMEmptyToolCalls(t *testing.T) {
	for _, raw := range []string{"", "  ", "[]"} {
		got := storeMsgToLLM(store.Message{Role: "user", Content: "hi", ToolCalls: raw}, false)
		if len(got.ToolCalls) != 0 {
			t.Errorf("ToolCalls=%q produced %d calls, want 0", raw, len(got.ToolCalls))
		}
	}
}

// TestStoreMsgToLLMToolRolePreservesToolCallID 验证工具结果消息(role=tool)
// 的 tool_call_id 仍被正确保留（该字段之前就有，此处做回归保护）。
func TestStoreMsgToLLMToolRolePreservesToolCallID(t *testing.T) {
	m := store.Message{Role: "tool", Content: "result", ToolCallID: "call_1"}
	got := storeMsgToLLM(m, false)
	if got.ToolCallID != "call_1" {
		t.Errorf("tool_call_id = %q, want call_1", got.ToolCallID)
	}
	if len(got.ToolCalls) != 0 {
		t.Errorf("tool role should have no ToolCalls, got %d", len(got.ToolCalls))
	}
}

// TestStoreMsgToLLMRestoresImagesWhenVision 验证当前模型支持视觉时，
// 历史用户消息的图片（JSON dataURL 数组）被回填为多模态 ImageRef；
// 非 vision 模型则不回填——避免纯文本模型续聊带图历史被 provider 以 400 拒绝。
func TestStoreMsgToLLMRestoresImagesWhenVision(t *testing.T) {
	urls := `["data:image/png;base64,AAAA","data:image/png;base64,BBBB"]`
	m := store.Message{Role: "user", Content: "看这张图", Images: urls}

	got := storeMsgToLLM(m, true)
	if len(got.Images) != 2 {
		t.Fatalf("vision=true expected 2 images, got %d", len(got.Images))
	}
	if got.Images[0].DataURL != "data:image/png;base64,AAAA" {
		t.Errorf("first image DataURL = %q", got.Images[0].DataURL)
	}

	plain := storeMsgToLLM(m, false)
	if len(plain.Images) != 0 {
		t.Errorf("vision=false expected 0 images, got %d", len(plain.Images))
	}
}
