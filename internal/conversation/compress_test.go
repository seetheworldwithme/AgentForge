package conversation

import "testing"

type stubSummarizer struct {
	calls   int
	returns string
}

func (s *stubSummarizer) Summarize(msgs []Message) (string, error) {
	s.calls++
	return s.returns, nil
}

func TestCompressNotTriggeredUnderThreshold(t *testing.T) {
	stub := &stubSummarizer{returns: "摘要"}
	m := NewManager(WithSummarizer(stub), WithMaxTokens(1000))
	m.AppendUser("短消息")

	_ = m.ForRequest()
	if stub.calls != 0 {
		t.Fatalf("summarizer should not be called under threshold; calls=%d", stub.calls)
	}
}

func TestCompressTriggeredOverThreshold(t *testing.T) {
	stub := &stubSummarizer{returns: "[摘要]"}
	m := NewManager(WithSummarizer(stub), WithMaxTokens(5))
	m.AppendUser("第一轮长消息用来触发压缩")
	m.AppendAssistant(Message{Content: "第一轮回复也很长"})
	m.AppendUser("第二轮保留近期")

	msgs := m.ForRequest()
	if stub.calls == 0 {
		t.Fatal("summarizer should be called over threshold")
	}
	if len(msgs) == 0 {
		t.Fatal("expected non-empty messages after compress")
	}
	if msgs[0].Role != RoleSystem {
		t.Errorf("expected first message to be system summary, got %s", msgs[0].Role)
	}
	foundRecent := false
	for _, msg := range msgs {
		if msg.Content == "第二轮保留近期" {
			foundRecent = true
		}
	}
	if !foundRecent {
		t.Error("expected recent user message preserved after compress")
	}
}

func TestCompressPreservesToolCallPairing(t *testing.T) {
	stub := &stubSummarizer{returns: "[摘要]"}
	m := NewManager(WithSummarizer(stub), WithMaxTokens(5))

	m.AppendUser("第一轮")
	m.AppendAssistant(Message{
		ToolCalls: []ToolCall{{ID: "call_1", Name: "x"}},
	})
	m.AppendUser("第二轮")
	m.AppendToolResult("call_1", "x", "result", false)

	msgs := m.ForRequest()

	seenCallIDs := map[string]bool{}
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			seenCallIDs[tc.ID] = true
		}
		if msg.Role == RoleTool {
			if !seenCallIDs[msg.ToolCallID] {
				t.Fatalf("tool result %q has no preceding tool_call in compressed messages", msg.ToolCallID)
			}
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world!", 3},
	}
	for _, tt := range tests {
		got := estimateTokens(tt.content)
		if got != tt.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.content, got, tt.want)
		}
	}
}
