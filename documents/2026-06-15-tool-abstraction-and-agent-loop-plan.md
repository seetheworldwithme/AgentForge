# Tool 抽象层 + Agent Loop 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/` 下建立 Agent 核心引擎——Tool 统一抽象、流式 Agent Loop、消息模型与 Context 压缩——为 AgentForge 的 CLI/GUI 双模式提供共享业务逻辑。

**Architecture:** Go 共享核心引擎位于 `internal/`，自底向上分层构建：`tool/`（工具接口）→ `conversation/`（消息模型）→ `llm/`（Provider 接口与流式累积）→ `agent/`（Loop + Policy）。每个包有清晰单一职责，通过接口通信，可独立测试。`command/` 包的白名单命令实现 `tool.Tool` 接口。

**Tech Stack:** Go 1.22+（实际 1.26.4）、标准库 `net/http`、`encoding/json`、`os/exec`、`context`、`sync`；测试用标准库 `testing` + `net/http/httptest`。第一版零第三方依赖（无 viper、无 tiktoken、无 go-sqlite3）。

**关联文档：** `documents/2026-06-15-tool-abstraction-and-agent-loop-design.md`（设计补丁）、`documents/agent-desktop-demo-技术栈与实现方案.md`（原方案）

---

## 文件结构总览

实施完成后将产生以下文件（每个文件单一职责）：

```
agent/
├── go.mod                                    # Task 1 创建
├── internal/
│   ├── tool/
│   │   ├── types.go                          # Task 2：Event/Result/EventKind 类型
│   │   ├── tool.go                           # Task 2：Tool 接口
│   │   ├── registry.go                       # Task 2：Registry 注册表
│   │   └── registry_test.go                  # Task 2
│   ├── conversation/
│   │   ├── message.go                        # Task 3：Message/Role/ToolCall 类型
│   │   ├── manager.go                        # Task 3：Manager 基础方法
│   │   ├── manager_test.go                   # Task 3
│   │   ├── compress.go                       # Task 6：摘要压缩与边界切分
│   │   └── compress_test.go                  # Task 6
│   ├── llm/
│   │   ├── provider.go                       # Task 4：Provider 接口 + Request/Response/ToolDef
│   │   ├── accumulator.go                    # Task 5：流式 tool_calls 增量累积
│   │   ├── accumulator_test.go               # Task 5
│   │   └── fake_provider.go                  # Task 7：测试用 fake Provider
│   ├── agent/
│   │   ├── errors.go                         # Task 8：ErrMaxIterationsReached
│   │   ├── policy.go                         # Task 8：Policy + LoopEvent + EventSink
│   │   ├── agent.go                          # Task 8：Agent 结构体 + V1 Loop；Task 9 在此追加 executeToolCall
│   │   ├── agent_v1_test.go                  # Task 8
│   │   └── agent_v2_test.go                  # Task 9：V2 Loop（含工具调用）
│   ├── command/
│   │   ├── types.go                          # Task 10：白名单命令定义
│   │   ├── runner.go                         # Task 10：命令执行（结构化传参）
│   │   ├── runner_test.go                    # Task 10
│   │   └── tools.go                          # Task 10：实现 tool.Tool 的 SystemInfoTool
│   └── registry/
│       ├── registry.go                       # Task 10：集中注册（打破 tool↔command 循环）
│       └── registry_test.go                  # Task 10
```

---

## Task 1：初始化 Go Module

**目标：** 建立工程基础，确保后续任务有可编译的 module。

**Files:**
- Create: `go.mod`

- [ ] **Step 1: 初始化 Go module**

Run:
```bash
cd F:\code\Go\myself\agent
go mod init github.com/agentforge/agentforge
```

Expected: 创建 `go.mod`，内容包含 `module github.com/agentforge/agentforge` 与 `go 1.26`。

- [ ] **Step 2: 验证 module 可编译**

Run:
```bash
go build ./...
```

Expected: 无输出（成功，当前无源文件）。

- [ ] **Step 3: 提交**

```bash
git add go.mod
git commit -m "chore: init go module github.com/agentforge/agentforge"
```

---

## Task 2：Tool 抽象层与 Registry

**目标：** 建立 `internal/tool/` 包，定义 `Tool` 接口、`Event`/`Result` 事件模型与集中式 `Registry`。这是整个架构的地基，后续所有工具都实现此接口。

**设计依据：** 补丁 §3（Tool 抽象层）。

**Files:**
- Create: `internal/tool/types.go`
- Create: `internal/tool/tool.go`
- Create: `internal/tool/registry.go`
- Create: `internal/tool/registry_test.go`

- [ ] **Step 1: 创建事件与结果类型**

Create `internal/tool/types.go`:

```go
// Package tool 定义 Agent 工具的统一抽象。
// 所有能力（命令执行、文件操作、未来扩展）都实现 Tool 接口，
// 通过 Registry 注册，供 Agent Loop 调度。
package tool

import "context"

// EventKind 标识工具执行过程中推送的事件类型。Loop 按类型分发处理。
type EventKind int

const (
	// EventDelta 中间输出（命令的 stdout 行、文件读取片段）→ 实时推给用户。
	EventDelta EventKind = iota
	// EventProgress 进度信息（可选，GUI 可用作进度条）。
	EventProgress
	// EventResult 最终结构化结果，必须且只能出现一次，作为通道关闭前的最后一个事件。
	EventResult
	// EventError 工具执行出错（非致命，可继续对话）。
	EventError
)

// Event 是工具通过通道推送的单个事件。
type Event struct {
	Kind EventKind
	// Text 用于 Delta / Progress / Error 的文本。
	Text string
	// Result 仅 EventResult 使用，内容回填给 LLM 作为 tool 消息。
	Result *Result
}

// Result 是回填给 LLM 的工具结果，对应 OpenAI "tool" role message。
type Result struct {
	// Content 回填给模型的文本内容。
	Content string
	// IsError 标记为错误（模型可据此调整策略），非致命错误用此而非返回 error。
	IsError bool
}

// Tool 是所有工具的统一接口。
type Tool interface {
	// Name 工具唯一标识，用于 function calling 的 "name" 字段。
	Name() string
	// Description 给 LLM 看的描述（影响模型是否选择调用此工具）。
	Description() string
	// Schema 参数的 JSON Schema，遵循 OpenAI function calling 规范。
	Schema() []byte
	// Execute 流式执行。返回事件通道，调用方读取直到关闭。
	// 工具侧负责：在 goroutine 中执行并通过 ctx 响应取消；通道必须最终关闭。
	Execute(ctx context.Context, args []byte) (<-chan Event, error)
}
```

说明：`Schema()` 返回 `[]byte`（即 `json.RawMessage` 的底层类型）而非 `json.RawMessage`，避免引入 `encoding/json` 到接口签名——json.RawMessage 只是类型别名，用 `[]byte` 更通用。

- [ ] **Step 2: 创建 Registry 注册表**

Create `internal/tool/registry.go`:

```go
package tool

import "sync"

// Registry 管理可用工具集合。V1 用全局 Default 实例。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// Default 是全局默认注册表，内置工具通过 init() 注册到这里。
var Default = NewRegistry()

// NewRegistry 创建空的 Registry。
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register 注册一个工具。同名工具会被覆盖（用于测试时替换）。
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get 按名称查找工具。
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List 返回所有已注册工具的切片（顺序不保证，调用方如需稳定顺序应自行排序）。
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}
```

- [ ] **Step 3: 写失败测试 —— Registry 基本行为**

Create `internal/tool/registry_test.go`:

```go
package tool

import (
	"context"
	"reflect"
	"testing"
)

// fakeTool 是测试用的 Tool 实现。
type fakeTool struct {
	name string
}

func (f *fakeTool) Name() string                                    { return f.name }
func (f *fakeTool) Description() string                             { return "fake" }
func (f *fakeTool) Schema() []byte                                  { return []byte(`{}`) }
func (f *fakeTool) Execute(ctx context.Context, args []byte) (<-chan Event, error) {
	ch := make(chan Event)
	go func() {
		defer close(ch)
		ch <- Event{Kind: EventResult, Result: &Result{Content: "ok"}}
	}()
	return ch, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	t1 := &fakeTool{name: "tool_a"}

	r.Register(t1)

	got, ok := r.Get("tool_a")
	if !ok {
		t.Fatal("expected to find registered tool")
	}
	if got.Name() != "tool_a" {
		t.Fatalf("got name %q, want %q", got.Name(), "tool_a")
	}
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get("nonexistent"); ok {
		t.Fatal("expected miss for unregistered tool")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeTool{name: "a"})
	r.Register(&fakeTool{name: "b"})

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(list))
	}
	// 验证两个名字都在（顺序不保证）
	names := map[string]bool{}
	for _, tool := range list {
		names[tool.Name()] = true
	}
	if !reflect.DeepEqual(names, map[string]bool{"a": true, "b": true}) {
		t.Fatalf("unexpected tool names: %v", names)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/tool/ -v -run TestRegistry
```

Expected: 3 个测试全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/tool/
git commit -m "feat(tool): add Tool interface, Event/Result types, and Registry"
```

---

## Task 3：消息模型与 Manager 基础

**目标：** 建立 `internal/conversation/` 包，定义 `Message`/`Role`/`ToolCall` 类型与 `Manager` 的基础增删查方法（不含压缩，压缩在 Task 6）。

**设计依据：** 补丁 §5.1（完整 Message 结构）。

**Files:**
- Create: `internal/conversation/message.go`
- Create: `internal/conversation/manager.go`
- Create: `internal/conversation/manager_test.go`

- [ ] **Step 1: 定义消息类型**

Create `internal/conversation/message.go`:

```go
// Package conversation 定义 Agent 对话的消息模型与历史管理。
// Message 结构覆盖 OpenAI Chat Completions 的所有角色，
// 包括 function calling 所需的 tool_calls 与 tool role 回填。
package conversation

import "encoding/json"

// Role 标识消息发送方角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool 工具执行结果，通过 ToolCallID 关联到对应的 ToolCall。
	RoleTool Role = "tool"
)

// ToolCall 是 assistant 消息里发起的工具调用请求。
type ToolCall struct {
	// ID 模型生成，回填 tool 消息时必须对应（OpenAI 要求配对）。
	ID string `json:"id"`
	// Name 被调用的工具名。
	Name string `json:"name"`
	// Args 工具参数（JSON 字符串，对应 OpenAI 的 function.arguments）。
	Args json.RawMessage `json:"args"`
}

// Message 是对话中的一条消息，兼容 OpenAI Chat Completions 格式。
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`    // 仅 RoleAssistant 使用
	ToolCallID string     `json:"tool_call_id,omitempty"` // 仅 RoleTool 使用：关联的调用 ID
	Name       string     `json:"name,omitempty"`         // 仅 RoleTool 使用：工具名
}
```

- [ ] **Step 2: 创建 Manager 基础方法**

Create `internal/conversation/manager.go`:

```go
package conversation

// Manager 负责消息存储与（Task 6 加入的）context 压缩。
// 本任务仅实现基础的追加与查询；压缩逻辑见 compress.go。
type Manager struct {
	messages []Message
}

// NewManager 创建空 Manager。
func NewManager() *Manager {
	return &Manager{}
}

// AppendSystem 追加 system 消息（通常对话开始时一次性设置人设）。
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
// toolCallID 必须对应之前 assistant 消息里的某个 ToolCall.ID。
func (m *Manager) AppendToolResult(toolCallID, toolName, content string, isError bool) {
	m.messages = append(m.messages, Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		Name:       toolName,
	})
}

// Messages 返回当前所有消息（未经压缩）。
func (m *Manager) Messages() []Message {
	return m.messages
}

// ForRequest 返回发给 LLM 的消息序列。
// Task 6 会在此加入压缩逻辑；当前直接返回全量。
func (m *Manager) ForRequest() []Message {
	return m.messages
}
```

- [ ] **Step 3: 写失败测试 —— 追加与查询**

Create `internal/conversation/manager_test.go`:

```go
package conversation

import "testing"

func TestManagerAppendAndMessages(t *testing.T) {
	m := NewManager()
	m.AppendSystem("你是助手")
	m.AppendUser("你好")

	msgs := m.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != RoleSystem || msgs[0].Content != "你是助手" {
		t.Errorf("unexpected system message: %+v", msgs[0])
	}
	if msgs[1].Role != RoleUser || msgs[1].Content != "你好" {
		t.Errorf("unexpected user message: %+v", msgs[1])
	}
}

func TestManagerAppendAssistantWithToolCalls(t *testing.T) {
	m := NewManager()
	m.AppendUser("查一下系统信息")

	m.AppendAssistant(Message{
		Content: "我来查一下",
		ToolCalls: []ToolCall{
			{ID: "call_1", Name: "get_system_info", Args: []byte(`{}`)},
		},
	})

	msgs := m.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	assistant := msgs[1]
	if assistant.Role != RoleAssistant {
		t.Errorf("expected assistant role, got %s", assistant.Role)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_1" {
		t.Errorf("unexpected tool calls: %+v", assistant.ToolCalls)
	}
}

func TestManagerAppendToolResult(t *testing.T) {
	m := NewManager()
	m.AppendAssistant(Message{
		ToolCalls: []ToolCall{{ID: "call_1", Name: "get_system_info"}},
	})
	m.AppendToolResult("call_1", "get_system_info", "OS: Windows", false)

	msgs := m.Messages()
	toolMsg := msgs[1]
	if toolMsg.Role != RoleTool {
		t.Errorf("expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id call_1, got %s", toolMsg.ToolCallID)
	}
	if toolMsg.Name != "get_system_info" {
		t.Errorf("expected name get_system_info, got %s", toolMsg.Name)
	}
}

func TestManagerForRequestReturnsAllBeforeCompress(t *testing.T) {
	m := NewManager()
	m.AppendUser("a")
	m.AppendUser("b")
	if got := m.ForRequest(); len(got) != 2 {
		t.Fatalf("ForRequest before compress should return all; got %d", len(got))
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/conversation/ -v
```

Expected: 4 个测试全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/conversation/
git commit -m "feat(conversation): add Message model and Manager with append/query"
```

---

## Task 4：LLM Provider 接口

**目标：** 建立 `internal/llm/` 包，定义 `Provider` 接口、`Request`/`Response`/`ToolDef` 类型。本任务只定义接口，不实现具体 Provider——实现是后续阶段的事，Agent Loop 测试用 fake。

**设计依据：** 补丁 §6.1（接口化）。

**Files:**
- Create: `internal/llm/provider.go`

- [ ] **Step 1: 定义 Provider 接口与请求/响应类型**

Create `internal/llm/provider.go`:

```go
// Package llm 定义大模型 Provider 的抽象接口。
// 当前仅定义接口与数据类型；具体 OpenAI 兼容实现在后续阶段补充。
// 测试 Agent Loop 时使用 fake_provider.go 提供的桩实现。
package llm

import (
	"context"

	"github.com/agentforge/agentforge/internal/conversation"
)

// Provider 是大模型后端的统一接口。
// 实现方负责：发起 HTTP 请求、解析 SSE 流、累积 tool_calls 分片、
// 通过 OnDelta 回调实时推送文本 token。
type Provider interface {
	// ChatStream 发起一次流式对话请求。
	//   - req.Tools 为空时表示纯对话模式（不启用 function calling）。
	//   - req.OnDelta 在每个文本 token 到达时被调用（可能并发，实现方需自行同步或单线程派发）。
	// 返回的 Response.Message 包含完整 assistant 消息（含累积后的 tool_calls）。
	ChatStream(ctx context.Context, req Request) (*Response, error)
}

// ToolDef 是传给 API 的工具定义（OpenAI tools 数组的一项）。
type ToolDef struct {
	Name        string
	Description string
	Schema      []byte // JSON Schema
}

// Request 是一次对话请求的参数。
type Request struct {
	// Messages 发给模型的消息序列（来自 conversation.Manager.ForRequest）。
	Messages []conversation.Message
	// Tools 暴露给模型的工具定义。nil 或空 = 不启用 function calling。
	Tools []ToolDef
	// OnDelta 流式文本 token 回调。Loop 在此把 token 推给用户。
	OnDelta func(text string)
}

// Response 是一次对话请求的结果。
type Response struct {
	// Message 完整的 assistant 消息（流结束后组装）。
	Message conversation.Message
}
```

- [ ] **Step 2: 验证编译**

Run:
```bash
go build ./internal/llm/
```

Expected: 无输出（成功）。

- [ ] **Step 3: 提交**

```bash
git add internal/llm/
git commit -m "feat(llm): add Provider interface, Request/Response, ToolDef types"
```

---

## Task 5：流式 tool_calls 增量累积器

**目标：** 实现 OpenAI SSE 流式响应中 `tool_calls` 分片 delta 的累积器。这是原方案"bufio.Scanner 逐行解析"完全没展开的关键难点——tool_calls 是按 index 分片到达的，需要拼装。

**设计依据：** 补丁 §6.2（流式 tool_calls 的增量累积）。

**Files:**
- Create: `internal/llm/accumulator.go`
- Create: `internal/llm/accumulator_test.go`

- [ ] **Step 1: 写失败测试 —— 单个调用的分片累积**

Create `internal/llm/accumulator_test.go`:

```go
package llm

import (
	"reflect"
	"testing"
)

// deltaChunk 模拟 OpenAI SSE 里的一个 tool_calls delta 分片。
type deltaChunk struct {
	Index         int
	ID            string // 仅首个分片有，后续为空
	FunctionName  string // 仅首个分片有，后续为空
	ArgumentsFrag string // arguments 的分片
}

func TestAccumulatorSingleCall(t *testing.T) {
	acc := newToolCallAccumulator()

	// 模拟 OpenAI 流：第一个分片带 id 和 function.name
	acc.add(deltaChunk{Index: 0, ID: "call_1", FunctionName: "get_system_info", ArgumentsFrag: `{"query":"`})
	// 第二个分片只有 arguments 的后续片段
	acc.add(deltaChunk{Index: 0, ArgumentsFrag: `memory"`})
	// 第三个分片收尾
	acc.add(deltaChunk{Index: 0, ArgumentsFrag: `}`})

	got := acc.result()
	want := []expectedCall{
		{ID: "call_1", Name: "get_system_info", Args: `{"query":"memory"}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAccumulatorMultipleCalls(t *testing.T) {
	acc := newToolCallAccumulator()

	// 两个调用交替到达（OpenAI 真实行为：按 index 分片，可能交错）
	acc.add(deltaChunk{Index: 0, ID: "call_1", FunctionName: "tool_a", ArgumentsFrag: `{}`})
	acc.add(deltaChunk{Index: 1, ID: "call_2", FunctionName: "tool_b", ArgumentsFrag: `{"x":1`})
	acc.add(deltaChunk{Index: 1, ArgumentsFrag: `}`})

	got := acc.result()
	want := []expectedCall{
		{ID: "call_1", Name: "tool_a", Args: `{}`},
		{ID: "call_2", Name: "tool_b", Args: `{"x":1}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAccumulatorEmpty(t *testing.T) {
	acc := newToolCallAccumulator()
	got := acc.result()
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %+v", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/llm/ -v -run TestAccumulator
```

Expected: 编译失败 —— `newToolCallAccumulator` 未定义。

- [ ] **Step 3: 实现累积器**

Create `internal/llm/accumulator.go`:

```go
package llm

import (
	"encoding/json"
	"sort"

	"github.com/agentforge/agentforge/internal/conversation"
)

// deltaChunk 描述 OpenAI SSE 流中一个 tool_calls 分片。
// 累积器是 OpenAI 协议特有的逻辑，故类型定义在此包内。
type deltaChunk struct {
	// Index 对应 OpenAI delta.tool_calls[].index，用于定位累积到哪个 call。
	Index int
	// ID 仅首个分片携带（后续为空），对应 tool_calls[].id。
	ID string
	// FunctionName 仅首个分片携带，对应 tool_calls[].function.name。
	FunctionName string
	// ArgumentsFrag arguments JSON 的分片字符串，需按顺序拼接。
	ArgumentsFrag string
}

// toolCallAccumulator 累积流式到达的 tool_calls 分片。
// 每个 index 对应一个独立的工具调用。
type toolCallAccumulator struct {
	calls map[int]*conversation.ToolCall
	// order 记录 index 首次出现的顺序，保证输出稳定。
	order []int
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{calls: make(map[int]*conversation.ToolCall)}
}

// add 追加一个分片。首个分片建立 call 条目，后续分片追加 arguments。
func (a *toolCallAccumulator) add(d deltaChunk) {
	call, exists := a.calls[d.Index]
	if !exists {
		call = &conversation.ToolCall{}
		a.calls[d.Index] = call
		a.order = append(a.order, d.Index)
	}
	// id 与 name 仅首个分片有
	if d.ID != "" {
		call.ID = d.ID
	}
	if d.FunctionName != "" {
		call.Name = d.FunctionName
	}
	// arguments 按到达顺序拼接
	if d.ArgumentsFrag != "" {
		// 追加到现有 args（RawMessage 底层是 []byte）
		call.Args = append(call.Args, []byte(d.ArgumentsFrag)...)
	}
}

// result 返回累积完成的工具调用切片，按 index 升序排列。
func (a *toolCallAccumulator) result() []expectedCall {
	if len(a.calls) == 0 {
		return nil
	}
	indices := make([]int, 0, len(a.calls))
	for i := range a.calls {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	result := make([]expectedCall, 0, len(indices))
	for _, idx := range indices {
		call := a.calls[idx]
		// 校验 JSON 合法性（累积出错时 args 可能为非法 JSON）
		_ = json.Valid(call.Args) // V1 仅校验不报错，未来可记录警告
		result = append(result, expectedCall{
			ID:   call.ID,
			Name: call.Name,
			Args: string(call.Args),
		})
	}
	return result
}

// expectedCall 是累积结果的可观测形式（Args 为字符串便于断言）。
// 定义在实现文件而非测试文件，使测试与实现共用同一类型。
type expectedCall struct {
	ID   string
	Name string
	Args string
}
```

说明：`result()` 返回 `[]expectedCall`（Args 为字符串便于测试断言），而非 `[]conversation.ToolCall`。未来真实的 OpenAI Provider 实现会在此基础上直接用累积好的 `a.calls`（已是 `map[int]*conversation.ToolCall`）输出 `[]conversation.ToolCall`，本任务先保证累积逻辑本身的正确性可测。

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/llm/ -v -run TestAccumulator
```

Expected: 3 个测试全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/llm/accumulator.go internal/llm/accumulator_test.go
git commit -m "feat(llm): add streaming tool_calls delta accumulator"
```

---

## Task 6：Context 摘要压缩

**目标：** 为 `conversation.Manager` 增加摘要压缩能力：超过 token 阈值时，把旧消息（按安全边界切分）压缩成摘要。**关键约束：不能切断 tool_call ↔ tool_result 的配对**，否则 OpenAI API 返回 400。

**设计依据：** 补丁 §5.2（摘要压缩与压缩的关键约束）。

**Files:**
- Create: `internal/conversation/compress.go`
- Create: `internal/conversation/compress_test.go`
- Modify: `internal/conversation/manager.go`

- [ ] **Step 1: 写失败测试 —— 压缩触发与边界安全**

Create `internal/conversation/compress_test.go`:

```go
package conversation

import "testing"

// stubSummarizer 是测试用的摘要生成器，返回固定文本。
type stubSummarizer struct {
	calls   int
	returns string
}

func (s *stubSummarizer) Summarize(msgs []Message) (string, error) {
	s.calls++
	return s.returns, nil
}

func TestCompressNotTriggeredUnderThreshold(t *testing.T) {
	stub := &stubSummarizer{returns: "摘要"}
	m := NewManager(WithSummarizer(stub), WithMaxTokens(1000))
	m.AppendUser("短消息") // ~3 tokens，远低于阈值

	_ = m.ForRequest()
	if stub.calls != 0 {
		t.Fatalf("summarizer should not be called under threshold; calls=%d", stub.calls)
	}
}

func TestCompressTriggeredOverThreshold(t *testing.T) {
	stub := &stubSummarizer{returns: "[摘要]"}
	m := NewManager(WithSummarizer(stub), WithMaxTokens(5)) // 极低阈值
	// 构造可压缩的多轮对话：早期消息会被压缩，最后一条 user 保留
	m.AppendUser("第一轮长消息用来触发压缩")
	m.AppendAssistant(Message{Content: "第一轮回复也很长"})
	m.AppendUser("第二轮保留近期")

	msgs := m.ForRequest()
	if stub.calls == 0 {
		t.Fatal("summarizer should be called over threshold")
	}
	// 压缩后应包含摘要 system 消息 + 保留的近期消息
	if len(msgs) == 0 {
		t.Fatal("expected non-empty messages after compress")
	}
	// 第一条应是 system（摘要）
	if msgs[0].Role != RoleSystem {
		t.Errorf("expected first message to be system summary, got %s", msgs[0].Role)
	}
	// 保留区应包含最后一条 user 消息
	foundRecent := false
	for _, msg := range msgs {
		if msg.Content == "第二轮保留近期" {
			foundRecent = true
		}
	}
	if !foundRecent {
		t.Error("expected recent user message preserved after compress")
	}
}

func TestCompressPreservesToolCallPairing(t *testing.T) {
	stub := &stubSummarizer{returns: "[摘要]"}
	m := NewManager(WithSummarizer(stub), WithMaxTokens(5))

	// 构造一段会切断配对的历史：
	// [user] [assistant(tool_call=call_1)] [user] [tool(result for call_1)]
	// 若压缩在 assistant 与 tool 之间切，会留下孤立的 tool 消息 → API 400。
	m.AppendUser("第一轮")
	m.AppendAssistant(Message{
		ToolCalls: []ToolCall{{ID: "call_1", Name: "x"}},
	})
	m.AppendUser("第二轮")
	m.AppendToolResult("call_1", "x", "result", false)

	msgs := m.ForRequest()

	// 验证：保留的 tool 消息前必须有对应的 tool_call
	seenCallIDs := map[string]bool{}
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			seenCallIDs[tc.ID] = true
		}
		if msg.Role == RoleTool {
			if !seenCallIDs[msg.ToolCallID] {
				t.Fatalf("tool result %q has no preceding tool_call in compressed messages", msg.ToolCallID)
			}
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	// 简单估算：字符数 / 4
	tests := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"hello", 1},       // 5/4 = 1
		{"hello world!", 3}, // 12/4 = 3
	}
	for _, tt := range tests {
		got := estimateTokens(tt.content)
		if got != tt.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.content, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/conversation/ -v -run TestCompress
```

Expected: 编译失败 —— `WithSummarizer`/`WithMaxTokens`/`estimateTokens` 未定义。

- [ ] **Step 3: 实现 compress.go**

Create `internal/conversation/compress.go`:

```go
package conversation

// Summarizer 负责把一段旧消息压缩成摘要文本。
// 生产实现会调用 LLM；测试用 stub。定义成接口便于替换。
type Summarizer interface {
	Summarize(msgs []Message) (string, error)
}

// 选项模式（functional options）配置 Manager。
type option func(*Manager)

// WithSummarizer 设置摘要生成器。不设置则压缩降级为"丢弃"（仅保留近期）。
func WithSummarizer(s Summarizer) option {
	return func(m *Manager) { m.summarizer = s }
}

// WithMaxTokens 设置触发压缩的 token 阈值（估算值）。
func WithMaxTokens(n int) option {
	return func(m *Manager) { m.maxTokens = n }
}

// estimateTokens 粗略估算文本的 token 数。
// V1 用字符数/4 的近似（英文约 4 字符/token，中文偏保守）。
// 不引入 tokenizer 依赖；后续如需精确可换 tiktoken-go。
func estimateTokens(content string) int {
	return len(content) / 4
}

// estimateMessagesTokens 估算消息序列的总 token 数（仅算 content，忽略结构开销）。
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
```

- [ ] **Step 4: 修改 manager.go 加入压缩逻辑**

Replace `internal/conversation/manager.go` 的全部内容为：

```go
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

// ForRequest 返回发给 LLM 的消息序列。
// 若配置了 maxTokens 且当前消息超阈值，则触发压缩。
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
//
// 安全边界规则（关键约束）：
// 一个安全切分点 = 一条 user 消息之前。
// 即：被压缩的旧块以 assistant/tool 结尾时，必须连同后续的 tool 结果一起压缩，
// 不能把 tool_call 和它的 tool_result 拆到不同块——否则 OpenAI API 返回 400。
//
// 实现策略：从末尾向前找第一个"安全边界"，使得保留区内的每个 tool 消息
// 都能在保留区内找到对应的 tool_call。
func (m *Manager) compress() []Message {
	// 找到保留区起点：从末尾向前，找到第一个满足"保留区内 tool 消息全有配对 call"的位置。
	keepFrom := findSafeKeepFrom(m.messages)

	if keepFrom == 0 {
		// 无法安全压缩（整段都在一个配对组里），放弃压缩返回原样。
		return m.messages
	}

	oldMessages := m.messages[:keepFrom]
	recentMessages := m.messages[keepFrom:]

	// 生成摘要
	var summary string
	if m.summarizer != nil {
		s, err := m.summarizer.Summarize(oldMessages)
		if err == nil {
			summary = s
		}
		// 摘要失败则降级：不生成摘要，仅丢弃旧消息（已在 recentMessages 之外）
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

// findSafeKeepFrom 从末尾向前扫描，找到保留区的起点索引。
// 保留区必须满足：其中每个 RoleTool 消息的 ToolCallID 都能在保留区内
// 找到对应的 ToolCall（即 assistant 消息里的同 ID 调用）。
//
// 策略：保留区从"最后一条 user 消息"开始向前扩展，
// 直到保留区内所有 tool 消息都有配对的 tool_call。
// 若整个消息序列里没有任何 tool 配对问题，则可以保留尽可能少的近期消息。
func findSafeKeepFrom(msgs []Message) int {
	// 先找到最后一条 user 消息作为初始保留区起点
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			lastUser = i
			break
		}
	}
	// 没有 user 消息 → 无法安全压缩（没有自然边界），返回 0 表示不压缩
	if lastUser < 0 {
		return 0
	}

	// 从 lastUser 向前扩展保留区，直到保留区内所有 tool 结果都有配对 call
	for keepFrom := lastUser; keepFrom >= 0; keepFrom-- {
		if allToolResultsPaired(msgs[keepFrom:]) {
			return keepFrom
		}
	}
	// 退到最前仍不安全 → 返回 0 表示不可压缩
	return 0
}

// allToolResultsPaired 检查给定消息序列内每个 tool 消息是否都有配对的 call。
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
```

- [ ] **Step 5: 运行全部 conversation 测试**

Run:
```bash
go test ./internal/conversation/ -v
```

Expected: 包括 Task 3 的 4 个测试 + 本任务的 4 个测试，共 8 个全部 PASS。

注意：Task 3 的 `TestManagerForRequestReturnsAllBeforeCompress` 仍应通过——`NewManager()` 无 option 时 `maxTokens=0`，`ForRequest()` 直接返回全量。

- [ ] **Step 6: 提交**

```bash
git add internal/conversation/
git commit -m "feat(conversation): add summary compression with tool_call pairing safety"
```

---

## Task 7：测试用 Fake Provider

**目标：** 创建一个供 Agent Loop 测试用的 `Provider` 桩实现。它支持脚本化：按预设序列返回 Response（可含 tool_calls），并记录调用参数便于断言。

**设计依据：** 设计补丁未直接定义，但 Agent Loop 的 TDD 必需——不能依赖真实 API。

**Files:**
- Create: `internal/llm/fake_provider.go`
- Create: `internal/llm/fake_provider_test.go`

- [ ] **Step 1: 写失败测试 —— Fake Provider 基本行为**

Create `internal/llm/fake_provider_test.go`:

```go
package llm

import (
	"context"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
)

func TestFakeProviderReturnsScriptedResponses(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{
		{Text: "你好", DeltaText: "你好"},
		{Text: "还有什么需要帮忙的吗？", DeltaText: "还有什么需要帮忙的吗？"},
	})

	// 第一次调用
	var deltas1 string
	resp1, err := fp.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: "user", Content: "hi"}},
		OnDelta:  func(s string) { deltas1 += s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp1.Message.Content != "你好" {
		t.Errorf("got %q, want 你好", resp1.Message.Content)
	}
	if deltas1 != "你好" {
		t.Errorf("deltas got %q, want 你好", deltas1)
	}

	// 第二次调用
	resp2, _ := fp.ChatStream(context.Background(), Request{OnDelta: func(string) {}})
	if resp2.Message.Content != "还有什么需要帮忙的吗？" {
		t.Errorf("second call got %q", resp2.Message.Content)
	}
}

func TestFakeProviderRecordsRequests(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{{Text: "ok"}})

	_, _ = fp.ChatStream(context.Background(), Request{
		Messages: []conversation.Message{{Role: "user", Content: "测试记录"}},
		Tools:    []ToolDef{{Name: "tool_x", Description: "x", Schema: []byte(`{}`)}},
	})

	calls := fp.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(calls))
	}
	if calls[0].Messages[0].Content != "测试记录" {
		t.Errorf("unexpected recorded message: %+v", calls[0].Messages[0])
	}
	if len(calls[0].Tools) != 1 || calls[0].Tools[0].Name != "tool_x" {
		t.Errorf("tools not recorded: %+v", calls[0].Tools)
	}
}

func TestFakeProviderReturnsToolCalls(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{
		{
			Text: "我来查一下",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "get_system_info", Args: []byte(`{}`)},
			},
		},
	})

	resp, err := fp.ChatStream(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Name != "get_system_info" {
		t.Errorf("got tool name %q", resp.Message.ToolCalls[0].Name)
	}
}

func TestFakeProviderExhaustedScript(t *testing.T) {
	fp := NewFakeProvider()
	fp.Script([]FakeResponse{{Text: "only one"}})

	_, _ = fp.ChatStream(context.Background(), Request{})
	_, err := fp.ChatStream(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error when script exhausted")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/llm/ -v -run TestFakeProvider
```

Expected: 编译失败 —— `FakeProvider`/`NewFakeProvider`/`FakeResponse` 未定义。

- [ ] **Step 3: 实现 Fake Provider**

Create `internal/llm/fake_provider.go`:

```go
package llm

import (
	"context"
	"errors"

	"github.com/agentforge/agentforge/internal/conversation"
)

// FakeResponse 是脚本化的单次响应。
type FakeResponse struct {
	// Text assistant 消息的 content。
	Text string
	// DeltaText 通过 OnDelta 回调推送的文本（模拟流式）。为空则用 Text。
	DeltaText string
	// ToolCalls 本次响应携带的工具调用。为空则表示对话结束。
	ToolCalls []conversation.ToolCall
}

// FakeProvider 是测试用 Provider 桩。
// 按 Script 预设的序列依次返回响应；记录所有调用参数供断言。
type FakeProvider struct {
	responses []FakeResponse
	calls     []Request
}

// NewFakeProvider 创建空的 FakeProvider，需调用 Script 注入响应。
func NewFakeProvider() *FakeProvider {
	return &FakeProvider{}
}

// Script 设置响应序列。后续 ChatStream 按序消费，耗尽后返回错误。
func (f *FakeProvider) Script(rs []FakeResponse) {
	f.responses = rs
}

// ChatStream 实现 Provider 接口。
func (f *FakeProvider) ChatStream(ctx context.Context, req Request) (*Response, error) {
	f.calls = append(f.calls, req)

	if len(f.calls) > len(f.responses) {
		return nil, errors.New("fake provider: script exhausted")
	}

	resp := f.responses[len(f.calls)-1]

	// 模拟流式推送 delta
	if req.OnDelta != nil {
		delta := resp.DeltaText
		if delta == "" {
			delta = resp.Text
		}
		req.OnDelta(delta)
	}

	return &Response{
		Message: conversation.Message{
			Role:      conversation.RoleAssistant,
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		},
	}, nil
}

// Calls 返回所有记录的请求（按调用顺序）。
func (f *FakeProvider) Calls() []Request {
	return f.calls
}
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/llm/ -v
```

Expected: Task 5 的 3 个 + 本任务的 4 个，共 7 个全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/llm/fake_provider.go internal/llm/fake_provider_test.go
git commit -m "feat(llm): add FakeProvider for Agent Loop testing"
```

---

## Task 8：Agent 结构体与 V1 Loop（禁用工具）

**目标：** 建立 `internal/agent/` 包，定义 `Agent`/`Policy`/`LoopEvent`/`EventSink`，并实现 V1 模式的 Loop——`AllowToolCalls: false`，纯对话，单轮即结束。这是 V2/V3 共用的 Loop 骨架。

**设计依据：** 补丁 §4.1（Policy）、§4.1.1（EventSink）、§4.2（Loop 主流程）。

**Files:**
- Create: `internal/agent/errors.go`
- Create: `internal/agent/policy.go`
- Create: `internal/agent/agent.go`
- Create: `internal/agent/agent_v1_test.go`

- [ ] **Step 1: 定义错误类型**

Create `internal/agent/errors.go`:

```go
package agent

import "errors"

// ErrMaxIterationsReached 在 Loop 达到 MaxIterations 上限仍未结束时返回。
// 通常意味着模型陷入反复调用工具的循环。
var ErrMaxIterationsReached = errors.New("agent: max iterations reached")
```

- [ ] **Step 2: 定义 Policy 与 EventSink**

Create `internal/agent/policy.go`:

```go
// Package agent 实现 Agent 对话循环（Loop）与工具调度。
// Loop 代码只有一份，通过 Policy 控制是否启用工具调用：
//   - V1：AllowToolCalls=false，纯对话，单轮结束
//   - V2：AllowToolCalls=true，模型可建议工具，用户确认后执行
package agent

import "github.com/agentforge/agentforge/internal/conversation"

// Policy 控制 Agent 在一轮思考中如何处理工具调用。
// V1 传禁用工具的 Policy；V2 传需确认的 Policy。Loop 代码不变。
type Policy struct {
	// AllowToolCalls 是否允许 LLM 返回工具调用。
	// false 时：不向 API 传 tools 参数，纯对话模式（V1）。
	AllowToolCalls bool

	// Confirm 工具执行前的确认回调。
	// nil 表示无需确认（自动执行，仅用于 safe 只读工具）。
	// 非 nil 时：返回 false 则跳过该调用，结果标记为"用户拒绝"。
	Confirm func(call conversation.ToolCall) (approved bool, err error)

	// MaxIterations 防止无限循环的硬上限。0 表示默认值 10。
	// 每次工具调用后让 LLM 继续思考算一轮；超限则强制结束。
	MaxIterations int
}

// effectiveMaxIterations 返回生效的 MaxIterations（处理 0 默认值）。
func (p Policy) effectiveMaxIterations() int {
	if p.MaxIterations <= 0 {
		return 10
	}
	return p.MaxIterations
}

// LoopEventKind 标识 Loop 推送的事件类型。
type LoopEventKind int

const (
	// LoopDelta LLM 生成的文本 token，实时渲染给用户。
	LoopDelta LoopEventKind = iota
	// LoopProgress 工具执行进度。
	LoopProgress
	// LoopToolCallStart 开始执行某次工具调用。
	LoopToolCallStart
	// LoopToolCallEnd 某次工具调用结束。
	LoopToolCallEnd
	// LoopInfo 提示信息（如"达到最大迭代数"）。
	LoopInfo
)

// LoopEvent 是 Agent Loop 对外推送的事件。面向不同的"壳"（CLI/GUI）。
type LoopEvent struct {
	Kind     LoopEventKind
	// Text 用于 Delta / Progress / Info 的文本。
	Text string
	// ToolCall 仅 ToolCallStart / ToolCallEnd 时携带。
	ToolCall *conversation.ToolCall
}

// EventSink 由调用方（CLI/GUI）实现，决定如何展示 Loop 过程。
type EventSink func(LoopEvent)

// DeltaEvent 便利构造函数。
func DeltaEvent(text string) LoopEvent {
	return LoopEvent{Kind: LoopDelta, Text: text}
}

// ProgressEvent 便利构造函数。
func ProgressEvent(text string) LoopEvent {
	return LoopEvent{Kind: LoopProgress, Text: text}
}
```

- [ ] **Step 3: 写失败测试 —— V1 纯对话**

Create `internal/agent/agent_v1_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	"github.com/agentforge/agentforge/internal/tool"
)

func TestV1LoopPureChat(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{Text: "你好，有什么可以帮你？"},
	})

	agent := NewAgent(fp, tool.NewRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: false})

	var collected string
	sink := func(ev LoopEvent) {
		if ev.Kind == LoopDelta {
			collected += ev.Text
		}
	}

	err := agent.Run(context.Background(), "hi", sink)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if collected != "你好，有什么可以帮你？" {
		t.Errorf("delta collected %q, want 你好，有什么可以帮你？", collected)
	}

	// V1 不传 tools 给 Provider
	calls := fp.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(calls))
	}
	if len(calls[0].Tools) != 0 {
		t.Errorf("V1 should not pass tools; got %d tools", len(calls[0].Tools))
	}
}

func TestV1LoopStoresHistory(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{{Text: "reply"}})

	mgr := conversation.NewManager()
	agent := NewAgent(fp, tool.NewRegistry(), mgr, Policy{AllowToolCalls: false})

	_ = agent.Run(context.Background(), "user msg", func(LoopEvent) {})

	msgs := mgr.Messages()
	// 期望：user + assistant
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in history, got %d", len(msgs))
	}
	if msgs[0].Role != conversation.RoleUser || msgs[0].Content != "user msg" {
		t.Errorf("unexpected user msg: %+v", msgs[0])
	}
	if msgs[1].Role != conversation.RoleAssistant || msgs[1].Content != "reply" {
		t.Errorf("unexpected assistant msg: %+v", msgs[1])
	}
}

func TestV1LoopPropagatesProviderError(t *testing.T) {
	fp := llm.NewFakeProvider()
	// 不注入 script → 第一次调用就耗尽报错
	agent := NewAgent(fp, tool.NewRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: false})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err == nil {
		t.Fatal("expected error from exhausted provider")
	}
	// 错误应由 Run 包装并向上传递（provider 的 script exhausted 错误）
}
```

- [ ] **Step 4: 运行测试确认失败**

Run:
```bash
go test ./internal/agent/ -v -run TestV1
```

Expected: 编译失败 —— `NewAgent`/`Run` 未定义。

- [ ] **Step 5: 实现 V1 Loop**

Create `internal/agent/agent.go`:

```go
package agent

import (
	"context"
	"fmt"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	"github.com/agentforge/agentforge/internal/tool"
)

// Agent 持有对话运行时状态，执行 Agent Loop。
type Agent struct {
	llm     llm.Provider
	tools   *tool.Registry
	history *conversation.Manager
	policy  Policy
}

// NewAgent 创建 Agent。tools 可为 nil（V1 纯对话时不需要工具）。
func NewAgent(provider llm.Provider, tools *tool.Registry, history *conversation.Manager, policy Policy) *Agent {
	if tools == nil {
		tools = tool.NewRegistry()
	}
	return &Agent{
		llm:     provider,
		tools:   tools,
		history: history,
		policy:  policy,
	}
}

// Run 执行一次用户输入，流式推送事件直到对话结束。
func (a *Agent) Run(ctx context.Context, userInput string, sink EventSink) error {
	a.history.AppendUser(userInput)

	maxIter := a.policy.effectiveMaxIterations()
	for iter := 0; iter < maxIter; iter++ {
		// 1. 准备消息（含可能的压缩）
		msgs := a.history.ForRequest()

		// 2. 决定是否带工具（V1 不带）
		var toolDefs []llm.ToolDef
		if a.policy.AllowToolCalls {
			toolDefs = a.buildToolDefs()
		}

		// 3. 请求 LLM
		resp, err := a.llm.ChatStream(ctx, llm.Request{
			Messages: msgs,
			Tools:    toolDefs,
			OnDelta: func(text string) {
				if sink != nil {
					sink(DeltaEvent(text))
				}
			},
		})
		if err != nil {
			return fmt.Errorf("agent loop iteration %d: %w", iter, err)
		}

		// 4. 记录 assistant 消息
		a.history.AppendAssistant(resp.Message)

		// 5. 无工具调用 → 对话结束（V1 总走这里）
		if len(resp.Message.ToolCalls) == 0 {
			return nil
		}

		// 6. 有工具调用 → 执行（V2 路径，Task 9 实现）
		for _, call := range resp.Message.ToolCalls {
			if err := a.executeToolCall(ctx, call, sink); err != nil {
				return err
			}
		}
	}
	sink(LoopEvent{Kind: LoopInfo, Text: "达到最大迭代数"})
	return ErrMaxIterationsReached
}

// buildToolDefs 从 Registry 构造传给 LLM 的工具定义列表。
func (a *Agent) buildToolDefs() []llm.ToolDef {
	tools := a.tools.List()
	defs := make([]llm.ToolDef, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return defs
}
```

注意：`executeToolCall` 方法在 Task 9 实现。为让 Task 8 编译通过，需要在此文件先放一个桩方法。把以下内容追加到 `agent.go` 末尾（在 Task 9 会替换为真实实现）：

```go
// executeToolCall 桩实现（Task 9 替换为真实逻辑）。
// V1 不会走到这里（AllowToolCalls=false 时模型不会返回 tool_calls）。
func (a *Agent) executeToolCall(ctx context.Context, call conversation.ToolCall, sink EventSink) error {
	return fmt.Errorf("tool execution not implemented (V1)")
}
```

- [ ] **Step 6: 运行测试确认通过**

Run:
```bash
go test ./internal/agent/ -v -run TestV1
```

Expected: 3 个测试全部 PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/agent/
git commit -m "feat(agent): add Agent struct, Policy, EventSink, and V1 pure-chat loop"
```

---

## Task 9：V2 Loop —— 工具调用执行

**目标：** 实现 `executeToolCall`，让 Loop 能处理模型返回的工具调用：查找工具 → Policy 确认 → 流式执行 → 结果回填。完成后 V1/V2 共用同一份 Loop 代码，差异仅在 Policy。

**设计依据：** 补丁 §4.3（工具调用执行）。

**Files:**
- Modify: `internal/agent/agent.go`（替换桩方法为真实实现）
- Create: `internal/agent/agent_v2_test.go`

- [ ] **Step 1: 写失败测试 —— V2 工具调用闭环**

Create `internal/agent/agent_v2_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	"github.com/agentforge/agentforge/internal/tool"
)

// echoTool 是测试用的内联工具，实现 tool.Tool，返回固定结果。
// 用内联定义而非依赖 command 包，让 Task 9 的测试自包含。
type echoTool struct{}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "回声测试工具" }
func (e *echoTool) Schema() []byte      { return []byte(`{"type":"object","properties":{}}`) }

func (e *echoTool) Execute(ctx context.Context, args []byte) (<-chan tool.Event, error) {
	ch := make(chan tool.Event)
	go func() {
		defer close(ch)
		ch <- tool.Event{Kind: tool.EventDelta, Text: "running..."}
		ch <- tool.Event{
			Kind:   tool.EventResult,
			Result: &tool.Result{Content: "echo result"},
		}
	}()
	return ch, nil
}

// newEchoRegistry 返回注册了 echo 工具的 Registry。
func newEchoRegistry() *tool.Registry {
	r := tool.NewRegistry()
	r.Register(&echoTool{})
	return r
}

func TestV2LoopExecutesToolCall(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		// 第一轮：模型决定调工具
		{
			Text: "我来查一下",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "echo", Args: []byte(`{}`)},
			},
		},
		// 第二轮：看到工具结果后，模型给出最终回复
		{Text: "工具返回了结果"},
	})

	agent := NewAgent(fp, newEchoRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: true, MaxIterations: 5})

	var deltas string
	var toolEvents []LoopEvent
	sink := func(ev LoopEvent) {
		if ev.Kind == LoopDelta {
			deltas += ev.Text
		}
		if ev.Kind == LoopToolCallStart || ev.Kind == LoopToolCallEnd {
			toolEvents = append(toolEvents, ev)
		}
	}

	err := agent.Run(context.Background(), "打个招呼", sink)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应收到两段 delta（两轮对话）；工具的 progress 也会通过 LoopProgress 推送但不计入 delta
	if deltas != "我来查一下工具返回了结果" {
		t.Errorf("deltas collected %q", deltas)
	}

	// 应有工具调用事件（start + end）
	if len(toolEvents) < 2 {
		t.Fatalf("expected at least 2 tool events (start+end), got %d", len(toolEvents))
	}

	// Provider 应被调用 2 次（第一轮调工具，第二轮看结果后回复）
	if len(fp.Calls()) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(fp.Calls()))
	}
}

func TestV2LoopConfirmRejected(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "想调工具",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "echo", Args: []byte(`{}`)},
			},
		},
		{Text: "好的，不调了"},
	})

	// Confirm 总是返回 false（用户拒绝）
	rejected := false
	agent := NewAgent(fp, newEchoRegistry(), conversation.NewManager(),
		Policy{
			AllowToolCalls: true,
			Confirm: func(call conversation.ToolCall) (bool, error) {
				rejected = true
				return false, nil
			},
			MaxIterations: 5,
		})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rejected {
		t.Fatal("expected Confirm to be called")
	}
}

func TestV2LoopUnknownTool(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "调个不存在的工具",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "nonexistent_tool", Args: []byte(`{}`)},
			},
		},
		{Text: "好吧，那个工具不存在"},
	})

	agent := NewAgent(fp, tool.NewRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: true, MaxIterations: 5})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err != nil {
		t.Fatalf("unknown tool should not be fatal: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:
```bash
go test ./internal/agent/ -v -run TestV2
```

Expected: 测试失败 —— 桩方法返回 "tool execution not implemented"。

- [ ] **Step 3: 替换 executeToolCall 为真实实现**

Edit `internal/agent/agent.go`：把末尾的桩方法替换为真实实现。

找到这段（Task 8 追加的桩）：
```go
// executeToolCall 桩实现（Task 9 替换为真实逻辑）。
// V1 不会走到这里（AllowToolCalls=false 时模型不会返回 tool_calls）。
func (a *Agent) executeToolCall(ctx context.Context, call conversation.ToolCall, sink EventSink) error {
	return fmt.Errorf("tool execution not implemented (V1)")
}
```

替换为：

```go
// executeToolCall 执行单次工具调用，受 Policy 约束。
// 流程：查找工具 → Confirm 确认 → 流式执行 → 结果回填历史。
// 任何非致命错误（工具不存在、用户拒绝、执行出错）都回填为 tool 消息让 Loop 继续。
func (a *Agent) executeToolCall(ctx context.Context, call conversation.ToolCall, sink EventSink) error {
	if sink != nil {
		sink(LoopEvent{Kind: LoopToolCallStart, ToolCall: &call})
		defer func() {
			sink(LoopEvent{Kind: LoopToolCallEnd, ToolCall: &call})
		}()
	}

	// 1. 查找工具
	t, ok := a.tools.Get(call.Name)
	if !ok {
		a.history.AppendToolResult(call.ID, call.Name, "未知工具: "+call.Name, true)
		return nil
	}

	// 2. Policy 确认（V2 的 ask 层）
	if a.policy.Confirm != nil {
		approved, err := a.policy.Confirm(call)
		if err != nil {
			return fmt.Errorf("confirm %s: %w", call.Name, err)
		}
		if !approved {
			a.history.AppendToolResult(call.ID, call.Name, "用户拒绝执行此工具调用", true)
			return nil
		}
	}

	// 3. 流式执行
	events, err := t.Execute(ctx, call.Args)
	if err != nil {
		a.history.AppendToolResult(call.ID, call.Name, "执行失败: "+err.Error(), true)
		return nil
	}

	// 4. 消费事件流
	for ev := range events {
		switch ev.Kind {
		case tool.EventDelta:
			if sink != nil {
				sink(ProgressEvent(ev.Text))
			}
		case tool.EventProgress:
			if sink != nil {
				sink(ProgressEvent(ev.Text))
			}
		case tool.EventResult:
			if ev.Result != nil {
				a.history.AppendToolResult(call.ID, call.Name, ev.Result.Content, ev.Result.IsError)
			}
		case tool.EventError:
			a.history.AppendToolResult(call.ID, call.Name, ev.Text, true)
		}
	}
	return nil
}
```

同时需要在 `agent.go` 顶部增加 `tool` 包的 import：

```go
import (
	"context"
	"fmt"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	"github.com/agentforge/agentforge/internal/tool"
)
```

- [ ] **Step 4: 运行 V2 测试确认通过**

Run:
```bash
go test ./internal/agent/ -v -run TestV2
```

Expected: 3 个测试全部 PASS。

- [ ] **Step 5: 运行 V1 测试确认未回归**

Run:
```bash
go test ./internal/agent/ -v -run TestV1
```

Expected: 3 个测试仍全部 PASS（证明 V1/V2 共用同一份 Loop，V1 不受 V2 逻辑影响）。

- [ ] **Step 6: 提交**

```bash
git add internal/agent/agent.go internal/agent/agent_v2_test.go
git commit -m "feat(agent): implement tool call execution with Policy confirmation (V2)"
```

---

## Task 10：Command 包与 SystemInfoTool

**目标：** 实现 `internal/command/` 包，把白名单命令封装为实现 `tool.Tool` 接口的工具。采用**结构化传参**（`exec.Command(binary, args...)`）而非 shell 字符串拼接，规避注入风险。

**设计依据：** 补丁 §7（command 包的改造）、原方案 §5（命令执行白名单策略）。

**Files:**
- Create: `internal/command/types.go`
- Create: `internal/command/runner.go`
- Create: `internal/command/runner_test.go`
- Create: `internal/command/tools.go`
- Modify: `internal/tool/register.go`（注册 SystemInfoTool）

- [ ] **Step 1: 定义白名单命令类型**

Create `internal/command/types.go`:

```go
// Package command 实现白名单命令的执行，封装为 tool.Tool。
// 安全策略：使用 exec.Command(binary, args...) 结构化传参，不经过 shell，
// 从根本上规避字符串拼接导致的命令注入。
package command

// CommandSpec 定义一个白名单命令。
type CommandSpec struct {
	// Title 展示名（给人看）。
	Title string
	// Binary 可执行文件路径或名称（由 exec.LookPath 解析）。
	Binary string
	// Args 固定参数模板（V1 不支持动态参数位）。
	Args []string
}
```

- [ ] **Step 2: 写失败测试 —— Runner 执行与流式输出**

Create `internal/command/runner_test.go`:

```go
package command

import (
	"context"
	"runtime"
	"testing"

	"github.com/agentforge/agentforge/internal/tool"
)

func TestRunEchoCommand(t *testing.T) {
	var spec CommandSpec
	if runtime.GOOS == "windows" {
		spec = CommandSpec{Title: "echo", Binary: "cmd", Args: []string{"/c", "echo hello"}}
	} else {
		spec = CommandSpec{Title: "echo", Binary: "echo", Args: []string{"hello"}}
	}

	runner := NewRunner()
	var deltas []string
	var resultContent string
	var resultIsError bool

	events, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	for ev := range events {
		switch ev.Kind {
		case tool.EventDelta:
			deltas = append(deltas, ev.Text)
		case tool.EventResult:
			if ev.Result != nil {
				resultContent = ev.Result.Content
				resultIsError = ev.Result.IsError
			}
		}
	}

	if len(deltas) == 0 {
		t.Fatal("expected at least one delta line")
	}
	if resultContent == "" {
		t.Fatal("expected non-empty result content")
	}
	if resultIsError {
		t.Error("expected successful result, got error")
	}
}

func TestRunFailingCommand(t *testing.T) {
	// 调用一个不存在的二进制 → 应得到 error 结果
	spec := CommandSpec{Title: "bad", Binary: "this-binary-does-not-exist-xyz", Args: nil}

	runner := NewRunner()
	var resultContent string
	var resultIsError bool

	events, err := runner.Run(context.Background(), spec)
	// Run 本身不返回 error（启动失败也会通过事件流报），但保险起见处理两种情况
	if err == nil {
		for ev := range events {
			if ev.Kind == tool.EventResult && ev.Result != nil {
				resultContent = ev.Result.Content
				resultIsError = ev.Result.IsError
			}
		}
	}

	if !resultIsError {
		t.Error("expected error result for nonexistent binary")
	}
	if resultContent == "" {
		t.Error("expected non-empty error content")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run:
```bash
go test ./internal/command/ -v
```

Expected: 编译失败 —— `NewRunner`/`Run` 未定义。

- [ ] **Step 4: 实现 Runner**

Create `internal/command/runner.go`:

```go
package command

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"

	"github.com/agentforge/agentforge/internal/tool"
)

// Runner 执行白名单命令并把输出转为 tool.Event 流。
type Runner struct{}

// NewRunner 创建 Runner。
func NewRunner() *Runner {
	return &Runner{}
}

// Run 执行一个命令规格，返回事件流。
// 使用 exec.Command(binary, args...) 结构化传参，不经过 shell。
// stdout/stderr 逐行作为 EventDelta 推送，最终汇总为 EventResult。
func (r *Runner) Run(ctx context.Context, spec CommandSpec) (<-chan tool.Event, error) {
	ch := make(chan tool.Event)

	go func() {
		defer close(ch)

		cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)

		// 分别捕获 stdout 和 stderr
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "启动失败(stdout pipe): " + err.Error(),
				IsError: true,
			}}
			return
		}
		stderrBuf := &bytes.Buffer{}
		cmd.Stderr = stderrBuf

		if err := cmd.Start(); err != nil {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "启动失败: " + err.Error(),
				IsError: true,
			}}
			return
		}

		// 逐行读 stdout 推送
		scanner := bufio.NewScanner(stdoutPipe)
		var collected bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			ch <- tool.Event{Kind: tool.EventDelta, Text: line}
			collected.WriteString(line + "\n")
		}

		if err := cmd.Wait(); err != nil {
			errText := err.Error()
			if stderrBuf.Len() > 0 {
				errText += "\nstderr: " + stderrBuf.String()
			}
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: errText,
				IsError: true,
			}}
			return
		}

		ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
			Content: collected.String(),
		}}
	}()

	return ch, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

Run:
```bash
go test ./internal/command/ -v
```

Expected: 2 个测试全部 PASS。

- [ ] **Step 6: 实现 SystemInfoTool（tool.Tool 接口）**

Create `internal/command/tools.go`:

```go
package command

import (
	"context"
	"runtime"

	"github.com/agentforge/agentforge/internal/tool"
)

// SystemInfoTool 获取系统信息，实现 tool.Tool。
// 按平台选择命令：Windows 用 PowerShell，其他用 uname/uname -a。
type SystemInfoTool struct {
	runner *Runner
}

// NewSystemInfoTool 创建 SystemInfoTool。
func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{runner: NewRunner()}
}

func (t *SystemInfoTool) Name() string        { return "get_system_info" }
func (t *SystemInfoTool) Description() string { return "获取系统信息（OS、运行时）" }

func (t *SystemInfoTool) Schema() []byte {
	// 无参数工具
	return []byte(`{"type":"object","properties":{}}`)
}

func (t *SystemInfoTool) Execute(ctx context.Context, args []byte) (<-chan tool.Event, error) {
	spec := t.platformSpec()
	return t.runner.Run(ctx, spec)
}

// platformSpec 返回当前平台的命令规格。
// 用结构化传参（Binary + 固定 Args），不经 shell，规避注入。
func (t *SystemInfoTool) platformSpec() CommandSpec {
	if runtime.GOOS == "windows" {
		// PowerShell 输出 OS 信息
		return CommandSpec{
			Title:  "系统信息",
			Binary: "powershell.exe",
			Args:   []string{"-NoProfile", "-Command", "Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,OSArchitecture | Format-List"},
		}
	}
	// macOS / Linux
	return CommandSpec{
		Title:  "系统信息",
		Binary: "uname",
		Args:   []string{"-a"},
	}
}
```

- [ ] **Step 7: 创建 registry 包（集中注册，打破循环依赖）**

⚠️ **循环依赖说明**：`command` 包实现 `tool.Tool` 接口，故 `command` import `tool`。如果 `tool` 包内直接注册 `command.SystemInfoTool`，则 `tool` 又要 import `command` → 形成循环依赖。

**解决方案**：单独建一个 `registry` 包，它同时 import `tool` 和 `command`，负责装配。这样 `tool` 永远不依赖 `command`，循环被打破。

Create `internal/registry/registry.go`:

```go
// Package registry 集中注册所有内置工具。
// 单独成包是为了打破 tool ↔ command 的循环依赖：
// tool 定义接口，command 实现接口（依赖 tool），registry 依赖两者来装配。
package registry

import (
	"github.com/agentforge/agentforge/internal/command"
	"github.com/agentforge/agentforge/internal/tool"
)

// Setup 把所有内置工具注册到给定 Registry。
func Setup(r *tool.Registry) {
	r.Register(command.NewSystemInfoTool())
	// 未来工具在此追加：
	// r.Register(file.NewReadFileTool())
	// r.Register(file.NewWriteFileTool())
}

// Default 返回已注册好内置工具的全局 Registry。
func Default() *tool.Registry {
	r := tool.NewRegistry()
	Setup(r)
	return r
}
```

Create `internal/registry/registry_test.go`:

```go
package registry

import "testing"

func TestSetupRegistersSystemInfo(t *testing.T) {
	r := Default()
	if _, ok := r.Get("get_system_info"); !ok {
		t.Fatal("expected get_system_info tool registered")
	}
	if len(r.List()) < 1 {
		t.Fatal("expected at least 1 builtin tool")
	}
}
```

- [ ] **Step 8: 运行全部测试**

Run:
```bash
go test ./...
```

Expected: 所有包测试全部 PASS。包括：
- `internal/tool/`（Task 2 的 3 个）
- `internal/conversation/`（Task 3+6 的 8 个）
- `internal/llm/`（Task 5+7 的 7 个）
- `internal/agent/`（Task 8+9 的 6 个）
- `internal/command/`（Task 10 的 2 个）
- `internal/registry/`（Task 10 的 1 个）

- [ ] **Step 9: 提交**

```bash
git add internal/command/ internal/registry/
git commit -m "feat(command): add whitelist command runner and SystemInfoTool; add registry package"
```

---

## Task 11：集成验收测试

**目标：** 写一个端到端集成测试，验证整个 Agent 核心引擎的完整闭环：用户输入 → LLM 决策 → 工具调用 → 结果回填 → LLM 最终回复。使用 FakeProvider + 真实 SystemInfoTool。

**设计依据：** 补丁 §9（验收标准）。

**Files:**
- Create: `internal/agent/integration_test.go`

- [ ] **Step 1: 写集成测试**

Create `internal/agent/integration_test.go`:

```go
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	registrypkg "github.com/agentforge/agentforge/internal/registry"
)

// TestIntegrationEndToEnd 验证完整 Agent 闭环：
// 用户提问 → 模型决定调用 get_system_info → 工具执行 → 结果回填 → 模型总结。
func TestIntegrationEndToEnd(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		// 第一轮：模型决定调工具
		{
			Text: "我来查一下系统信息",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "get_system_info", Args: []byte(`{}`)},
			},
		},
		// 第二轮：基于工具结果给出最终回复
		{Text: "已获取系统信息，分析完成"},
	})

	registry := registrypkg.Default()
	agent := NewAgent(fp, registry, conversation.NewManager(),
		Policy{AllowToolCalls: true, MaxIterations: 5})

	var deltas []string
	var toolCallCount int
	sink := func(ev LoopEvent) {
		switch ev.Kind {
		case LoopDelta:
			deltas = append(deltas, ev.Text)
		case LoopToolCallStart:
			toolCallCount++
		}
	}

	err := agent.Run(context.Background(), "帮我看看系统信息", sink)
	if err != nil {
		t.Fatalf("end-to-end failed: %v", err)
	}

	// 验收点 1：工具被调用一次
	if toolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", toolCallCount)
	}

	// 验收点 2：Provider 被调用两次（调工具 + 总结）
	if len(fp.Calls()) != 2 {
		t.Errorf("expected 2 provider calls, got %d", len(fp.Calls()))
	}

	// 验收点 3：第一轮传了 tools（AllowToolCalls=true）
	if len(fp.Calls()[0].Tools) == 0 {
		t.Error("first call should pass tools to provider")
	}

	// 验收点 4：两轮 delta 都收到了
	allDeltas := strings.Join(deltas, "")
	if !strings.Contains(allDeltas, "我来查一下") {
		t.Errorf("missing first delta; got %q", allDeltas)
	}
	if !strings.Contains(allDeltas, "已获取系统信息") {
		t.Errorf("missing final delta; got %q", allDeltas)
	}
}
```

- [ ] **Step 2: 运行集成测试**

Run:
```bash
go test ./internal/agent/ -v -run TestIntegration
```

Expected: PASS。

- [ ] **Step 3: 运行全量测试确认无回归**

Run:
```bash
go test ./...
```

Expected: 所有包全部 PASS。

- [ ] **Step 4: 验证整体编译**

Run:
```bash
go build ./...
```

Expected: 无输出（成功）。

- [ ] **Step 5: 提交**

```bash
git add internal/agent/integration_test.go
git commit -m "test(agent): add end-to-end integration test for full agent loop"
```

---

## 完成后的状态

实施完成后，Agent 核心引擎具备：

| 能力 | 对应验收标准（补丁 §9） | 验证任务 |
|------|----------------------|---------|
| Tool 接口可被实现 | `tool.Tool` 接口 + command 实现 | Task 10 |
| Registry 注册/查找/列举 | Registry 基本行为 | Task 2、Task 10 |
| 流式 Execute 推送 | SystemInfoTool 的 delta 流 | Task 10 |
| V1 Loop 等价纯聊天 | `AllowToolCalls: false` 单轮结束 | Task 8、Task 11 |
| V2 Loop 完整闭环 | tool_calls → 确认 → 执行 → 回填 | Task 9、Task 11 |
| 流式 tool_calls 累积 | accumulator 按 index 拼装 | Task 5 |
| Context 压缩不破坏配对 | 压缩后 tool 消息有配对 call | Task 6 |

## 不在本计划范围内（明确排除）

- 具体的 OpenAI Provider 实现（仅有接口 + Fake）→ 下一份计划
- CLI 入口 `cmd/cli/main.go` → 下一份计划
- Wails GUI 与前端 → 独立计划
- npm wrapper 与发布流程 → 独立计划
- 权限分级模型（safe/ask/deny）→ 独立补丁
- 命令注入防护的进阶策略（本计划用结构化传参已规避主要风险）

下一步建议：完成本计划后，用 brainstorming 设计 "OpenAI Provider 实现 + CLI 入口" 计划，让 Agent 能真正跑起来。
