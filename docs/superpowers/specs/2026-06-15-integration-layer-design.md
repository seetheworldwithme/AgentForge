# 设计：集成层（修复 plan.md 的割裂感）

> **日期：** 2026-06-15
> **状态：** 待用户审阅
> **目标文件：** 在 `plan.md` 中**新增**两节，不改动现有 T1-T39 任何内容。

## 1. 背景与问题

通读 `plan.md` 四部分后，确认"割裂感"的根因不是章节顺序（第一部分是架构决策、不可执行，直接实现第二部分是正确的），而是**缺了三块"胶水"任务**，导致各部分像孤岛：

| # | 缺口 | 证据 |
|---|------|------|
| **B** | 真实 OpenAI Provider 无对应 Task | Part 2 的 T4 只定义 `llm.Provider` **接口**，T5/T7 是 accumulator + FakeProvider。跑完 T1-T39 整个程序**无法真正对话**。 |
| **C** | 没有入口（组装）Task | Part 1 画了 `cmd/cli/main.go` 和 GUI 入口，但 T1-T39 **没有任何 Task** 把 `internal/*` 接成能跑的 `main`。Phase 1 检查点明写"不包含 CLI 入口"。 |
| **D** | Phase 1 与 Phase 2（RAG）无真实依赖 | T11 后 RAG（T12+）完全独立跑。Part 1 §7 的 V1→V3 演进与 RAG 是**两条平行线**，plan 没定义它们在哪汇合。Part 4 自己也承认 embedder"自持 HTTP client"，将来才"改为复用 llm/ 的 client"。 |

## 2. 目标

补 3 个可执行级 Task，把孤岛接起来，使：
- 跑完 Phase 1.5 → **有一个能真正对话的 CLI**（满足 plan §1 的 V1 成功标准）；
- 跑完 T39.5 → **对话引擎与 RAG 在工具层汇合**（模型能 function-call 查知识库再回答）。

**非目标：** 不重构现有 plan、不改 Embedder 的"自持 HTTP client"（用户已明确不做第 4 块胶水，仅在 spec 留 TODO）。

## 3. 方案：新增两节，编号 T11.5 / T11.6 / T39.5

用小数点编号表示"插入"，**T1-T39 编号完全不动**（T1-T3 已完成）。

### 插入位置

- **Phase 1.5「集成层」** → 插在 `### Phase 1 检查点`（plan 行 2461）**之后**、`# 第三部分：RAG 功能设计 spec`（行 2479）**之前**。
- **T39.5** → 插在 `## T38 & T39` 之后、`# 全局里程碑与检查点总览`（行 6441）之前。

### 3.1 新增：### Phase 1.5：集成层（让程序真正跑起来）

#### T11.5：OpenAI Provider（真实实现）

**目标：** 实现真正能调用 OpenAI 兼容 Chat Completions API 的 `OpenAIProvider`：流式 SSE 解析、tool_calls delta 复用 T5 的 accumulator、错误处理（401/429/网络）、非流式降级。填补 plan 缺口 B。

**依赖：** T3（conversation.Message）、T4（Provider/Request/Response/ToolDef）、T5（accumulator）。

**Files:**
- Create: `internal/llm/openai.go`
- Create: `internal/llm/openai_test.go`

**关键技术决策：**
- `net/http` + `bufio.Scanner` 解析 `data: {...}` SSE 帧（plan §5 指定）。
- tool_calls delta 用 T5 的 `newToolCallAccumulator`（已有，直接复用，不重写）。
- 内部 `chatRequest`/`chatChoice` 结构与 `conversation.Message` 字段名不同（`function`/`arguments`）——在 Provider 内做 **内部模型 ↔ 线上格式** 映射，印证 T3 加的 ToolCall godoc TODO。
- API key 仅走 header `Authorization: Bearer`，不进 URL query、不进日志（plan §10 安全基线）。
- 非流式降级：当 `req.OnDelta == nil` 时不设 `stream:true`，直接读完整 JSON。

**完整 Step（写入 plan 的内容草案）：**

- [ ] **Step 1: 添加依赖**（无新依赖，纯标准库）

- [ ] **Step 2: 写失败测试（httptest mock）**

Create `internal/llm/openai_test.go`:
```go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
)

// TestOpenAIProvider_StreamsDeltas 验证 SSE 逐 token 推送到 OnDelta，
// 且 Response.Message.Content 为完整文本。
func TestOpenAIProvider_StreamsDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-fake" {
			t.Errorf("missing/invalid auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// 模拟两帧 SSE：分片输出 "你好"
		chunks := []string{`{"choices":[{"delta":{"content":"你"}}]}`,
			`{"choices":[{"delta":{"content":"好"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`}
		for _, c := range chunks {
			w.Write([]byte("data: " + c + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	var collected string
	resp, err := p.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: conversation.RoleUser, Content: "hi"}},
		OnDelta:  func(s string) { collected += s },
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if collected != "你好" {
		t.Errorf("deltas got %q, want 你好", collected)
	}
	if resp.Message.Content != "你好" {
		t.Errorf("message content got %q, want 你好", resp.Message.Content)
	}
	if resp.Message.Role != conversation.RoleAssistant {
		t.Errorf("role got %s, want assistant", resp.Message.Role)
	}
}

// TestOpenAIProvider_AccumulatesToolCalls 验证分片 tool_calls delta 被正确累积。
func TestOpenAIProvider_AccumulatesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_system_info","arguments":"{\"q\":\""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"info\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		}
		for _, f := range frames {
			w.Write([]byte("data: " + f + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	resp, err := p.ChatStream(context.Background(), Request{
		Tools: []ToolDef{{Name: "get_system_info", Description: "x", Schema: []byte(`{}`)}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "get_system_info" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if string(tc.Args) != `{"q":"info"}` {
		t.Errorf("accumulated args got %q", string(tc.Args))
	}
}

// TestOpenAIProvider_ApiError 验证 401 返回明确错误。
func TestOpenAIProvider_ApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "Invalid API key"},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	_, err := p.ChatStream(context.Background(), Request{OnDelta: func(string) {}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

// TestOpenAIProvider_NonStreamingFallback 验证 OnDelta==nil 时走非流式。
func TestOpenAIProvider_NonStreamingFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if stream, ok := req["stream"].(bool); ok && stream {
			t.Error("non-streaming request should not set stream:true")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "完整回复"},
				 "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "sk-fake", "gpt-4o-mini")
	resp, err := p.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: conversation.RoleUser, Content: "hi"}},
		// OnDelta 故意不设
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Message.Content != "完整回复" {
		t.Errorf("got %q", resp.Message.Content)
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/llm/ -run TestOpenAIProvider -v`
Expected: 编译失败 —— `NewOpenAIProvider` 未定义。

- [ ] **Step 4: 实现 openai.go**

Create `internal/llm/openai.go`:
```go
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
		baseURL: baseURL, apiKey: apiKey, model: model,
		client: &http.Client{},
	}
}

// 线上格式（OpenAI wire format）
type wireMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
}
type wireToolCall struct {
	Index    int            `json:"index"`
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"type,omitempty"`
	Function wireFunction   `json:"function"`
}
type wireFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // OpenAI 用 string 编码的 JSON
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []wireMessage  `json:"messages"`
	Tools    []wireToolDef  `json:"tools,omitempty"`
	Stream   bool           `json:"stream,omitempty"`
}
type wireToolDef struct {
	Type     string      `json:"type"` // 固定 "function"
	Function ToolDef     `json:"function"`
}

// toWire 把内部 conversation.Message 转为线上格式。
func toWire(msg conversation.Message) wireMessage {
	wm := wireMessage{Role: string(msg.Role), Content: msg.Content}
	for _, tc := range msg.ToolCalls {
		wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
			ID: tc.ID, Type: "function",
			Function: wireFunction{Name: tc.Name, Arguments: string(tc.Args)},
		})
	}
	return wm
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req Request) (*Response, error) {
	streaming := req.OnDelta != nil
	body := chatRequest{
		Model: p.model, Stream: streaming,
		Tools: toWireToolDefs(req.Tools),
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
					Index: tc.Index, ID: tc.ID,
					FunctionName: tc.Function.Name,
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
			ID: ec.ID, Name: ec.Name, Args: json.RawMessage(ec.Args),
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
			ID: tc.ID, Name: tc.Function.Name,
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
```

> **⚠️ 注意 string → json.RawMessage 转换：** 上面 readStream/readFull 里 `Args: json.RawMessage(tc.Function.Arguments)` 是合法的——`json.RawMessage` 是 `[]byte` 的类型别名，可直接从 string 转换（Go 允许 `[]byte("str")` 这种显式转换）。注意：OpenAI 的 `function.arguments` 是 string 编码的 JSON，转成 `json.RawMessage` 后可直接回填给内部模型，无需二次解析。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/llm/ -v`  Expected: T5/T7 的 7 个 + T11.5 的 4 个，共 11 个全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/llm/openai.go internal/llm/openai_test.go
git commit -m "feat(llm): add OpenAIProvider with streaming SSE and tool_calls accumulation"
```

---

#### T11.6：CLI 入口（main.go）

**目标：** 写 `cmd/cli/main.go`，把 `internal/*` 接起来，满足 plan §1 的 V1 成功标准：`agentforge chat "你好"` 流式对话、`agentforge run <command>` 执行白名单命令。填补 plan 缺口 C。

**依赖：** T8-T10（Agent/Policy/registry）、T11.5（OpenAIProvider）。

**Files:**
- Create: `cmd/cli/main.go`

**关键技术决策：**
- CLI 框架：**cobra**（plan §9 明确指定）。引入 `github.com/spf13/cobra`。
- 配置来源：先从环境变量读（`AGENTFORGE_BASE_URL`/`AGENTFORGE_API_KEY`/`AGENTFORGE_MODEL`），缺省 `~/.agentforge/config.json`。**V1 不实现 SecureStorage 加密文件**（plan §10 是 GUI 才系统 Keychain，CLI 用文件），但留 TODO。
- api_key **绝不**进日志、不进命令行参数（会进进程列表，不安全）——只从 env/配置文件读。
- `chat` 命令：组装 provider + registry.Default() + conversation.Manager + Agent(Policy{AllowToolCalls:false})，调 `agent.Run`，sink 把 LoopDelta 写 stdout。
- `run` 命令：从 registry 取工具，直接 `Execute`，把 delta 流式打印。
- 成功标准（plan §1）：需真实 api_key 才能验证；CI 无法验证，但 httptest 单测能覆盖组装逻辑。

**完整 Step：**

- [ ] **Step 1: 添加依赖**

```bash
go get github.com/spf13/cobra@latest
```

- [ ] **Step 2: 写 main.go**

Create `cmd/cli/main.go`:
```go
// Package main 是 agentforge CLI 入口。
// 组装 internal/* 共享核心，提供 chat / run / config 子命令。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/agentforge/agentforge/internal/agent"
	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	registrypkg "github.com/agentforge/agentforge/internal/registry"
	"github.com/agentforge/agentforge/internal/tool"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agentforge",
		Short: "AgentForge - 跨平台智能 Agent 工具",
	}
	root.AddCommand(newChatCmd(), newRunCmd())
	return root
}

// config 从环境变量读，缺省回退到 ~/.agentforge/config.json。
// api_key 绝不进命令行参数或日志。
type config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

func loadConfig() (config, error) {
	cfg := config{
		BaseURL: os.Getenv("AGENTFORGE_BASE_URL"),
		APIKey:  os.Getenv("AGENTFORGE_API_KEY"),
		Model:   os.Getenv("AGENTFORGE_MODEL"),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	// 回退配置文件（api_key 优先用 env）
	path := configPath()
	if data, err := os.ReadFile(path); err == nil {
		var fc config
		if json.Unmarshal(data, &fc) == nil {
			if cfg.BaseURL == "" {
				cfg.BaseURL = fc.BaseURL
			}
			if cfg.APIKey == "" {
				cfg.APIKey = fc.APIKey
			}
			if cfg.Model == "" {
				cfg.Model = fc.Model
			}
		}
	}
	// TODO(secure storage): V2 接入系统 Keychain / 加密文件
	return cfg, nil
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "agentforge", "config.json")
}

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat [message]",
		Short: "与 Agent 对话（流式输出）",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if cfg.APIKey == "" {
				return fmt.Errorf("api_key 未配置：设置 AGENTFORGE_API_KEY 环境变量或写入 %s", configPath())
			}
			provider := llm.NewOpenAIProvider(cfg.BaseURL, cfg.APIKey, cfg.Model)
			registry := registrypkg.Default()
			mgr := conversation.NewManager()
			a := agent.NewAgent(provider, registry, mgr, agent.Policy{AllowToolCalls: false})

			sink := func(ev agent.LoopEvent) {
				if ev.Kind == agent.LoopDelta {
					fmt.Fprint(os.Stdout, ev.Text)
				}
			}
			return a.Run(context.Background(), args[0], sink)
		},
	}
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <command> [args]",
		Short: "执行白名单命令",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry := registrypkg.Default()
			t, ok := registry.Get(args[0])
			if !ok {
				list := listToolNames(registry)
				return fmt.Errorf("未知命令 %q；可用：%v", args[0], list)
			}
			callArgs := []byte(`{}`)
			if len(args) > 1 {
				callArgs = []byte(args[1])
			}
			events, err := t.Execute(context.Background(), callArgs)
			if err != nil {
				return err
			}
			for ev := range events {
				switch ev.Kind {
				case tool.EventDelta, tool.EventProgress:
					fmt.Fprintln(os.Stdout, ev.Text)
				case tool.EventResult:
					if ev.Result != nil {
						io.WriteString(os.Stdout, ev.Result.Content)
					}
				case tool.EventError:
					fmt.Fprintln(os.Stderr, "error:", ev.Text)
				}
			}
			return nil
		},
	}
}

func listToolNames(r *tool.Registry) []string {
	tools := r.List()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return names
}
```

- [ ] **Step 3: 验证编译 + 构建 CLI 二进制**

```bash
go build -o /tmp/agentforge.exe ./cmd/cli
go vet ./cmd/cli/
```
Expected: 无输出（成功）。二进制生成。

> **测试说明：** CLI 的真实运行需 api_key + 网络访问，CI 无法覆盖。组装逻辑（provider/registry/agent 的接线）已被 T8/T11/T11.5 的单元测试覆盖；CLI 层是薄壳，主要是参数解析。**不在 T11.6 写 httptest**——避免与 T11.5 重复且无增量价值（YAGNI，符合 CLAUDE.md §2）。验收靠下面的手动成功标准。

- [ ] **Step 4: 提交**

```bash
git add cmd/cli/main.go go.mod go.sum
git commit -m "feat(cli): add agentforge CLI entrypoint with chat and run commands"
```

---

#### Phase 1.5 检查点

完成 T11.5 + T11.6 后，**程序可真正运行**（满足 plan §1 的 V1 成功标准）：

| 验收 | 方式 |
|------|------|
| `go build ./...` 全绿 | `go build ./...` |
| 单测全绿 | `go test ./...` |
| CLI 能真实对话（需 api_key） | `AGENTFORGE_API_KEY=sk-xxx go run ./cmd/cli chat "你好"` 应流式输出 |
| CLI 能跑白名单命令 | `go run ./cmd/cli run get_system_info` 应打印系统信息 |

**Phase 1.5 不包含：** SecureStorage 加密文件（V2）、交互式 readline、GUI、RAG。

---

### 3.2 新增：## T39.5：知识库检索工具（RAG ↔ Agent Loop 集成）

**目标：** 把 RAG 检索封装为实现 `tool.Tool` 的 `KnowledgeBaseTool`，注册进 Registry。模型可通过 function calling 查知识库 → 拿 top-k chunk → 据此回答。**打通对话引擎与 RAG 两条平行线**（修复 plan 缺口 D）。

**依赖：** T2/T10（tool.Tool 接口与模式）、T25（Retriever）、T27（Service）。

**插入位置：** `## T38 & T39` 之后、`# 全局里程碑` 之前。

**Files:**
- Create: `internal/rag/tool.go`
- Create: `internal/rag/tool_test.go`

**关键技术决策：**
- `rag.Service` 已提供 `Retrieve(kbID, query, topK) ([]ScoredChunk, error)`（T27）。KnowledgeBaseTool 持有 `*rag.Service` + 目标 `kbID` + `topK`。
- Schema 让模型知道参数：`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`。
- Execute 把 top-k chunk 拼成 `HeadingPath + Content` 文本（plan R.3：喂 LLM 的是 HeadingPath+Content），作为 `Result.Content` 回填。非致命错误（store 失败）走 `IsError:true` 不崩 Loop。
- 注册时机：在 `internal/registry` 的 `Setup` 增加 `if ragService != nil { r.Register(rag.NewKnowledgeBaseTool(svc, kbID)) }`——但 `registry.Setup` 当前签名是 `func Setup(r *tool.Registry)`，**无法传 ragService**。决策：新增 `registry.SetupWithRAG(r, ragSvc, kbID)`，`Default()` 不变（无 RAG 时用）。这样不破坏 T10 已有签名与测试。

**完整 Step：**

- [ ] **Step 1: 写失败测试**

Create `internal/rag/tool_test.go`:
```go
package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/tool"
)

func TestKnowledgeBaseTool_RetrievesAndFormats(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	svc, err := NewService(ServiceConfig{
		DBPath: dbPath, EmbedDim: 32,
		Embedder: &embedder.FakeEmbedder{Dim: 32}, EmbeddingModel: "fake",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	kbID, _ := svc.CreateKnowledgeBase("test", 512, 50)

	mdPath := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(mdPath, []byte("# 安装\n\n用 brew 安装 agentforge。"), 0644)
	svc.ImportDocument(kbID, mdPath)

	kbTool := NewKnowledgeBaseTool(svc, kbID, 3)
	if kbTool.Name() != "search_knowledge_base" {
		t.Errorf("name got %q", kbTool.Name())
	}

	events, err := kbTool.Execute(context.Background(), []byte(`{"query":"安装"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resultContent string
	var isError bool
	for ev := range events {
		if ev.Kind == tool.EventResult && ev.Result != nil {
			resultContent = ev.Result.Content
			isError = ev.Result.IsError
		}
	}
	if isError {
		t.Error("expected successful result")
	}
	if resultContent == "" {
		t.Fatal("expected non-empty result content")
	}
}

func TestKnowledgeBaseTool_InvalidArgs(t *testing.T) {
	svc, _ := NewService(ServiceConfig{
		DBPath: filepath.Join(t.TempDir(), "t.db"), EmbedDim: 32,
		Embedder: &embedder.FakeEmbedder{Dim: 32}, EmbeddingModel: "fake",
	})
	defer svc.Close()
	kbTool := NewKnowledgeBaseTool(svc, "kb1", 3)

	events, _ := kbTool.Execute(context.Background(), []byte(`{bad json`))
	for ev := range events {
		if ev.Kind == tool.EventResult && ev.Result != nil && !ev.Result.IsError {
			t.Error("expected error result for invalid args")
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/rag/ -run TestKnowledgeBaseTool -v`
Expected: 编译失败 —— `NewKnowledgeBaseTool` 未定义。

- [ ] **Step 3: 实现 tool.go**

Create `internal/rag/tool.go`:
```go
package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agentforge/agentforge/internal/tool"
)

// KnowledgeBaseTool 把 RAG 检索暴露为一个 tool.Tool，
// 让 Agent Loop 能通过 function calling 查知识库。
type KnowledgeBaseTool struct {
	svc   *Service
	kbID  string
	topK  int
}

func NewKnowledgeBaseTool(svc *Service, kbID string, topK int) *KnowledgeBaseTool {
	if topK <= 0 {
		topK = 5
	}
	return &KnowledgeBaseTool{svc: svc, kbID: kbID, topK: topK}
}

func (t *KnowledgeBaseTool) Name() string        { return "search_knowledge_base" }
func (t *KnowledgeBaseTool) Description() string { return "在知识库中检索与问题相关的文档片段" }

func (t *KnowledgeBaseTool) Schema() []byte {
	return []byte(`{"type":"object","properties":{"query":{"type":"string","description":"检索问题"}},"required":["query"]}`)
}

func (t *KnowledgeBaseTool) Execute(ctx context.Context, args []byte) (<-chan tool.Event, error) {
	ch := make(chan tool.Event)
	go func() {
		defer close(ch)

		var p struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "参数解析失败: " + err.Error(), IsError: true,
			}}
			return
		}
		if strings.TrimSpace(p.Query) == "" {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "query 不能为空", IsError: true,
			}}
			return
		}

		chunks, err := t.svc.Retrieve(t.kbID, p.Query, t.topK)
		if err != nil {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "检索失败: " + err.Error(), IsError: true,
			}}
			return
		}
		if len(chunks) == 0 {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "知识库中未找到相关内容",
			}}
			return
		}

		ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
			Content: formatChunks(chunks),
		}}
	}()
	return ch, nil
}

// formatChunks 按 R.3 约定：喂 LLM 的是 HeadingPath + Content。
func formatChunks(chunks []ScoredChunk) string {
	var b strings.Builder
	for i, c := range chunks {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		if c.HeadingPath != "" {
			fmt.Fprintf(&b, "[%s]\n", c.HeadingPath)
		}
		if c.Source != "" {
			fmt.Fprintf(&b, "(来源: %s)\n", c.Source)
		}
		b.WriteString(c.Content)
	}
	return b.String()
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/rag/ -run TestKnowledgeBaseTool -v`
Expected: 2 个测试 PASS。

- [ ] **Step 5: 在 registry 包新增 SetupWithRAG**

Modify `internal/registry/registry.go`，追加：
```go
import "github.com/agentforge/agentforge/internal/rag"

// SetupWithRAG 注册内置工具 + 知识库检索工具。
// ragSvc 为 nil 时等价于 Setup（无 RAG）。
func SetupWithRAG(r *tool.Registry, ragSvc *rag.Service, kbID string) {
	Setup(r)
	if ragSvc != nil {
		r.Register(rag.NewKnowledgeBaseTool(ragSvc, kbID, 5))
	}
}
```

追加测试到 `internal/registry/registry_test.go`:
```go
func TestSetupWithRAG_NoRAG(t *testing.T) {
	r := tool.NewRegistry()
	SetupWithRAG(r, nil, "")
	// 无 RAG 时应等价于 Setup：有 get_system_info，无 search_knowledge_base
	if _, ok := r.Get("get_system_info"); !ok {
		t.Error("expected get_system_info registered")
	}
	if _, ok := r.Get("search_knowledge_base"); ok {
		t.Error("search_knowledge_base should NOT be registered when ragSvc is nil")
	}
}
```

- [ ] **Step 6: 运行全量测试**

Run: `go test ./...`
Expected: 所有包 PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/rag/tool.go internal/rag/tool_test.go internal/registry/
git commit -m "feat(rag): add KnowledgeBaseTool to integrate RAG retrieval into Agent Loop"
```

---

### 3.3 更新「全局里程碑与检查点总览」

在全局里程碑表中追加两行：
| Phase 1.5（T11.5-T11.6） | 程序可真实运行（CLI 对话 + 命令） | Provider + CLI 入口 |
| Phase 2 收尾（T39.5） | 对话引擎与 RAG 集成 | KnowledgeBaseTool |

## 4. 对 Part 4「自持 HTTP client」的处理

用户已确认**不做** Embedder 复用 llm client 这第 4 块胶水。处理方式：
- 不改 T13/T23 任何内容。
- 在 T11.5 的 spec 里加一句备注：「`OpenAIProvider` 的 config（base_url/api_key）来源统一为 `loadConfig()`；将来 `OpenAIEmbedder` 复用同一 config 是机械改动，接口不变」。
- 在全局风险登记簿追加一条：「Embedder 与 Provider 各自持 HTTP client，配置来源未统一（低风险，机械可改）」。

## 5. 范围边界

**做：** 3 个新 Task（T11.5/T11.6/T39.5）+ 全局里程碑表追加 + 风险登记簿追加 1 条。
**不做：** 改动任何现有 T1-T39 内容；改 Embedder；GUI；SecureStorage 加密文件；交互式 readline。

## 6. 风险与权衡

| 风险 | 影响 | 缓解 |
|------|------|------|
| OpenAI SSE 帧格式与 mock 不一致 | T11.5 测试通过但真实 API 失败 | Phase 1.5 检查点要求**真实 api_key 跑通**才算过 |
| cobra 引入第三方依赖 | 与 CLAUDE.md §2「零依赖」有张力 | 用户已明确「按 plan」选 cobra；plan §9 指定 cobra |
| CLI 无单测 | 组装 bug 晚发现 | 接线的各组件（provider/agent/registry）已有自己的单测；CLI 是薄壳 |
```
