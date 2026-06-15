// Package conversation 定义 Agent 对话的消息模型与历史管理。
// Message 结构覆盖 OpenAI Chat Completions 的所有角色，
// 包括 function calling 所需的 tool_calls 与 tool role 回填。
package conversation

import "encoding/json"

// Role 标识消息发送方角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool 工具执行结果，通过 ToolCallID 关联到对应的 ToolCall。
	RoleTool Role = "tool"
)

// ToolCall 是 assistant 消息里发起的工具调用请求。
//
// 注意：这是内部领域模型，不直接对应 OpenAI 线上格式。OpenAI 把工具调用
// 包装为 {"type":"function","function":{"name":..,"arguments":"<json string>"}}，
// 由 llm.Provider 层（T4/T5）负责内部模型 ↔ 线上格式的转换，不要在此结构上
// 直接 json.Marshal 后发给 API。
type ToolCall struct {
	// ID 模型生成，回填 tool 消息时必须对应（OpenAI 要求配对）。
	ID string `json:"id"`
	// Name 被调用的工具名。
	Name string `json:"name"`
	// Args 工具参数（JSON 字符串，对应 OpenAI 的 function.arguments）。
	Args json.RawMessage `json:"args"`
}

// Message 是对话中的一条消息，兼容 OpenAI Chat Completions 格式。
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // 仅 RoleAssistant 使用
	ToolCallID string     `json:"tool_call_id,omitempty"` // 仅 RoleTool 使用
	Name       string     `json:"name,omitempty"`         // 仅 RoleTool 使用
}
