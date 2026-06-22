package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	TransportStdio = "stdio"
	TransportSSE   = "sse"
)

type ServerConfig struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Enabled   bool              `json:"enabled"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type StdioClient struct {
	cfg ServerConfig
}

type Client interface {
	ListTools() ([]Tool, error)
	CallTool(name string, args map[string]any) (string, error)
}

func NewClient(cfg ServerConfig) Client {
	cfg = normalizeServer(cfg)
	if cfg.Transport == TransportSSE {
		return NewHTTPClient(cfg)
	}
	return NewStdioClient(cfg)
}

func NewStdioClient(cfg ServerConfig) *StdioClient {
	return &StdioClient{cfg: cfg}
}

func (c *StdioClient) ListTools() ([]Tool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := c.start(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()

	if err := s.initialize(); err != nil {
		return nil, err
	}
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := s.request("tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

type HTTPClient struct {
	cfg    ServerConfig
	http   *http.Client
	nextID int
}

func NewHTTPClient(cfg ServerConfig) *HTTPClient {
	return &HTTPClient{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}, nextID: 1}
}

func (c *HTTPClient) ListTools() ([]Tool, error) {
	if err := c.initialize(); err != nil {
		return nil, err
	}
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.request("tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *HTTPClient) CallTool(name string, args map[string]any) (string, error) {
	if err := c.initialize(); err != nil {
		return "", err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.request("tools/call", map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return "", err
	}
	var parts []string
	for _, part := range result.Content {
		if part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n"), nil
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

func (c *HTTPClient) initialize() error {
	var result map[string]any
	return c.request("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "AgentForge", "version": "0.1.0"},
	}, &result)
}

func (c *HTTPClient) request(method string, params any, result any) error {
	if strings.TrimSpace(c.cfg.URL) == "" {
		return fmt.Errorf("mcp server %s url is required", c.cfg.Name)
	}
	id := c.nextID
	c.nextID++
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mcp http %d: %s", resp.StatusCode, string(b))
	}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return decodeSSEResponse(resp.Body, id, result)
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}
	return decodeRPCResponse(resp.Body, id, result)
}

func decodeSSEResponse(r io.Reader, id int, result any) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var data strings.Builder
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		payload := data.String()
		data.Reset()
		return decodeRPCBytes([]byte(payload), id, result)
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

func decodeRPCResponse(r io.Reader, id int, result any) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return decodeRPCBytes(b, id, result)
}

func decodeRPCBytes(b []byte, id int, result any) error {
	var resp struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	if resp.ID != id {
		return nil
	}
	if resp.Error != nil {
		return fmt.Errorf("mcp: %s", resp.Error.Message)
	}
	if result != nil {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

func (c *StdioClient) CallTool(name string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := c.start(ctx)
	if err != nil {
		return "", err
	}
	defer s.close()

	if err := s.initialize(); err != nil {
		return "", err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := s.request("tools/call", map[string]any{"name": name, "arguments": args}, &result); err != nil {
		return "", err
	}
	var parts []string
	for _, part := range result.Content {
		if part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n"), nil
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

func (c *StdioClient) start(ctx context.Context) (*session, error) {
	if strings.TrimSpace(c.cfg.Command) == "" {
		return nil, fmt.Errorf("mcp server %s command is required", c.cfg.Name)
	}
	cmd := exec.CommandContext(ctx, c.cfg.Command, c.cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range c.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go io.Copy(io.Discard, stderr)
	return &session{
		cmd:     cmd,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
		nextID:  1,
	}, nil
}

type session struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	nextID  int
}

func (s *session) initialize() error {
	var result map[string]any
	if err := s.request("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "AgentForge", "version": "0.1.0"},
	}, &result); err != nil {
		return err
	}
	return s.notify("notifications/initialized", map[string]any{})
}

func (s *session) request(method string, params any, result any) error {
	id := s.nextID
	s.nextID++
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := writeMessage(s.stdin, msg); err != nil {
		return err
	}
	for s.scanner.Scan() {
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(s.scanner.Bytes(), &resp); err != nil {
			continue
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("mcp %s: %s", method, resp.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	}
	if err := s.scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func (s *session) notify(method string, params any) error {
	return writeMessage(s.stdin, map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

func (s *session) close() {
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.cmd.Wait()
}

func writeMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
