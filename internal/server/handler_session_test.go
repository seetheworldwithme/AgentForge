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
