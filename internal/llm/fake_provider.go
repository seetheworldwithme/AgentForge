package llm

import (
	"context"
	"errors"

	"github.com/agentforge/agentforge/internal/conversation"
)

// FakeResponse 是脚本化的单次响应。
type FakeResponse struct {
	Text string
	// DeltaText 流式增量；为空时 OnDelta 一次性收到完整 Text。
	// 这是 FakeProvider 的隐式契约：下游（如 Agent Loop 测试）可不设
	// DeltaText 而依赖 Text 被 stream 出去，故不可当作"多余"删除。
	DeltaText string
	ToolCalls []conversation.ToolCall
}

// FakeProvider 是测试用 Provider 桩。
type FakeProvider struct {
	responses []FakeResponse
	calls     []Request
}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{}
}

func (f *FakeProvider) Script(rs []FakeResponse) {
	f.responses = rs
}

func (f *FakeProvider) ChatStream(ctx context.Context, req Request) (*Response, error) {
	f.calls = append(f.calls, req)

	if len(f.calls) > len(f.responses) {
		return nil, errors.New("fake provider: script exhausted")
	}

	resp := f.responses[len(f.calls)-1]

	if req.OnDelta != nil {
		delta := resp.DeltaText
		if delta == "" {
			delta = resp.Text
		}
		req.OnDelta(delta)
	}

	return &Response{
		Message: conversation.Message{
			Role:      conversation.RoleAssistant,
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		},
	}, nil
}

func (f *FakeProvider) Calls() []Request {
	return f.calls
}
