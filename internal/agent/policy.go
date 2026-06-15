// Package agent 实现 Agent 对话循环（Loop）与工具调度。
package agent

import "github.com/agentforge/agentforge/internal/conversation"

// Policy 控制 Agent 在一轮思考中如何处理工具调用。
type Policy struct {
	// AllowToolCalls 是否允许 LLM 返回工具调用。
	AllowToolCalls bool
	// Confirm 工具执行前的确认回调。nil 表示无需确认。
	Confirm func(call conversation.ToolCall) (approved bool, err error)
	// MaxIterations 防止无限循环的硬上限。0 表示默认 10。
	MaxIterations int
}

func (p Policy) effectiveMaxIterations() int {
	if p.MaxIterations <= 0 {
		return 10
	}
	return p.MaxIterations
}

// LoopEventKind 标识 Loop 推送的事件类型。
type LoopEventKind int

const (
	LoopDelta LoopEventKind = iota
	LoopProgress
	LoopToolCallStart
	LoopToolCallEnd
	LoopInfo
)

// LoopEvent 是 Agent Loop 对外推送的事件。
type LoopEvent struct {
	Kind     LoopEventKind
	Text     string
	ToolCall *conversation.ToolCall
}

// EventSink 由调用方（CLI/GUI）实现。
type EventSink func(LoopEvent)

func DeltaEvent(text string) LoopEvent {
	return LoopEvent{Kind: LoopDelta, Text: text}
}

func ProgressEvent(text string) LoopEvent {
	return LoopEvent{Kind: LoopProgress, Text: text}
}
