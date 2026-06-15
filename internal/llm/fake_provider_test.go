package llm

import (
	"context"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
)

func TestFakeProviderReturnsScriptedResponses(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{
		{Text: "你好", DeltaText: "你好"},
		{Text: "还有什么需要帮忙的吗？", DeltaText: "还有什么需要帮忙的吗？"},
	})

	var deltas1 string
	resp1, err := fp.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: "user", Content: "hi"}},
		OnDelta:  func(s string) { deltas1 += s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp1.Message.Content != "你好" {
		t.Errorf("got %q, want 你好", resp1.Message.Content)
	}
	if deltas1 != "你好" {
		t.Errorf("deltas got %q, want 你好", deltas1)
	}

	resp2, _ := fp.ChatStream(context.Background(), Request{OnDelta: func(string) {}})
	if resp2.Message.Content != "还有什么需要帮忙的吗？" {
		t.Errorf("second call got %q", resp2.Message.Content)
	}
}

func TestFakeProviderRecordsRequests(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{{Text: "ok"}})

	_, _ = fp.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: "user", Content: "测试记录"}},
		Tools:    []ToolDef{{Name: "tool_x", Description: "x", Schema: []byte(`{}`)}},
	})

	calls := fp.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(calls))
	}
	if calls[0].Messages[0].Content != "测试记录" {
		t.Errorf("unexpected recorded message: %+v", calls[0].Messages[0])
	}
	if len(calls[0].Tools) != 1 || calls[0].Tools[0].Name != "tool_x" {
		t.Errorf("tools not recorded: %+v", calls[0].Tools)
	}
}

func TestFakeProviderReturnsToolCalls(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{
		{
			Text: "我来查一下",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "get_system_info", Args: []byte(`{}`)},
			},
		},
	})

	resp, err := fp.ChatStream(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
}

func TestFakeProviderExhaustedScript(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{{Text: "only one"}})

	_, _ = fp.ChatStream(context.Background(), Request{})
	_, err := fp.ChatStream(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error when script exhausted")
	}
}

// TestFakeProviderDeltaTextFallback 钉住 DeltaText 为空时 fallback 到 Text 的契约。
// 下游（如 Agent Loop 测试）依赖此行为，删除会破坏 T8 的 TestV1LoopPureChat。
func TestFakeProviderDeltaTextFallback(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{{Text: "完整文本"}}) // 故意不设 DeltaText

	var got string
	resp, err := fp.ChatStream(context.Background(), Request{
		OnDelta: func(s string) { got += s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "完整文本" {
		t.Errorf("OnDelta should receive Text as fallback; got %q", got)
	}
	if resp.Message.Content != "完整文本" {
		t.Errorf("content got %q", resp.Message.Content)
	}
}
