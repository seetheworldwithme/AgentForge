// Package tool 定义 Agent 工具的统一抽象。
// 所有能力（命令执行、文件操作、未来扩展）都实现 Tool 接口，
// 通过 Registry 注册，供 Agent Loop 调度。
package tool

import "context"

// EventKind 标识工具执行过程中推送的事件类型。Loop 按类型分发处理。
type EventKind int

const (
	// EventDelta 中间输出（命令的 stdout 行、文件读取片段）→ 实时推给用户。
	EventDelta EventKind = iota
	// EventProgress 进度信息（可选，GUI 可用作进度条）。
	EventProgress
	// EventResult 最终结构化结果，必须且只能出现一次，作为通道关闭前的最后一个事件。
	EventResult
	// EventError 工具执行出错（非致命，可继续对话）。
	EventError
)

// Event 是工具通过通道推送的单个事件。
type Event struct {
	Kind EventKind
	// Text 用于 Delta / Progress / Error 的文本。
	Text string
	// Result 仅 EventResult 使用，内容回填给 LLM 作为 tool 消息。
	Result *Result
}

// Result 是回填给 LLM 的工具结果，对应 OpenAI "tool" role message。
type Result struct {
	// Content 回填给模型的文本内容。
	Content string
	// IsError 标记为错误（模型可据此调整策略），非致命错误用此而非返回 error。
	IsError bool
}

// Tool 是所有工具的统一接口。
type Tool interface {
	// Name 工具唯一标识，用于 function calling 的 "name" 字段。
	Name() string
	// Description 给 LLM 看的描述（影响模型是否选择调用此工具）。
	Description() string
	// Schema 参数的 JSON Schema，遵循 OpenAI function calling 规范。
	Schema() []byte
	// Execute 流式执行。返回事件通道，调用方读取直到关闭。
	// 工具侧负责：在 goroutine 中执行并通过 ctx 响应取消；通道必须最终关闭。
	Execute(ctx context.Context, args []byte) (<-chan Event, error)
}
