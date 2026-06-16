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
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // for role=tool
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
	// Embed returns vectors for the given inputs (used by RAG).
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}
