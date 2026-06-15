package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/agentforge/agentforge/internal/conversation"
)

// OpenAIProvider 调用 OpenAI 兼容 Chat Completions API。
// 流式优先（SSE），OnDelta 为 nil 时降级为非流式。
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAIProvider(baseURL, apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{},
	}
}

// 线上格式（OpenAI wire format）——与内部 conversation.Message 字段不同，
// 在此 Provider 内做内部模型 ↔ 线上格式的映射。
type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"` // 仅 role=tool 使用
	Name       string         `json:"name,omitempty"`         // 仅 role=tool 使用
}
type wireToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function wireFunction `json:"function"`
}
type wireFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // OpenAI 用 string 编码的 JSON
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []wireToolDef `json:"tools,omitempty"`
	Stream   bool          `json:"stream,omitempty"`
}
type wireToolDef struct {
	Type     string  `json:"type"` // 固定 "function"
	Function ToolDef `json:"function"`
}

// toWire 把内部 conversation.Message 转为线上格式。
func toWire(msg conversation.Message) wireMessage {
	wm := wireMessage{
		Role:       string(msg.Role),
		Content:    msg.Content,
		ToolCallID: msg.ToolCallID,
		Name:       msg.Name,
	}
	for _, tc := range msg.ToolCalls {
		wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: wireFunction{
				Name:      tc.Name,
				Arguments: string(tc.Args),
			},
		})
	}
	return wm
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req Request) (*Response, error) {
	streaming := req.OnDelta != nil
	body := chatRequest{
		Model:  p.model,
		Stream: streaming,
		Tools:  toWireToolDefs(req.Tools),
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, toWire(m))
	}
	raw, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		strings.TrimRight(p.baseURL, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	if streaming {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, p.mapAPIError(resp)
	}

	if streaming {
		return p.readStream(resp.Body, req.OnDelta)
	}
	return p.readFull(resp.Body)
}

func (p *OpenAIProvider) mapAPIError(resp *http.Response) error {
	msg := fmt.Sprintf("api status %d", resp.StatusCode)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		msg = "api_key 无效（401）"
	case http.StatusTooManyRequests:
		msg = "被限流（429），请稍后重试"
	}
	return fmt.Errorf("%s", msg)
}

// readStream 逐帧解析 SSE，累积 content 与 tool_calls。
func (p *OpenAIProvider) readStream(r io.Reader, onDelta func(string)) (*Response, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var contentBuilder strings.Builder
	acc := newToolCallAccumulator()

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id,omitempty"`
						Function struct {
							Name      string `json:"name,omitempty"`
							Arguments string `json:"arguments,omitempty"`
						} `json:"function"`
					} `json:"tool_calls,omitempty"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason,omitempty"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			continue // 跳过无法解析的帧
		}
		for _, ch := range frame.Choices {
			if ch.Delta.Content != "" {
				contentBuilder.WriteString(ch.Delta.Content)
				if onDelta != nil {
					onDelta(ch.Delta.Content)
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				acc.add(deltaChunk{
					Index:         tc.Index,
					ID:            tc.ID,
					FunctionName:  tc.Function.Name,
					ArgumentsFrag: tc.Function.Arguments,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	msg := conversation.Message{
		Role:    conversation.RoleAssistant,
		Content: contentBuilder.String(),
	}
	for _, ec := range acc.result() {
		msg.ToolCalls = append(msg.ToolCalls, conversation.ToolCall{
			ID:   ec.ID,
			Name: ec.Name,
			Args: json.RawMessage(ec.Args),
		})
	}
	return &Response{Message: msg}, nil
}

// readFull 非流式：一次性读完整 JSON。
func (p *OpenAIProvider) readFull(r io.Reader) (*Response, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var fr struct {
		Choices []struct {
			Message wireMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &fr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(fr.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}
	wm := fr.Choices[0].Message
	msg := conversation.Message{Role: conversation.Role(wm.Role), Content: wm.Content}
	for _, tc := range wm.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, conversation.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments),
		})
	}
	return &Response{Message: msg}, nil
}

func toWireToolDefs(defs []ToolDef) []wireToolDef {
	if len(defs) == 0 {
		return nil
	}
	out := make([]wireToolDef, len(defs))
	for i, d := range defs {
		out[i] = wireToolDef{Type: "function", Function: d}
	}
	return out
}
