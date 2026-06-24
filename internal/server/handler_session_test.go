package server

import (
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/store"
)

func TestMessagesForDisplayCombinesToolCallsAndResults(t *testing.T) {
	msgs := []store.Message{
		{ID: "u1", SessionID: "sess_1", Role: "user", Content: "run"},
		{
			ID:        "a1",
			SessionID: "sess_1",
			Role:      "assistant",
			Content:   "I will inspect it.",
			ToolCalls: `[{"id":"call_1","name":"bash","args":"{\"command\":\"excelcli inspect /tmp/a.db\",\"timeout\":120}"}]`,
		},
		{ID: "t1", SessionID: "sess_1", Role: "tool", Content: "inspect output", ToolCallID: "call_1"},
		{ID: "a2", SessionID: "sess_1", Role: "assistant", Content: "done"},
	}

	got := messagesForDisplay(msgs)

	if len(got) != 4 {
		t.Fatalf("display message count = %d, want 4: %+v", len(got), got)
	}
	if got[1].Role != "assistant" || got[1].Content != "I will inspect it." {
		t.Fatalf("assistant text not preserved: %+v", got[1])
	}
	if got[2].Role != "tool" {
		t.Fatalf("combined message role = %q, want tool", got[2].Role)
	}
	if !strings.Contains(got[2].Content, `→ bash({"command":"excelcli inspect /tmp/a.db","timeout":120})`) {
		t.Fatalf("tool command missing from combined content: %q", got[2].Content)
	}
	if !strings.Contains(got[2].Content, "\n─────────\ninspect output") {
		t.Fatalf("tool result missing from combined content: %q", got[2].Content)
	}
	if got[2].ToolCallID != "call_1" {
		t.Fatalf("tool_call_id = %q, want call_1", got[2].ToolCallID)
	}
}

// TestToMessageDTOParsesImages 防回归：store.Message.Images 是 JSON 字符串，
// toMessageDTO 必须解析为 []string，否则前端 m.images.map 会崩溃白屏。
func TestToMessageDTOParsesImages(t *testing.T) {
	m := store.Message{
		Role: "user", Content: "看图",
		Images: `["data:image/png;base64,AAAA","data:image/png;base64,BBBB"]`,
	}
	dto := toMessageDTO(m)
	if len(dto.Images) != 2 {
		t.Fatalf("expected 2 images parsed, got %d", len(dto.Images))
	}
	if dto.Images[0] != "data:image/png;base64,AAAA" {
		t.Errorf("first image = %q", dto.Images[0])
	}
	// 空 / 无效 images 不应产生数组（omitempty），避免前端误判。
	for _, raw := range []string{"", "  ", "{not json}"} {
		empty := toMessageDTO(store.Message{Role: "user", Images: raw})
		if len(empty.Images) != 0 {
			t.Errorf("images=%q produced %d entries, want 0", raw, len(empty.Images))
		}
	}
}
