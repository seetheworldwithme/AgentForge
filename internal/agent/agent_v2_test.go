package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	"github.com/agentforge/agentforge/internal/tool"
)

type echoTool struct{}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "回声测试工具" }
func (e *echoTool) Schema() []byte      { return []byte(`{"type":"object","properties":{}}`) }

func (e *echoTool) Execute(ctx context.Context, args []byte) (<-chan tool.Event, error) {
	ch := make(chan tool.Event)
	go func() {
		defer close(ch)
		ch <- tool.Event{Kind: tool.EventDelta, Text: "running..."}
		ch <- tool.Event{
			Kind:   tool.EventResult,
			Result: &tool.Result{Content: "echo result"},
		}
	}()
	return ch, nil
}

func newEchoRegistry() *tool.Registry {
	r := tool.NewRegistry()
	r.Register(&echoTool{})
	return r
}

func TestV2LoopExecutesToolCall(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "我来查一下",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "echo", Args: []byte(`{}`)},
			},
		},
		{Text: "工具返回了结果"},
	})

	agent := NewAgent(fp, newEchoRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: true, MaxIterations: 5})

	var deltas string
	var toolEvents []LoopEvent
	sink := func(ev LoopEvent) {
		if ev.Kind == LoopDelta {
			deltas += ev.Text
		}
		if ev.Kind == LoopToolCallStart || ev.Kind == LoopToolCallEnd {
			toolEvents = append(toolEvents, ev)
		}
	}

	err := agent.Run(context.Background(), "打个招呼", sink)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deltas != "我来查一下工具返回了结果" {
		t.Errorf("deltas collected %q", deltas)
	}
	if len(toolEvents) < 2 {
		t.Fatalf("expected at least 2 tool events (start+end), got %d", len(toolEvents))
	}
	if len(fp.Calls()) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(fp.Calls()))
	}
}

func TestV2LoopConfirmRejected(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "想调工具",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "echo", Args: []byte(`{}`)},
			},
		},
		{Text: "好的，不调了"},
	})

	rejected := false
	agent := NewAgent(fp, newEchoRegistry(), conversation.NewManager(),
		Policy{
			AllowToolCalls: true,
			Confirm: func(call conversation.ToolCall) (bool, error) {
				rejected = true
				return false, nil
			},
			MaxIterations: 5,
		})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rejected {
		t.Fatal("expected Confirm to be called")
	}
}

func TestV2LoopUnknownTool(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "调个不存在的工具",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "nonexistent_tool", Args: []byte(`{}`)},
			},
		},
		{Text: "好吧，那个工具不存在"},
	})

	mgr := conversation.NewManager()
	agent := NewAgent(fp, tool.NewRegistry(), mgr,
		Policy{AllowToolCalls: true, MaxIterations: 5})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err != nil {
		t.Fatalf("unknown tool should not be fatal: %v", err)
	}

	// 确认错误被回填给历史（LLM 能据此调整策略），而非静默吞掉。
	var foundToolResult bool
	for _, msg := range mgr.Messages() {
		if msg.Role == conversation.RoleTool && msg.ToolCallID == "call_1" {
			foundToolResult = true
			if !strings.Contains(msg.Content, "未知工具: nonexistent_tool") {
				t.Errorf("expected error content mentioning unknown tool; got %q", msg.Content)
			}
		}
	}
	if !foundToolResult {
		t.Fatal("expected a tool result recorded for the unknown tool call")
	}
}

// TestV2LoopConfirmError 验证 Confirm 返回 error 时是致命的（中止整个 Run）。
func TestV2LoopConfirmError(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "想调工具",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "echo", Args: []byte(`{}`)},
			},
		},
	})

	agent := NewAgent(fp, newEchoRegistry(), conversation.NewManager(),
		Policy{
			AllowToolCalls: true,
			Confirm: func(call conversation.ToolCall) (bool, error) {
				return false, errors.New("confirm backend down")
			},
			MaxIterations: 5,
		})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err == nil {
		t.Fatal("expected Run to abort when Confirm returns an error")
	}
	if !strings.Contains(err.Error(), "confirm backend down") {
		t.Errorf("expected wrapped error to mention cause; got %q", err.Error())
	}
}
