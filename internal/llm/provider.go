// Package llm 定义大模型 Provider 的抽象接口。
package llm

import (
	"context"

	"github.com/agentforge/agentforge/internal/conversation"
)

// Provider 是大模型后端的统一接口。
type Provider interface {
	// ChatStream 发起一次流式对话请求。
	// req.Tools 为空时表示纯对话模式（不启用 function calling）。
	// req.OnDelta 在每个文本 token 到达时被调用。
	// 返回的 Response.Message 包含完整 assistant 消息（含累积后的 tool_calls）。
	ChatStream(ctx context.Context, req Request) (*Response, error)
}

// ToolDef 是传给 API 的工具定义（OpenAI tools 数组的一项）。
type ToolDef struct {
	Name        string
	Description string
	Schema      []byte // JSON Schema
}

// Request 是一次对话请求的参数。
type Request struct {
	// Messages 发给模型的消息序列。
	Messages []conversation.Message
	// Tools 暴露给模型的工具定义。nil 或空 = 不启用 function calling。
	Tools []ToolDef
	// OnDelta 流式文本 token 回调。
	OnDelta func(text string)
}

// Response 是一次对话请求的结果。
type Response struct {
	// Message 完整的 assistant 消息（流结束后组装）。
	Message conversation.Message
}
