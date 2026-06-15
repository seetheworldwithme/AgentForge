package conversation

// Manager 负责消息存储与 context 压缩。
type Manager struct {
	messages   []Message
	summarizer Summarizer // 可选；nil 时压缩降级为丢弃
	maxTokens  int        // 触发压缩的阈值；0 表示不压缩
}

// NewManager 创建 Manager，可通过 option 配置压缩。
func NewManager(opts ...option) *Manager {
	m := &Manager{}
	for _, opt := range opts {
		opt(m)
	}
	return m
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

// ForRequest 返回发给 LLM 的消息序列。若配置 maxTokens 且超阈值，触发压缩。
func (m *Manager) ForRequest() []Message {
	if m.maxTokens == 0 {
		return m.messages
	}
	if estimateMessagesTokens(m.messages) <= m.maxTokens {
		return m.messages
	}
	return m.compress()
}

// compress 执行压缩：按安全边界切分旧消息，生成摘要，保留近期消息。
// 安全边界：被压缩的旧块不能把 tool_call 和它的 tool_result 拆到不同块。
func (m *Manager) compress() []Message {
	keepFrom := findSafeKeepFrom(m.messages)
	if keepFrom == 0 {
		return m.messages
	}

	oldMessages := m.messages[:keepFrom]
	recentMessages := m.messages[keepFrom:]

	var summary string
	if m.summarizer != nil {
		s, err := m.summarizer.Summarize(oldMessages)
		if err == nil {
			summary = s
		}
	}

	result := make([]Message, 0, len(recentMessages)+1)
	if summary != "" {
		result = append(result, Message{
			Role:    RoleSystem,
			Content: "[对话摘要] " + summary,
		})
	}
	result = append(result, recentMessages...)
	return result
}

func findSafeKeepFrom(msgs []Message) int {
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return 0
	}
	for keepFrom := lastUser; keepFrom >= 0; keepFrom-- {
		if allToolResultsPaired(msgs[keepFrom:]) {
			return keepFrom
		}
	}
	return 0
}

func allToolResultsPaired(msgs []Message) bool {
	calls := map[string]bool{}
	for _, msg := range msgs {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				calls[tc.ID] = true
			}
		}
	}
	for _, msg := range msgs {
		if msg.Role == RoleTool && !calls[msg.ToolCallID] {
			return false
		}
	}
	return true
}
