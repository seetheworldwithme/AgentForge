package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	registrypkg "github.com/agentforge/agentforge/internal/registry"
)

func TestIntegrationEndToEnd(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "我来查一下系统信息",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "get_system_info", Args: []byte(`{}`)},
			},
		},
		{Text: "已获取系统信息，分析完成"},
	})

	registry := registrypkg.Default()
	agent := NewAgent(fp, registry, conversation.NewManager(),
		Policy{AllowToolCalls: true, MaxIterations: 5})

	var deltas []string
	var toolCallCount int
	sink := func(ev LoopEvent) {
		switch ev.Kind {
		case LoopDelta:
			deltas = append(deltas, ev.Text)
		case LoopToolCallStart:
			toolCallCount++
		}
	}

	err := agent.Run(context.Background(), "帮我看看系统信息", sink)
	if err != nil {
		t.Fatalf("end-to-end failed: %v", err)
	}

	if toolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", toolCallCount)
	}
	if len(fp.Calls()) != 2 {
		t.Errorf("expected 2 provider calls, got %d", len(fp.Calls()))
	}
	if len(fp.Calls()[0].Tools) == 0 {
		t.Error("first call should pass tools to provider")
	}
	// 直接验证"结果回填"：第二次 provider 调用的消息里必须包含工具执行结果（RoleTool）。
	secondCallMsgs := fp.Calls()[1].Messages
	var foundToolResult bool
	for _, msg := range secondCallMsgs {
		if msg.Role == conversation.RoleTool && msg.ToolCallID == "call_1" {
			foundToolResult = true
			if msg.Content == "" {
				t.Error("tool result fed back to provider should have non-empty content")
			}
		}
	}
	if !foundToolResult {
		t.Fatal("expected the tool result to be fed back into the 2nd provider call")
	}
	allDeltas := strings.Join(deltas, "")
	if !strings.Contains(allDeltas, "我来查一下") {
		t.Errorf("missing first delta; got %q", allDeltas)
	}
	if !strings.Contains(allDeltas, "已获取系统信息") {
		t.Errorf("missing final delta; got %q", allDeltas)
	}
}
