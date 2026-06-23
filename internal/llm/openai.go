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
)

type OpenAIClient struct {
	cfg  Config
	http *http.Client
}

func NewOpenAIClient(cfg Config) *OpenAIClient {
	return &OpenAIClient{
		cfg:  cfg,
		http: &http.Client{Timeout: 0}, // streaming; per-request context handles timeout
	}
}

type chatReq struct {
	Model    string    `json:"model"`
	Messages []rawMsg  `json:"messages"`
	Tools    []rawTool `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

type rawMsg struct {
	Role       string        `json:"role"`
	Content    any           `json:"content,omitempty"` // string（纯文本）或 []contentPart（多模态）
	ToolCalls  []rawToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

// contentPart 是 OpenAI 多模态 content 的一个片段（文本或图片）。
type contentPart struct {
	Type     string        `json:"type"` // "text" 或 "image_url"
	Text     string        `json:"text,omitempty"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

type imageURLPart struct {
	URL string `json:"url"`
}

type rawTool struct {
	Type     string      `json:"type"` // always "function"
	Function rawToolSpec `json:"function"`
}

type rawToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // arbitrary JSON object
}

type rawToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"` // "function"
	Function rawToolCallFunc `json:"function"`
}

type rawToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func toRawMessages(msgs []Message) []rawMsg {
	out := make([]rawMsg, 0, len(msgs))
	for _, m := range msgs {
		rm := rawMsg{Role: string(m.Role), ToolCallID: m.ToolCallID}
		if len(m.Images) > 0 {
			// 多模态：content 是片段数组（文本 + 图片）。
			parts := make([]contentPart, 0, len(m.Images)+1)
			if m.Content != "" {
				parts = append(parts, contentPart{Type: "text", Text: m.Content})
			}
			for _, img := range m.Images {
				parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURLPart{URL: img.DataURL}})
			}
			rm.Content = parts
		} else {
			rm.Content = m.Content
		}
		for _, tc := range m.ToolCalls {
			rm.ToolCalls = append(rm.ToolCalls, rawToolCall{
				ID: tc.ID, Type: "function",
				Function: rawToolCallFunc{Name: tc.Name, Arguments: tc.Args},
			})
		}
		out = append(out, rm)
	}
	return out
}

func toRawTools(tools []ToolSpec) []rawTool {
	out := make([]rawTool, 0, len(tools))
	for _, ts := range tools {
		var params any
		_ = json.Unmarshal([]byte(ts.Parameters), &params)
		out = append(out, rawTool{Type: "function", Function: rawToolSpec{
			Name: ts.Name, Description: ts.Description, Parameters: params,
		}})
	}
	return out
}

func (c *OpenAIClient) ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error) {
	body, _ := json.Marshal(chatReq{
		Model: c.cfg.Model, Messages: toRawMessages(msgs),
		Tools: toRawTools(tools), Stream: true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: string(b)}
	}

	ch := make(chan Chunk, 8)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// Tool-call arguments arrive fragmented across many delta chunks
		// (e.g. '{"', '"command":"pwd"', '}'), each carrying only a piece of
		// the function name/id or the arguments string. Emitting a Chunk per
		// delta yields partial JSON the tool can't parse. Accumulate per
		// tool-call index and emit one complete ToolCall when the turn ends.
		type toolAccum struct {
			id, name string
			args     strings.Builder
		}
		pending := map[int]*toolAccum{}
		order := []int{}
		flushed := false
		flush := func() {
			if flushed {
				return
			}
			flushed = true
			for _, i := range order {
				a := pending[i]
				ch <- Chunk{ToolCall: &ToolCall{ID: a.id, Name: a.name, Args: a.args.String()}}
			}
		}

		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				flush()
				ch <- Chunk{Done: true}
				return
			}
			var ev struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			for _, choice := range ev.Choices {
				if choice.Delta.Content != "" {
					ch <- Chunk{Text: choice.Delta.Content}
				}
				for _, tc := range choice.Delta.ToolCalls {
					a, ok := pending[tc.Index]
					if !ok {
						a = &toolAccum{}
						pending[tc.Index] = a
						order = append(order, tc.Index)
					}
					if tc.ID != "" {
						a.id = tc.ID
					}
					if tc.Function.Name != "" {
						a.name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						a.args.WriteString(tc.Function.Arguments)
					}
				}
			}
			if ev.Usage != nil {
				ch <- Chunk{Usage: &Usage{
					InputTokens:  ev.Usage.PromptTokens,
					OutputTokens: ev.Usage.CompletionTokens,
				}}
			}
		}
		// Stream ended without [DONE]; flush any accumulated tool calls.
		flush()
	}()
	return ch, nil
}

func (c *OpenAIClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"model": c.cfg.Model, "input": inputs,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: "embed: " + string(b)}
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}

// Chat 是非流式一次性调用，返回完整文本。用于 VLM 图片描述等场景。
func (c *OpenAIClient) Chat(ctx context.Context, msgs []Message) (string, error) {
	body, _ := json.Marshal(chatReq{
		Model: c.cfg.Model, Messages: toRawMessages(msgs), Stream: false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return "", &HTTPError{StatusCode: resp.StatusCode, Body: string(b)}
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}
