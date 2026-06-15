package conversation

import "testing"

func TestManagerAppendAndMessages(t *testing.T) {
	m := NewManager()
	m.AppendSystem("你是助手")
	m.AppendUser("你好")

	msgs := m.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != RoleSystem || msgs[0].Content != "你是助手" {
		t.Errorf("unexpected system message: %+v", msgs[0])
	}
	if msgs[1].Role != RoleUser || msgs[1].Content != "你好" {
		t.Errorf("unexpected user message: %+v", msgs[1])
	}
}

func TestManagerAppendAssistantWithToolCalls(t *testing.T) {
	m := NewManager()
	m.AppendUser("查一下系统信息")

	m.AppendAssistant(Message{
		Content: "我来查一下",
		ToolCalls: []ToolCall{
			{ID: "call_1", Name: "get_system_info", Args: []byte(`{}`)},
		},
	})

	msgs := m.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	assistant := msgs[1]
	if assistant.Role != RoleAssistant {
		t.Errorf("expected assistant role, got %s", assistant.Role)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_1" {
		t.Errorf("unexpected tool calls: %+v", assistant.ToolCalls)
	}
}

func TestManagerAppendToolResult(t *testing.T) {
	m := NewManager()
	m.AppendAssistant(Message{
		ToolCalls: []ToolCall{{ID: "call_1", Name: "get_system_info"}},
	})
	m.AppendToolResult("call_1", "get_system_info", "OS: Windows", false)

	msgs := m.Messages()
	toolMsg := msgs[1]
	if toolMsg.Role != RoleTool {
		t.Errorf("expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id call_1, got %s", toolMsg.ToolCallID)
	}
}

func TestManagerForRequestReturnsAllBeforeCompress(t *testing.T) {
	m := NewManager()
	m.AppendUser("a")
	m.AppendUser("b")
	if got := m.ForRequest(); len(got) != 2 {
		t.Fatalf("ForRequest before compress should return all; got %d", len(got))
	}
}
