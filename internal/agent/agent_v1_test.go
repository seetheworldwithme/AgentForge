package agent

import (
	"context"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	"github.com/agentforge/agentforge/internal/tool"
)

func TestV1LoopPureChat(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{Text: "你好，有什么可以帮你？"},
	})

	agent := NewAgent(fp, tool.NewRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: false})

	var collected string
	sink := func(ev LoopEvent) {
		if ev.Kind == LoopDelta {
			collected += ev.Text
		}
	}

	err := agent.Run(context.Background(), "hi", sink)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if collected != "你好，有什么可以帮你？" {
		t.Errorf("delta collected %q", collected)
	}

	calls := fp.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(calls))
	}
	if len(calls[0].Tools) != 0 {
		t.Errorf("V1 should not pass tools; got %d tools", len(calls[0].Tools))
	}
}

func TestV1LoopStoresHistory(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{{Text: "reply"}})

	mgr := conversation.NewManager()
	agent := NewAgent(fp, tool.NewRegistry(), mgr, Policy{AllowToolCalls: false})

	_ = agent.Run(context.Background(), "user msg", func(LoopEvent) {})

	msgs := mgr.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in history, got %d", len(msgs))
	}
	if msgs[0].Role != conversation.RoleUser || msgs[0].Content != "user msg" {
		t.Errorf("unexpected user msg: %+v", msgs[0])
	}
}

func TestV1LoopPropagatesProviderError(t *testing.T) {
	fp := llm.NewFakeProvider()
	agent := NewAgent(fp, tool.NewRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: false})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err == nil {
		t.Fatal("expected error from exhausted provider")
	}
}
