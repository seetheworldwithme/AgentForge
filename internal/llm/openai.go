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
	Content    string        `json:"content,omitempty"`
	ToolCalls  []rawToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
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
		rm := rawMsg{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
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
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan Chunk, 8)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- Chunk{Done: true}
				return
			}
			var ev struct {
				Choices []struct {
					Delta struct {
						Content   string        `json:"content"`
						ToolCalls []rawToolCall `json:"tool_calls"`
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
				if len(choice.Delta.ToolCalls) > 0 {
					tc := choice.Delta.ToolCalls[0]
					ch <- Chunk{ToolCall: &ToolCall{
						ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments,
					}}
				}
			}
			if ev.Usage != nil {
				ch <- Chunk{Usage: &Usage{
					InputTokens:  ev.Usage.PromptTokens,
					OutputTokens: ev.Usage.CompletionTokens,
				}}
			}
		}
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
		return nil, fmt.Errorf("embed http %d: %s", resp.StatusCode, string(b))
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
