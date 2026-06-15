package conversation

// Summarizer 负责把一段旧消息压缩成摘要文本。
type Summarizer interface {
	Summarize(msgs []Message) (string, error)
}

// option 选项模式配置 Manager。
type option func(*Manager)

func WithSummarizer(s Summarizer) option {
	return func(m *Manager) { m.summarizer = s }
}

func WithMaxTokens(n int) option {
	return func(m *Manager) { m.maxTokens = n }
}

// estimateTokens 粗略估算文本的 token 数。V1 用字符数/4 近似。
//
// TODO(V2): 使用真实 tokenizer。当前按字节数/4 估算，对中文严重偏低
// （UTF-8 中文字符 3 字节，而 tokenizer 约 1-2 token/字），导致中文对话
// 压缩触发偏晚（阈值实际高出 2-3 倍）。方向安全（多保留而非切断配对），
// 但削弱了阈值的实际效果。
func estimateTokens(content string) int {
	return len(content) / 4
}

func estimateMessagesTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += estimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			total += estimateTokens(string(tc.Args))
		}
	}
	return total
}
