package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	Images     []ImageRef `json:"-"` // vision：由 openai.go 转成多模态 content，不直接序列化
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // for role=tool
}

// ImageRef 是发给支持 vision 模型的图片。DataURL 形如
// "data:image/png;base64,iVBOR..."（已内联 base64）。
type ImageRef struct {
	DataURL string
}

type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"` // raw JSON string
}

type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// JSON schema for arguments, serialized as a raw JSON string
	Parameters string `json:"parameters"`
}

// Chunk is one piece of a streamed response.
type Chunk struct {
	Text     string    // non-empty when assistant emits text
	ToolCall *ToolCall // non-nil when a tool call completes
	Usage    *Usage    // non-nil on final chunk
	Done     bool      // true on terminal chunk
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Config identifies how to reach a provider.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

// LLMClient streams a chat completion. The returned channel closes when done.
type LLMClient interface {
	ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error)
	// Chat 是非流式一次性调用，返回完整文本。用于 VLM 图片描述等不需要流式的场景。
	Chat(ctx context.Context, msgs []Message) (string, error)
	// Embed returns vectors for the given inputs (used by RAG).
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}
