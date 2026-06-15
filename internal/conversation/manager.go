package conversation

// Manager 负责消息存储与（T6 加入的）context 压缩。
type Manager struct {
	messages []Message
}

// NewManager 创建空 Manager。
func NewManager() *Manager {
	return &Manager{}
}

// AppendSystem 追加 system 消息。
func (m *Manager) AppendSystem(content string) {
	m.messages = append(m.messages, Message{Role: RoleSystem, Content: content})
}

// AppendUser 追加用户消息。
func (m *Manager) AppendUser(content string) {
	m.messages = append(m.messages, Message{Role: RoleUser, Content: content})
}

// AppendAssistant 追加 assistant 消息（可含 tool_calls）。
func (m *Manager) AppendAssistant(msg Message) {
	msg.Role = RoleAssistant
	m.messages = append(m.messages, msg)
}

// AppendToolResult 追加工具执行结果（role=tool）。
func (m *Manager) AppendToolResult(toolCallID, toolName, content string, isError bool) {
	m.messages = append(m.messages, Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		Name:       toolName,
	})
}

// Messages 返回当前所有消息（未经压缩的原始存储）。
func (m *Manager) Messages() []Message {
	return m.messages
}

// ForRequest 返回发给 LLM 的消息序列。
// T6 会在此加入压缩逻辑；当前直接返回全量。
func (m *Manager) ForRequest() []Message {
	return m.messages
}
