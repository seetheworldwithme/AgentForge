# AgentForge 总体实施计划 (plan.md)

> **本文档是 AgentForge 项目的唯一权威实施计划**，由以下文档合并而成：
> - `agent-desktop-demo-技术栈与实现方案.md`（项目总体技术方案 / 背景）
> - `2026-06-15-tool-abstraction-and-agent-loop-plan.md`（Tool 抽象层 + Agent Loop，原 Task 1-11）
> - `rag.md`（RAG 功能设计 spec）
> - `rag-plan.md`（RAG 实施计划，原 Task 1-28）
>
> **任务编号已全局重排，从 T1 连续递增到 T39**，消除原两份计划的编号冲突。
>
> **执行顺序：** 严格按 T1 → T39 顺序执行。每个 Task 都是 TDD 五步循环（写失败测试 → 确认失败 → 实现 → 确认通过 → 提交）。每个里程碑（Phase）结束有检查点。

---

## 目录

- [第一部分：项目总体方案](#第一部分项目总体方案)
  - [1. 项目定位与核心能力](#1-项目定位与核心能力)
  - [2. 技术栈定稿](#2-技术栈定稿)
  - [3. 双模式架构](#3-双模式架构)
  - [4. 应用模块划分](#4-应用模块划分)
  - [5. 大模型接入](#5-大模型接入)
  - [6. 命令执行白名单策略](#6-命令执行白名单策略)
  - [7. 演进路径 V1→V3](#7-演进路径-v1v3)
  - [8. 最终目录结构](#8-最终目录结构)
  - [9. 依赖选择](#9-依赖选择)
  - [10. 注意事项（安全/跨平台/架构）](#10-注意事项)
- [第二部分：Tool 抽象层 + Agent Loop（Phase 1，T1-T11）](#第二部分tool-抽象层--agent-loop)
- [第三部分：RAG 功能设计 spec](#第三部分rag-功能设计-spec)
- [第四部分：RAG 实施计划（Phase 2，T12-T39）](#第四部分rag-实施计划)
- [全局里程碑与检查点总览](#全局里程碑与检查点总览)
- [全局风险登记簿](#全局风险登记簿)

---

# 第一部分：项目总体方案

## 1. 项目定位与核心能力

项目名称：**AgentForge**

定位：一个跨平台的智能 Agent 工具，支持 CLI 和桌面 GUI 双模式使用。

核心能力：
1. 接入 OpenAI 兼容的大模型 API 做对话
2. 在本机执行命令：Windows 用 PowerShell、macOS/Linux 用 bash，并且默认采用「白名单命令」策略
3. **个人知识库 RAG**：导入用户文档（Markdown/PDF/Office），基于资料回答问题

双模式分发：
- **CLI 模式**：通过 `npm install -g agentforge` 安装，终端命令行使用（类似 Claude Code）
- **GUI 模式**：打包为桌面客户端安装包，图形化界面使用

成功标准：
- CLI 模式：能通过 `agentforge chat "你好"` 发起对话，`agentforge run <command>` 执行白名单命令
- GUI 模式：能通过桌面窗口完成上述所有操作，带流式输出展示
- 默认安全：不允许任意命令；不记录/不输出 api_key

---

## 2. 技术栈定稿

**Wails v2 + Go + React + TS + Vite**

- **Why**：Go 天然擅长并发、流式处理与系统级调用；Wails 使用系统原生 WebView 不打包 Chromium，安装包体积小（~10-20MB vs Electron ~80-150MB）；前后端通过 Go binding 直接通信，无 IPC 序列化开销。
- **Tradeoff**：Wails 社区比 Electron 小；Windows 上依赖 WebView2 Runtime（Win10/11 已预装，Win7/8 需额外安装）。

**运行时/版本建议**
- Go：1.22+（或最新稳定版）
- Wails：v2（wails CLI 脚手架）
- Node.js：20 LTS（仅前端构建用）
- 包管理：Go Modules + npm/pnpm（前端依赖）

---

## 3. 双模式架构

核心理念：**Go 业务逻辑只写一份，CLI 和 GUI 只是两个不同的「壳」。**

```
                    ┌─────────────────────────┐
                    │     internal/ (Go)       │  ← 共享业务逻辑
                    │     ├── llm/             │
                    │     ├── command/         │
                    │     ├── storage/         │
                    │     ├── conversation/    │
                    │     ├── tool/            │
                    │     ├── agent/           │
                    │     └── rag/             │
                    └───────────┬─────────────┘
                                │
                    ┌───────────┴───────────┐
                    │                       │
              ┌─────▼──────┐         ┌──────▼──────┐
              │  CLI 入口    │         │  Wails GUI  │
              │  cmd/cli/   │         │  cmd/gui/   │
              └──────┬──────┘         └─────────────┘
                     │
              ┌──────▼──────┐
              │  npm wrapper │
              └─────────────┘
```

**两种模式的功能对比**

| 功能 | CLI 模式 | GUI 模式 |
|------|---------|---------|
| 对话（LLM） | ✅ 终端流式输出 | ✅ 窗口流式渲染 |
| 命令执行 | ✅ 直接在终端看输出 | ✅ 窗口内嵌终端区 |
| Settings 配置 | ⚠️ 命令行参数 / 配置文件 | ✅ 图形化设置页面 |
| 会话历史 | ⚠️ 本地文件存储 | ✅ 本地存储 + 可视化浏览 |
| 工具调用链 | ✅ 终端展示过程 | ✅ 可视化展示 DAG |
| 安全存储 | ⚠️ 文件加密 | ✅ 系统 Keychain |
| RAG 知识库 | ⚠️ 命令行导入 | ✅ 图形化导入 + 评测页面 |

---

## 4. 应用模块划分

**Frontend（前端 UI — React + TS + Vite，GUI 模式专用）**
- Chat 页面：消息列表 + 输入框 + 发送按钮 + 流式输出展示（SSE 逐 token 渲染）
- Settings 页面：base_url / api_key / model / 允许的命令集开关
- Command 页面：选择白名单命令并执行，展示实时输出
- KnowledgeBase 页面：知识库管理 + 文档导入 + 检索测试
- Evaluation 页面：召回评测，对话式测试 + 指标面板

**Backend（Go 后端 — 共享核心引擎）**
- LLMClient：负责请求 OpenAI 兼容 API（优先流式 SSE，支持非流式降级）
- CommandRunner：命令白名单校验 + 组装平台命令 + os/exec 执行 + 输出流实时转发
- SecureStorage：保存 api_key（优先系统 Keychain；退化到本地加密文件）
- ConversationStore：保存会话历史（SQLite 或 JSON 文件）
- Agent Loop：对话循环 + 工具调用调度（V1 纯对话 / V2 工具确认 / V3 编排）
- RAG Engine：文档切片 + embedding + 向量检索 + 评测

**CLI 层（Go — cmd/cli/）**
- 命令行参数解析（标准库 flag 或 cobra）
- 终端输出格式化（带颜色、进度条）
- 交互模式（readline 类似体验）

**Binding 层（Wails 自动生成，GUI 模式专用）**
- Wails 通过 Go struct tag 自动将 Go 方法暴露给前端
- 前端通过 `window.go.main.App.MethodName()` 直接调用 Go 函数
- 支持返回值、错误、以及 Events 机制（Go 端 EventsEmit → 前端监听）

---

## 5. 大模型接入

**配置项**
- base_url：例如 https://api.openai.com/v1 或任意兼容网关
- api_key：仅存储在系统安全存储中；不写日志；不通过 query string
- model：如 gpt-4o-mini / deepseek-chat 等（由用户输入）

**接口形态**
- 默认使用 Chat Completions API（流式 SSE）
- Go 端使用 net/http + bufio.Scanner 逐行解析 SSE 事件
- GUI 模式：通过 Wails EventsEmit 将每个 token/chunk 实时推送到前端
- CLI 模式：直接将 chunk 写入 stdout

**建议的错误处理**
- 401：提示 api_key 无效
- 429：提示限流并建议重试
- 网络错误：提示检查 base_url/代理

---

## 6. 命令执行白名单策略

**白名单的最小形态（建议）**
- 只允许预设命令 ID（而不是用户任意输入整段命令）
- 每个命令 ID 映射到：title / platform / shell / argsTemplate

**安全基线（默认）**
- 不允许白名单外命令
- 采用 `exec.Command(binary, args...)` 结构化传参，不经 shell，从根本上规避命令注入
- 不允许带重定向/管道等高风险语法
- 可选增强：在 UI 增加「执行确认」弹窗

---

## 7. 演进路径 V1→V3

**V1 阶段（纯对话）**
- Chat：用户问答（LLM）
- Tools：用户手动触发运行白名单命令（不让模型直接触发执行）
- Loop 中 `AllowToolCalls: false`

**V2 演进（模型建议 + 用户确认）**
- 模型通过 function calling 返回「工具调用请求」
- GUI 弹出确认框 / CLI 要求用户输入 y/n
- 用户同意后由 Go 端执行，执行结果回传给模型继续对话

**V3 演进（复杂工具调用链）**
- 多步骤编排：模型一次返回多个工具调用 → Go 端调度执行（支持并行）→ 结果聚合 → 继续对话
- Go 端 ToolChain 引擎：基于 goroutine + channel 的并行执行、context 超时控制、工具间依赖图（DAG）编排

---

## 8. 最终目录结构

```
agent/                           # 项目根目录
├── cmd/
│   ├── cli/                     # CLI 入口
│   │   └── main.go              # 编译为 agentforge 二进制
│   └── gui/                     # GUI 入口
│       ├── main.go              # Wails 启动入口
│       └── app.go               # Wails binding 方法
│
├── internal/                    # Go 共享业务逻辑（核心引擎）
│   ├── tool/                    # Tool 统一抽象（接口、Registry）
│   ├── conversation/            # 消息模型 + Manager + Context 压缩
│   ├── llm/                     # Provider 接口 + 流式 tool_calls 累积
│   ├── agent/                   # Agent Loop + Policy
│   ├── command/                 # 白名单命令执行（实现 tool.Tool）
│   ├── registry/                # 集中注册（打破 tool↔command 循环）
│   ├── storage/                 # 安全存储 + 配置
│   ├── rag/                     # ★ RAG 引擎
│   │   ├── chunker/             # 切片器
│   │   ├── embedder/            # embedding 调用
│   │   ├── store/               # sqlite-vec 存储 + 检索
│   │   ├── eval/                # 评测（metrics / judge / generator）
│   │   ├── retrieval.go         # 检索编排
│   │   ├── pipeline.go          # 导入编排
│   │   ├── prompt.go            # prompt 组装
│   │   ├── service.go           # 高层 Service 门面
│   │   └── types.go             # 数据结构
│   └── toolchain/               # 工具链引擎（V3 演进用）
│
├── frontend/                    # GUI 专用前端（React + TS + Vite）
│   ├── src/
│   │   ├── components/
│   │   ├── pages/               # Chat / Settings / Command / KnowledgeBase / Evaluation
│   │   ├── hooks/
│   │   ├── api/                 # Wails binding 调用封装
│   │   ├── types/
│   │   ├── App.tsx
│   │   └── main.tsx
│   └── ...
│
├── packages/
│   └── agentforge/              # npm 分发包（CLI wrapper）
│
├── build/                       # Wails 构建产物与图标资源
├── scripts/                     # 构建 & 发布脚本
├── go.mod
├── wails.json
└── Makefile
```

---

## 9. 依赖选择

**前端（GUI 专用）**
- React + Vite + TypeScript
- Markdown 渲染：react-markdown（仅展示，禁用危险 HTML）
- 代码高亮：highlight.js 或 prismjs

**Go 后端（共享）**
- HTTP 请求：net/http（标准库）
- JSON：encoding/json（标准库）
- SSE 解析：bufio.Scanner 手动解析（标准库）
- 安全存储：github.com/zalando/go-keyring（系统 Keychain）
- 配置管理：自定义 JSON 文件 或 viper
- CLI 框架：github.com/spf13/cobra
- SQLite：`modernc.org/sqlite`（纯 Go 无 CGO）+ sqlite-vec 向量扩展
- PDF 解析：`ledongthuc/pdf`（纯 Go）
- Office 解析：标准库 `archive/zip` + `encoding/xml`

说明：第一版以「最少依赖能跑」为准，优先使用 Go 标准库，后续按需引入。

---

## 10. 注意事项

### 安全
- **api_key 保护**：不写入日志、不出现在进程参数、不通过 URL query string、GUI 优先 Keychain / CLI 本地加密文件
- **命令执行安全**：白名单 + 结构化传参 + 执行确认（V2） + context 超时
- **npm 包安全**：postinstall 校验 checksum、从 GitHub Releases 下载

### 跨平台
- **Windows WebView2**：Win10/11 预装，Win7/8 需提示安装
- **Shell 差异**：Windows 用 PowerShell，macOS/Linux 用 bash，白名单按平台分别定义
- **路径处理**：用 `os.UserConfigDir()` + `filepath.Join()`，不硬编码分隔符
- **编码问题**：Windows 中文 PowerShell 输出可能 GBK，需转 UTF-8

### 架构
- **GUI 和 CLI 功能同步**：新功能先在 `internal/` 实现，CLI/GUI 只是调用层
- **internal/ 约束**：只能被父目录下代码导入，保证封装
- **CGO 避免**：V1 不引入 CGO 依赖，交叉编译干净（故选 modernc.org/sqlite 而非 go-sqlite3）

---

# 第二部分：Tool 抽象层 + Agent Loop

> **范围：** Phase 1，任务 T1-T11。在 `internal/` 下建立 Agent 核心引擎——Tool 统一抽象、流式 Agent Loop、消息模型与 Context 压缩——为 CLI/GUI 双模式提供共享业务逻辑。

**Architecture:** Go 共享核心引擎位于 `internal/`，自底向上分层构建：`tool/`（工具接口）→ `conversation/`（消息模型）→ `llm/`（Provider 接口与流式累积）→ `agent/`（Loop + Policy）。每个包有清晰单一职责，通过接口通信，可独立测试。`command/` 包的白名单命令实现 `tool.Tool` 接口。

**Tech Stack:** Go 1.22+、标准库 `net/http`、`encoding/json`、`os/exec`、`context`、`sync`；测试用标准库 `testing` + `net/http/httptest`。第一版零第三方依赖。

---

## T1：初始化 Go Module

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

## T2：Tool 抽象层与 Registry

**目标：** 建立 `internal/tool/` 包，定义 `Tool` 接口、`Event`/`Result` 事件模型与集中式 `Registry`。这是整个架构的地基。

**Files:**
- Create: `internal/tool/types.go`
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

// List 返回所有已注册工具的切片（顺序不保证）。
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

## T3：消息模型与 Manager 基础

**目标：** 建立 `internal/conversation/` 包，定义 `Message`/`Role`/`ToolCall` 类型与 `Manager` 的基础增删查方法（不含压缩，压缩在 T6）。

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
	ToolCallID string     `json:"tool_call_id,omitempty"` // 仅 RoleTool 使用
	Name       string     `json:"name,omitempty"`         // 仅 RoleTool 使用
}
```

- [ ] **Step 2: 创建 Manager 基础方法**

Create `internal/conversation/manager.go`:

```go
package conversation

// Manager 负责消息存储与（T6 加入的）context 压缩。
type Manager struct {
	messages []Message
}

// NewManager 创建空 Manager。
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
// T6 会在此加入压缩逻辑；当前直接返回全量。
func (m *Manager) ForRequest() []Message {
	if m.maxTokens == 0 {
		return m.messages
	}
	if estimateMessagesTokens(m.messages) <= m.maxTokens {
		return m.messages
	}
	return m.compress()
}
```

> 注：`option` 类型、`maxTokens` 字段、`estimateMessagesTokens`、`compress` 方法在 T6 加入。当前 T3 的 manager.go 先不含 `compress` 调用——ForRequest 直接 `return m.messages`。T6 会修改 ForRequest 加入压缩分支。为避免 T3 编译问题，T3 的 ForRequest 简化为直接返回 m.messages，T6 再替换。

**T3 实际的 ForRequest（简化版，T6 会替换）：**
```go
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

## T4：LLM Provider 接口

**目标：** 建立 `internal/llm/` 包，定义 `Provider` 接口、`Request`/`Response`/`ToolDef` 类型。本任务只定义接口，不实现具体 Provider——实现是后续阶段的事，Agent Loop 测试用 fake。

**Files:**
- Create: `internal/llm/provider.go`

- [ ] **Step 1: 定义 Provider 接口与请求/响应类型**

Create `internal/llm/provider.go`:

```go
// Package llm 定义大模型 Provider 的抽象接口。
package llm

import (
	"context"

	"github.com/agentforge/agentforge/internal/conversation"
)

// Provider 是大模型后端的统一接口。
type Provider interface {
	// ChatStream 发起一次流式对话请求。
	// req.Tools 为空时表示纯对话模式（不启用 function calling）。
	// req.OnDelta 在每个文本 token 到达时被调用。
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
	// Messages 发给模型的消息序列。
	Messages []conversation.Message
	// Tools 暴露给模型的工具定义。nil 或空 = 不启用 function calling。
	Tools []ToolDef
	// OnDelta 流式文本 token 回调。
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

## T5：流式 tool_calls 增量累积器

**目标：** 实现 OpenAI SSE 流式响应中 `tool_calls` 分片 delta 的累积器。tool_calls 是按 index 分片到达的，需要拼装。

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

type deltaChunk struct {
	Index         int
	ID            string
	FunctionName  string
	ArgumentsFrag string
}

func TestAccumulatorSingleCall(t *testing.T) {
	acc := newToolCallAccumulator()
	acc.add(deltaChunk{Index: 0, ID: "call_1", FunctionName: "get_system_info", ArgumentsFrag: `{"query":"`})
	acc.add(deltaChunk{Index: 0, ArgumentsFrag: `memory"`})
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

type deltaChunk struct {
	Index         int
	ID            string
	FunctionName  string
	ArgumentsFrag string
}

type toolCallAccumulator struct {
	calls map[int]*conversation.ToolCall
	order []int
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{calls: make(map[int]*conversation.ToolCall)}
}

func (a *toolCallAccumulator) add(d deltaChunk) {
	call, exists := a.calls[d.Index]
	if !exists {
		call = &conversation.ToolCall{}
		a.calls[d.Index] = call
		a.order = append(a.order, d.Index)
	}
	if d.ID != "" {
		call.ID = d.ID
	}
	if d.FunctionName != "" {
		call.Name = d.FunctionName
	}
	if d.ArgumentsFrag != "" {
		call.Args = append(call.Args, []byte(d.ArgumentsFrag)...)
	}
}

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
		_ = json.Valid(call.Args)
		result = append(result, expectedCall{
			ID:   call.ID,
			Name: call.Name,
			Args: string(call.Args),
		})
	}
	return result
}

type expectedCall struct {
	ID   string
	Name string
	Args string
}
```

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

## T6：Context 摘要压缩

**目标：** 为 `conversation.Manager` 增加摘要压缩能力：超过 token 阈值时，把旧消息按安全边界切分压缩成摘要。**关键约束：不能切断 tool_call ↔ tool_result 的配对**，否则 OpenAI API 返回 400。

**Files:**
- Create: `internal/conversation/compress.go`
- Create: `internal/conversation/compress_test.go`
- Modify: `internal/conversation/manager.go`

- [ ] **Step 1: 写失败测试 —— 压缩触发与边界安全**

Create `internal/conversation/compress_test.go`:

```go
package conversation

import "testing"

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
	m.AppendUser("短消息")

	_ = m.ForRequest()
	if stub.calls != 0 {
		t.Fatalf("summarizer should not be called under threshold; calls=%d", stub.calls)
	}
}

func TestCompressTriggeredOverThreshold(t *testing.T) {
	stub := &stubSummarizer{returns: "[摘要]"}
	m := NewManager(WithSummarizer(stub), WithMaxTokens(5))
	m.AppendUser("第一轮长消息用来触发压缩")
	m.AppendAssistant(Message{Content: "第一轮回复也很长"})
	m.AppendUser("第二轮保留近期")

	msgs := m.ForRequest()
	if stub.calls == 0 {
		t.Fatal("summarizer should be called over threshold")
	}
	if len(msgs) == 0 {
		t.Fatal("expected non-empty messages after compress")
	}
	if msgs[0].Role != RoleSystem {
		t.Errorf("expected first message to be system summary, got %s", msgs[0].Role)
	}
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

	m.AppendUser("第一轮")
	m.AppendAssistant(Message{
		ToolCalls: []ToolCall{{ID: "call_1", Name: "x"}},
	})
	m.AppendUser("第二轮")
	m.AppendToolResult("call_1", "x", "result", false)

	msgs := m.ForRequest()

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
	tests := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world!", 3},
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
```

- [ ] **Step 4: 修改 manager.go 加入压缩逻辑**

替换 `internal/conversation/manager.go` 全部内容：

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

func (m *Manager) AppendSystem(content string) {
	m.messages = append(m.messages, Message{Role: RoleSystem, Content: content})
}

func (m *Manager) AppendUser(content string) {
	m.messages = append(m.messages, Message{Role: RoleUser, Content: content})
}

func (m *Manager) AppendAssistant(msg Message) {
	msg.Role = RoleAssistant
	m.messages = append(m.messages, msg)
}

func (m *Manager) AppendToolResult(toolCallID, toolName, content string, isError bool) {
	m.messages = append(m.messages, Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		Name:       toolName,
	})
}

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
```

- [ ] **Step 5: 运行全部 conversation 测试**

Run:
```bash
go test ./internal/conversation/ -v
```

Expected: T3 的 4 个 + T6 的 4 个，共 8 个全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/conversation/
git commit -m "feat(conversation): add summary compression with tool_call pairing safety"
```

---

## T7：测试用 Fake Provider

**目标：** 创建供 Agent Loop 测试用的 `Provider` 桩实现。支持脚本化：按预设序列返回 Response，记录调用参数便于断言。

**Files:**
- Create: `internal/llm/fake_provider.go`
- Create: `internal/llm/fake_provider_test.go`

- [ ] **Step 1: 写失败测试**

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

Expected: 编译失败 —— `FakeProvider` 未定义。

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
	Text      string
	DeltaText string
	ToolCalls []conversation.ToolCall
}

// FakeProvider 是测试用 Provider 桩。
type FakeProvider struct {
	responses []FakeResponse
	calls     []Request
}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{}
}

func (f *FakeProvider) Script(rs []FakeResponse) {
	f.responses = rs
}

func (f *FakeProvider) ChatStream(ctx context.Context, req Request) (*Response, error) {
	f.calls = append(f.calls, req)

	if len(f.calls) > len(f.responses) {
		return nil, errors.New("fake provider: script exhausted")
	}

	resp := f.responses[len(f.calls)-1]

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

func (f *FakeProvider) Calls() []Request {
	return f.calls
}
```

- [ ] **Step 4: 运行测试确认通过**

Run:
```bash
go test ./internal/llm/ -v
```

Expected: T5 的 3 个 + T7 的 4 个，共 7 个全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/llm/fake_provider.go internal/llm/fake_provider_test.go
git commit -m "feat(llm): add FakeProvider for Agent Loop testing"
```

---

## T8：Agent 结构体与 V1 Loop（禁用工具）

**目标：** 建立 `internal/agent/` 包，定义 `Agent`/`Policy`/`LoopEvent`/`EventSink`，实现 V1 模式的 Loop——`AllowToolCalls: false`，纯对话，单轮结束。

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
var ErrMaxIterationsReached = errors.New("agent: max iterations reached")
```

- [ ] **Step 2: 定义 Policy 与 EventSink**

Create `internal/agent/policy.go`:

```go
// Package agent 实现 Agent 对话循环（Loop）与工具调度。
package agent

import "github.com/agentforge/agentforge/internal/conversation"

// Policy 控制 Agent 在一轮思考中如何处理工具调用。
type Policy struct {
	// AllowToolCalls 是否允许 LLM 返回工具调用。
	AllowToolCalls bool
	// Confirm 工具执行前的确认回调。nil 表示无需确认。
	Confirm func(call conversation.ToolCall) (approved bool, err error)
	// MaxIterations 防止无限循环的硬上限。0 表示默认 10。
	MaxIterations int
}

func (p Policy) effectiveMaxIterations() int {
	if p.MaxIterations <= 0 {
		return 10
	}
	return p.MaxIterations
}

// LoopEventKind 标识 Loop 推送的事件类型。
type LoopEventKind int

const (
	LoopDelta LoopEventKind = iota
	LoopProgress
	LoopToolCallStart
	LoopToolCallEnd
	LoopInfo
)

// LoopEvent 是 Agent Loop 对外推送的事件。
type LoopEvent struct {
	Kind     LoopEventKind
	Text     string
	ToolCall *conversation.ToolCall
}

// EventSink 由调用方（CLI/GUI）实现。
type EventSink func(LoopEvent)

func DeltaEvent(text string) LoopEvent {
	return LoopEvent{Kind: LoopDelta, Text: text}
}

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
		t.Errorf("delta collected %q", collected)
	}

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
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in history, got %d", len(msgs))
	}
	if msgs[0].Role != conversation.RoleUser || msgs[0].Content != "user msg" {
		t.Errorf("unexpected user msg: %+v", msgs[0])
	}
}

func TestV1LoopPropagatesProviderError(t *testing.T) {
	fp := llm.NewFakeProvider()
	agent := NewAgent(fp, tool.NewRegistry(), conversation.NewManager(),
		Policy{AllowToolCalls: false})

	err := agent.Run(context.Background(), "hi", func(LoopEvent) {})
	if err == nil {
		t.Fatal("expected error from exhausted provider")
	}
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
		msgs := a.history.ForRequest()

		var toolDefs []llm.ToolDef
		if a.policy.AllowToolCalls {
			toolDefs = a.buildToolDefs()
		}

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

		a.history.AppendAssistant(resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			return nil
		}

		// 有工具调用 → 执行（V2 路径，T9 实现）
		for _, call := range resp.Message.ToolCalls {
			if err := a.executeToolCall(ctx, call, sink); err != nil {
				return err
			}
		}
	}
	sink(LoopEvent{Kind: LoopInfo, Text: "达到最大迭代数"})
	return ErrMaxIterationsReached
}

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

// executeToolCall 桩实现（T9 替换为真实逻辑）。
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

## T9：V2 Loop —— 工具调用执行

**目标：** 实现 `executeToolCall`，让 Loop 能处理模型返回的工具调用：查找工具 → Policy 确认 → 流式执行 → 结果回填。完成后 V1/V2 共用同一份 Loop 代码，差异仅在 Policy。

**Files:**
- Modify: `internal/agent/agent.go`（替换桩方法）
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

func newEchoRegistry() *tool.Registry {
	r := tool.NewRegistry()
	r.Register(&echoTool{})
	return r
}

func TestV2LoopExecutesToolCall(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "我来查一下",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "echo", Args: []byte(`{}`)},
			},
		},
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

	if deltas != "我来查一下工具返回了结果" {
		t.Errorf("deltas collected %q", deltas)
	}
	if len(toolEvents) < 2 {
		t.Fatalf("expected at least 2 tool events (start+end), got %d", len(toolEvents))
	}
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

Edit `internal/agent/agent.go`，把末尾的桩方法替换为：

```go
// executeToolCall 执行单次工具调用，受 Policy 约束。
func (a *Agent) executeToolCall(ctx context.Context, call conversation.ToolCall, sink EventSink) error {
	if sink != nil {
		sink(LoopEvent{Kind: LoopToolCallStart, ToolCall: &call})
		defer func() {
			sink(LoopEvent{Kind: LoopToolCallEnd, ToolCall: &call})
		}()
	}

	t, ok := a.tools.Get(call.Name)
	if !ok {
		a.history.AppendToolResult(call.ID, call.Name, "未知工具: "+call.Name, true)
		return nil
	}

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

	events, err := t.Execute(ctx, call.Args)
	if err != nil {
		a.history.AppendToolResult(call.ID, call.Name, "执行失败: "+err.Error(), true)
		return nil
	}

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

Expected: 3 个测试仍全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/agent/agent.go internal/agent/agent_v2_test.go
git commit -m "feat(agent): implement tool call execution with Policy confirmation (V2)"
```

---

## T10：Command 包与 SystemInfoTool

**目标：** 实现 `internal/command/` 包，把白名单命令封装为实现 `tool.Tool` 接口的工具。采用结构化传参，规避注入风险。

**Files:**
- Create: `internal/command/types.go`
- Create: `internal/command/runner.go`
- Create: `internal/command/runner_test.go`
- Create: `internal/command/tools.go`
- Create: `internal/registry/registry.go`
- Create: `internal/registry/registry_test.go`

- [ ] **Step 1: 定义白名单命令类型**

Create `internal/command/types.go`:

```go
// Package command 实现白名单命令的执行，封装为 tool.Tool。
// 安全策略：使用 exec.Command(binary, args...) 结构化传参，不经过 shell。
package command

// CommandSpec 定义一个白名单命令。
type CommandSpec struct {
	Title  string
	Binary string
	Args   []string
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
	spec := CommandSpec{Title: "bad", Binary: "this-binary-does-not-exist-xyz", Args: nil}
	runner := NewRunner()
	var resultContent string
	var resultIsError bool

	events, err := runner.Run(context.Background(), spec)
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

func NewRunner() *Runner {
	return &Runner{}
}

// Run 执行一个命令规格，返回事件流。不经 shell，结构化传参。
func (r *Runner) Run(ctx context.Context, spec CommandSpec) (<-chan tool.Event, error) {
	ch := make(chan tool.Event)

	go func() {
		defer close(ch)

		cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)

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

- [ ] **Step 6: 实现 SystemInfoTool**

Create `internal/command/tools.go`:

```go
package command

import (
	"context"
	"runtime"

	"github.com/agentforge/agentforge/internal/tool"
)

// SystemInfoTool 获取系统信息，实现 tool.Tool。
type SystemInfoTool struct {
	runner *Runner
}

func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{runner: NewRunner()}
}

func (t *SystemInfoTool) Name() string        { return "get_system_info" }
func (t *SystemInfoTool) Description() string { return "获取系统信息（OS、运行时）" }

func (t *SystemInfoTool) Schema() []byte {
	return []byte(`{"type":"object","properties":{}}`)
}

func (t *SystemInfoTool) Execute(ctx context.Context, args []byte) (<-chan tool.Event, error) {
	spec := t.platformSpec()
	return t.runner.Run(ctx, spec)
}

func (t *SystemInfoTool) platformSpec() CommandSpec {
	if runtime.GOOS == "windows" {
		return CommandSpec{
			Title:  "系统信息",
			Binary: "powershell.exe",
			Args:   []string{"-NoProfile", "-Command", "Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,OSArchitecture | Format-List"},
		}
	}
	return CommandSpec{
		Title:  "系统信息",
		Binary: "uname",
		Args:   []string{"-a"},
	}
}
```

- [ ] **Step 7: 创建 registry 包（打破 tool↔command 循环依赖）**

⚠️ **循环依赖说明**：`command` 实现 `tool.Tool` 故 `command` import `tool`。若 `tool` 内注册 `command` 工具则 `tool` 又 import `command` → 循环。解决：单独建 `registry` 包同时 import 两者来装配。

Create `internal/registry/registry.go`:

```go
// Package registry 集中注册所有内置工具，打破 tool↔command 循环依赖。
package registry

import (
	"github.com/agentforge/agentforge/internal/command"
	"github.com/agentforge/agentforge/internal/tool"
)

// Setup 把所有内置工具注册到给定 Registry。
func Setup(r *tool.Registry) {
	r.Register(command.NewSystemInfoTool())
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

Expected: 所有包测试全部 PASS。

- [ ] **Step 9: 提交**

```bash
git add internal/command/ internal/registry/
git commit -m "feat(command): add whitelist command runner and SystemInfoTool; add registry package"
```

---

## T11：集成验收测试

**目标：** 端到端集成测试，验证完整 Agent 闭环：用户输入 → LLM 决策 → 工具调用 → 结果回填 → LLM 最终回复。

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

func TestIntegrationEndToEnd(t *testing.T) {
	fp := llm.NewFakeProvider()
	fp.Script([]llm.FakeResponse{
		{
			Text: "我来查一下系统信息",
			ToolCalls: []conversation.ToolCall{
				{ID: "call_1", Name: "get_system_info", Args: []byte(`{}`)},
			},
		},
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

	if toolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", toolCallCount)
	}
	if len(fp.Calls()) != 2 {
		t.Errorf("expected 2 provider calls, got %d", len(fp.Calls()))
	}
	if len(fp.Calls()[0].Tools) == 0 {
		t.Error("first call should pass tools to provider")
	}
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

### Phase 1 检查点

完成 T1-T11 后，Agent 核心引擎具备：

| 能力 | 验证任务 |
|------|---------|
| Tool 接口可被实现 | T10 |
| Registry 注册/查找/列举 | T2, T10 |
| 流式 Execute 推送 | T10 |
| V1 Loop 等价纯聊天 | T8, T11 |
| V2 Loop 完整闭环 | T9, T11 |
| 流式 tool_calls 累积 | T5 |
| Context 压缩不破坏配对 | T6 |

**Phase 1 不包含：** 具体 OpenAI Provider 实现（仅有接口+Fake）、CLI 入口、Wails GUI、RAG。

---

# 第三部分：RAG 功能设计 spec

> 本部分是 RAG 功能的设计决策记录，是第四部分实施计划的依据。

## R.1 背景与定位

**场景：个人知识库问答。** 用户导入个人文档（Markdown / PDF / Office），AgentForge 基于这些资料回答问题。

**规模与形态：**
- 数据规模：小（< 50MB，个人知识库）
- 部署形态：桌面应用单文件分发（Wails 打包），零运维
- Embedding 来源：复用已有 OpenAI 兼容 API 配置，不引入本地推理依赖

**文档类型覆盖：** Markdown/纯文本（P0）+ PDF（P1）+ Office（P1）

## R.2 数据库选型

**决策：`modernc.org/sqlite` + sqlite-vec 扩展，纯 Go 无 CGO。**

| 候选 | 是否采用 | 理由 |
|------|---------|------|
| `modernc.org/sqlite` + sqlite-vec | ✅ | 纯 Go 无 CGO，跨平台干净；原生支持；单文件分发 |
| `ncruces/go-sqlite3` (WASM) | ❌ | WASM 运行时开销与内存高于 modernc |
| `mattn/go-sqlite3` (CGO) | ❌ | CGO 交叉编译复杂，与「V1 避免 CGO」冲突 |
| PostgreSQL + pgvector | ❌ | 要求用户装 PG 服务端，与桌面单文件冲突 |
| Qdrant/Milvus/Chroma | ❌ | 需分发额外进程，过度设计 |

**关键事实：** sqlite-vec 当前仅暴力扫描，但 <50MB 规模毫秒级足够。`modernc.org/sqlite` 已原生支持 sqlite-vec。

**迁移路径：** modernc 与 mattn 都实现 `database/sql` 驱动接口，将来换驱动是机械替换，不伤业务逻辑。

## R.3 切片策略

**核心原则：以语义边界为主（段落/标题/幻灯片页），字符数/token 数仅兜底。** 禁止固定字符数硬切。

**各格式规则：**
- **Markdown**：按 `#`/`##`/`###` 标题 + 空行分段，每个切片存「祖先标题路径」(HeadingPath) 作上下文。embedding 的是 Content；喂给 LLM 的是 HeadingPath + Content。
- **PDF**：按文本块 + 行间距切。demo 不处理多栏/表格/图片/扫描件。解析库 `ledongthuc/pdf`。
- **Office**：本质是 ZIP+XML，用标准库 `archive/zip`+`encoding/xml`。docx 按段落、pptx 每页一个 chunk、xlsx 按行。

**Chunk 参数：** chunk_size 可配置默认 512 token；overlap 默认 50；硬上限 800 token。

**切片算法：** 1.按标题/段落/页切语义块 → 2.≤目标用 → 3.>目标按句号二次切 → 4.>>上限滑动窗口带 overlap。

## R.4 召回评测

**评测对象：** 召回（核心）+ 端到端对话（直观感受），同页两模式。

**指标分层：**
- **L1 召回透明化**：展示 top-k chunks + 相似度 + 来源（必做）
- **L2 有标注指标**：Recall@K / Precision@K / MRR（必做）
- **L3 LLM-as-judge**：LLM 给召回结果打相关性分（实现）
- L4 RAGAS 端到端（后期）

**测试集来源：** 人工标注（起步）+ LLM 自动生成（增强）。

**L3 约束：** judge prompt 固定模板；仅对无人工标注问题启用；用低成本 chat 模型；页面可关闭。

**页面：** 顶部控制栏（知识库下拉/top-k/chunk_size/运行评测）+ 三栏（测试问题列表/对话区/指标面板）。

## R.5 整体架构

RAG 作为 `internal/rag/` 平级模块。依赖单向：`rag/` 被 `conversation/` 依赖，不反向。

**数据流 A（导入写路径）：** 读文件 → 切片 → embedding → 入库（事务）。分两阶段便于失败重试。

**数据流 B（检索读路径）：** query → embed → 向量检索（按 kb_id 过滤）→ top-k → 组装 prompt → 流式对话。检索与对话解耦。

## R.6 数据库 Schema

```sql
CREATE TABLE knowledge_bases (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, embedding_model TEXT,
    chunk_size INTEGER DEFAULT 512, overlap INTEGER DEFAULT 50,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE documents (
    id TEXT PRIMARY KEY, kb_id TEXT NOT NULL REFERENCES knowledge_bases(id),
    file_path TEXT NOT NULL, file_type TEXT, chunk_count INTEGER,
    status TEXT, error_msg TEXT, content_hash TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
-- 向量虚表：维度运行时探测，不硬编码
CREATE VIRTUAL TABLE vec_chunks USING vec0( embedding float[N] );
CREATE TABLE chunks (
    id INTEGER PRIMARY KEY, doc_id TEXT NOT NULL REFERENCES documents(id),
    kb_id TEXT NOT NULL, content TEXT NOT NULL, heading_path TEXT,
    source TEXT, token_count INTEGER, seq INTEGER
);
CREATE INDEX idx_chunks_kb ON chunks(kb_id);
CREATE TABLE eval_questions (
    id INTEGER PRIMARY KEY, kb_id TEXT NOT NULL, question TEXT NOT NULL,
    source TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE eval_expected (
    question_id INTEGER NOT NULL, chunk_id INTEGER NOT NULL,
    PRIMARY KEY (question_id, chunk_id)
);
CREATE TABLE eval_runs (
    id INTEGER PRIMARY KEY, kb_id TEXT NOT NULL, params_json TEXT,
    recall_at_k REAL, mrr REAL, precision_at_k REAL, question_count INTEGER,
    run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**设计说明：** vec_chunks（虚表）与 chunks（普通表）分两张，用 rowid 关联——sqlite-vec 官方推荐。content_hash 防重复导入。embedding_model 记录建库模型，换模型必须重建。

## R.7 错误处理

| 场景 | 处理 |
|------|------|
| embedding API 失败 | 分阶段，重试仅需重跑 embedding+存储 |
| PDF 解析失败/加密 | 返回明确错误，不静默吞掉 |
| 扫描件 PDF | 返回「需 OCR」明确错误 |
| 批量导入单个失败 | 不阻塞其他，收集错误列表 |
| 不支持类型 | NewChunker 返回明确错误 |
| embedding 维度不一致 | 检测 embedding_model 字段，拒绝跨模型检索，提示重建 |
| 建库维度探测 | 先调一次 embedding API 探测维度，动态 CREATE VIRTUAL TABLE |
| 重复导入 | content_hash 检测，跳过 |

## R.8 未覆盖（明确排除）

OCR、多模态 embedding、reranker、hybrid search、多用户同步、RAGAS L4、ANN 索引（HNSW）。

---

# 第四部分：RAG 实施计划

> **范围：** Phase 2，任务 T12-T39。在 `internal/rag/` 下实现完整 RAG 能力，含 GUI 评测页面。

## 与 Phase 1 的衔接

Phase 1（T1-T11）已建立 `internal/{tool,conversation,llm,agent,command,registry}`。Phase 2 的 `rag/` 作为新增平级模块。

**关键衔接：** spec 写「rag/embedder 依赖 llm/」。但 RAG 计划为自包含可独立测试，`rag/embedder` 定义为：`Embedder` 接口 + `FakeEmbedder`（测试用）+ `OpenAIEmbedder`（自持 HTTP client）。将来 `internal/llm/` 有真实实现后，OpenAIEmbedder 可改为复用 llm/ 的 client——接口不变。

## 文件结构（Phase 2 产出）

```
internal/rag/
├── types.go                  # T12
├── embedder/
│   ├── embedder.go           # T13 接口 + FakeEmbedder
│   └── openai.go             # T15 OpenAIEmbedder
├── store/
│   ├── schema.go             # T14
│   ├── store.go              # T15
│   ├── chunks.go             # T16
│   ├── search.go             # T17
│   ├── crud.go               # T18
│   └── vecutil.go            # T16
├── chunker/
│   ├── chunker.go            # T19
│   ├── tokenizer.go          # T20
│   ├── markdown.go           # T21-T22
│   ├── pdf.go                # T33
│   └── office.go             # T34
├── pipeline.go               # T24
├── retrieval.go              # T25
├── prompt.go                 # T26
├── service.go                # T27
└── eval/
    ├── crud.go               # T28
    ├── metrics.go            # T29
    ├── eval.go               # T30
    ├── judge.go              # T35
    └── generator.go          # T36
```

---

## T12：RAG 类型定义

**目标：** 定义贯穿 RAG 全计划的核心数据结构。

**Files:**
- Create: `internal/rag/types.go`

- [ ] **Step 1: 写 types.go**

```go
package rag

import "time"

type KnowledgeBase struct {
	ID             string
	Name           string
	EmbeddingModel string
	ChunkSize      int
	Overlap        int
	CreatedAt      time.Time
}

type Document struct {
	ID          string
	KBID        string
	FilePath    string
	FileType    string
	ChunkCount  int
	Status      string
	ErrorMsg    string
	ContentHash string
	CreatedAt   time.Time
}

type Chunk struct {
	ID          int64
	DocID       string
	KBID        string
	Content     string
	HeadingPath string
	Source      string
	TokenCount  int
	Seq         int
}

type ScoredChunk struct {
	Chunk
	Score float64
}

type RawDocument struct {
	FilePath string
	Content  []byte
	FileType string
}
```

- [ ] **Step 2: 验证编译**

Run: `go build ./internal/rag/`
Expected: 无输出。

- [ ] **Step 3: Commit**

```bash
git add internal/rag/types.go
git commit -m "feat(rag): add core types"
```

---

## T13：Embedder 接口 + FakeEmbedder

**目标：** 定义 embedding 接口与确定性 fake 实现，让后续任务脱离真实 API 测试。

**Files:**
- Create: `internal/rag/embedder/embedder.go`
- Create: `internal/rag/embedder/embedder_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/embedder/embedder_test.go
package embedder

import "testing"

func TestFakeEmbedder_Deterministic(t *testing.T) {
	e := &FakeEmbedder{Dim: 8}
	v1, _ := e.EmbedOne("hello world")
	v2, _ := e.EmbedOne("hello world")
	if len(v1) != 8 {
		t.Fatalf("expected dim 8, got %d", len(v1))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("not deterministic at idx %d", i)
		}
	}
}

func TestFakeEmbedder_EmbedBatch(t *testing.T) {
	e := &FakeEmbedder{Dim: 4}
	vecs, err := e.Embed([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vecs))
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/rag/embedder/`
Expected: FAIL — `undefined: FakeEmbedder`

- [ ] **Step 3: 实现 embedder.go**

```go
// internal/rag/embedder/embedder.go
package embedder

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
)

type Embedder interface {
	Embed(texts []string) ([][]float32, error)
	EmbedOne(text string) ([]float32, error)
	Dim() int
}

// FakeEmbedder 确定性 embedding，基于内容词频哈希。仅测试用。
type FakeEmbedder struct {
	Dim int
}

func (e *FakeEmbedder) Dim() int { return e.Dim }

func (e *FakeEmbedder) EmbedOne(text string) ([]float32, error) {
	vecs, err := e.Embed([]string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e *FakeEmbedder) Embed(texts []string) ([][]float32, error) {
	if e.Dim <= 0 {
		return nil, fmt.Errorf("FakeEmbedder.Dim must be > 0")
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = e.hashVector(text)
	}
	return out, nil
}

func (e *FakeEmbedder) hashVector(text string) []float32 {
	v := make([]float32, e.Dim)
	for _, tok := range tokenize(text) {
		h := sha256.Sum256([]byte(tok))
		idx := int(binary.BigEndian.Uint32(h[:4])) % e.Dim
		val := math.Float32frombits(binary.BigEndian.Uint32(h[4:8]))
		if val < 0 {
			val = -val
		}
		if val == 0 {
			val = 0.001
		}
		v[idx] += val
	}
	var norm float32
	for _, x := range v {
		norm += x * x
	}
	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}

func tokenize(text string) []string {
	var out []string
	cur := ""
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/rag/embedder/ -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/rag/embedder/
git commit -m "feat(rag/embedder): add Embedder interface and FakeEmbedder"
```

---

## T14：SQLite Schema（动态维度）

**目标：** 生成建表 SQL，维度按 embedding 模型运行时确定。

**Files:**
- Create: `internal/rag/store/schema.go`
- Create: `internal/rag/store/schema_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/store/schema_test.go
package store

import (
	"strings"
	"testing"
)

func TestSchemaUsesDynamicDimension(t *testing.T) {
	s768 := Schema(768)
	s1024 := Schema(1024)
	if !strings.Contains(s768, "float[768]") {
		t.Errorf("768 schema should contain float[768]")
	}
	if !strings.Contains(s1024, "float[1024]") {
		t.Errorf("1024 schema should contain float[1024]")
	}
}

func TestSchemaCreatesAllTables(t *testing.T) {
	s := Schema(768)
	for _, table := range []string{"knowledge_bases", "documents", "chunks", "eval_questions", "eval_expected", "eval_runs"} {
		if !strings.Contains(s, table) {
			t.Errorf("schema missing table: %s", table)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/rag/store/`
Expected: FAIL — `undefined: Schema`

- [ ] **Step 3: 实现 schema.go**

```go
// internal/rag/store/schema.go
package store

import "fmt"

func Schema(dim int) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS knowledge_bases (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, embedding_model TEXT,
    chunk_size INTEGER DEFAULT 512, overlap INTEGER DEFAULT 50,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS documents (
    id TEXT PRIMARY KEY, kb_id TEXT NOT NULL REFERENCES knowledge_bases(id),
    file_path TEXT NOT NULL, file_type TEXT, chunk_count INTEGER,
    status TEXT, error_msg TEXT, content_hash TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
    embedding float[%d]
);
CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY, doc_id TEXT NOT NULL REFERENCES documents(id),
    kb_id TEXT NOT NULL, content TEXT NOT NULL, heading_path TEXT,
    source TEXT, token_count INTEGER, seq INTEGER
);
CREATE INDEX IF NOT EXISTS idx_chunks_kb ON chunks(kb_id);
CREATE TABLE IF NOT EXISTS eval_questions (
    id INTEGER PRIMARY KEY, kb_id TEXT NOT NULL, question TEXT NOT NULL,
    source TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS eval_expected (
    question_id INTEGER NOT NULL, chunk_id INTEGER NOT NULL,
    PRIMARY KEY (question_id, chunk_id)
);
CREATE TABLE IF NOT EXISTS eval_runs (
    id INTEGER PRIMARY KEY, kb_id TEXT NOT NULL, params_json TEXT,
    recall_at_k REAL, mrr REAL, precision_at_k REAL, question_count INTEGER,
    run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`, dim)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/rag/store/ -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/rag/store/schema.go internal/rag/store/schema_test.go
git commit -m "feat(rag/store): add schema with dynamic vector dimension"
```

---

## T15：Store 初始化（modernc/sqlite + sqlite-vec）

**目标：** 用 `modernc.org/sqlite` 打开 DB，加载 sqlite-vec，执行 schema。**【关键技术风险点】**

**Files:**
- Create: `internal/rag/store/store.go`
- Create: `internal/rag/store/store_test.go`

- [ ] **Step 1: 添加依赖**

```bash
go get modernc.org/sqlite@latest
```

> **⚠️ 验证 sqlite-vec 可用：** modernc.org/sqlite 需较新版本支持 sqlite-vec。先测试 `SELECT vec_version()`。若该版本不支持，升级版本或换 ncruces 方案。这是 Phase 2 的最大技术风险，必须先验证通过再继续。

- [ ] **Step 2: 写失败测试**

```go
// internal/rag/store/store_test.go
package store

import (
	"path/filepath"
	"testing"
)

func TestNew_CreatesSchemaAndLoadsVec(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath, 768)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer s.Close()

	var ver string
	err = s.db.QueryRow("SELECT vec_version()").Scan(&ver)
	if err != nil {
		t.Fatalf("sqlite-vec not loaded: %v", err)
	}
	if ver == "" {
		t.Fatal("vec_version() returned empty")
	}

	for _, table := range []string{"knowledge_bases", "documents", "chunks", "eval_questions", "eval_expected", "eval_runs"} {
		var name string
		err = s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s not created: %v", table, err)
		}
	}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/rag/store/ -run TestNew -v`
Expected: FAIL — `undefined: Store`。

- [ ] **Step 4: 实现 store.go**

```go
// internal/rag/store/store.go
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	dim  int
	path string
}

func New(dbPath string, dim int) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}

	var ver string
	if err := db.QueryRow("SELECT vec_version()").Scan(&ver); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite-vec not available (modernc version may need upgrade): %w", err)
	}

	if _, err := db.Exec(Schema(dim)); err != nil {
		db.Close()
		return nil, fmt.Errorf("exec schema: %w", err)
	}

	return &Store{db: db, dim: dim, path: dbPath}, nil
}

func (s *Store) Close() error      { return s.db.Close() }
func (s *Store) DB() *sql.DB       { return s.db }
func (s *Store) Dim() int           { return s.dim }
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/rag/store/ -run TestNew -v`
Expected: PASS。

> **若失败：** sqlite-vec 未加载。`go list -m modernc.org/sqlite` 看版本，必要时 `go get modernc.org/sqlite@latest`。必须先解决再继续。

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/rag/store/store.go internal/rag/store/store_test.go
git commit -m "feat(rag/store): init Store with modernc/sqlite + sqlite-vec"
```

---

## T16：SaveChunks（事务插入切片+向量）

**Files:**
- Create: `internal/rag/store/vecutil.go`
- Create: `internal/rag/store/chunks.go`
- Create: `internal/rag/store/chunks_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/store/chunks_test.go
package store

import (
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
)

func TestSaveChunks_AndCount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, err := New(dbPath, 8)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','test')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('doc1','kb1','a.md','markdown','indexed')`)

	chunks := []rag.Chunk{
		{DocID: "doc1", KBID: "kb1", Content: "hello world", HeadingPath: "intro", TokenCount: 2, Seq: 0},
		{DocID: "doc1", KBID: "kb1", Content: "foo bar baz", HeadingPath: "intro", TokenCount: 3, Seq: 1},
	}
	vectors := [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0, 0, 0},
	}

	ids, err := s.SaveChunks(chunks, vectors)
	if err != nil {
		t.Fatalf("SaveChunks: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}

	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM chunks WHERE kb_id='kb1'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 chunks, got %d", n)
	}
}

func TestSaveChunks_RollbackOnError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','test')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('doc1','kb1','a.md','markdown','indexed')`)

	chunks := []rag.Chunk{{DocID: "doc1", KBID: "kb1", Content: "x"}}
	vectors := [][]float32{{0, 0, 0}} // 维度不匹配

	_, err := s.SaveChunks(chunks, vectors)
	if err == nil {
		t.Fatal("expected error for dimension mismatch")
	}

	var n int
	s.DB().QueryRow("SELECT count(*) FROM chunks").Scan(&n)
	if n != 0 {
		t.Fatalf("expected 0 chunks after rollback, got %d", n)
	}
}

func mustExec(t *testing.T, s *Store, q string) {
	t.Helper()
	if _, err := s.DB().Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/rag/store/ -run TestSaveChunks -v`
Expected: FAIL — `undefined: Store.SaveChunks`。

- [ ] **Step 3: 实现 vecutil.go + chunks.go**

```go
// internal/rag/store/vecutil.go
package store

import "math"

func float32ToBytes(f float32) [4]byte {
	bits := math.Float32bits(f)
	return [4]byte{byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24)}
}

func vecToBlob(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		b := float32ToBytes(f)
		copy(buf[i*4:i*4+4], b[:])
	}
	return buf
}
```

```go
// internal/rag/store/chunks.go
package store

import (
	"errors"
	"fmt"

	"github.com/agentforge/agentforge/internal/rag"
)

// SaveChunks 批量写入 chunk 元数据 + 向量，一个事务。
func (s *Store) SaveChunks(chunks []rag.Chunk, vectors [][]float32) ([]int64, error) {
	if len(chunks) != len(vectors) {
		return nil, errors.New("chunks and vectors length mismatch")
	}
	for i, v := range vectors {
		if len(v) != s.dim {
			return nil, fmt.Errorf("vector[%d] dim %d != store dim %d", i, len(v), s.dim)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	ids := make([]int64, 0, len(chunks))
	chunkInsert, err := tx.Prepare(`INSERT INTO chunks(doc_id,kb_id,content,heading_path,source,token_count,seq) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare chunk insert: %w", err)
	}
	defer chunkInsert.Close()

	vecInsert, err := tx.Prepare(`INSERT INTO vec_chunks(embedding) VALUES (?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare vec insert: %w", err)
	}
	defer vecInsert.Close()

	for i, c := range chunks {
		res, err := chunkInsert.Exec(c.DocID, c.KBID, c.Content, c.HeadingPath, c.Source, c.TokenCount, c.Seq)
		if err != nil {
			return nil, fmt.Errorf("insert chunk[%d]: %w", i, err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)

		if _, err := vecInsert.Exec(vecToBlob(vectors[i])); err != nil {
			return nil, fmt.Errorf("insert vec[%d]: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return ids, nil
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/rag/store/ -run TestSaveChunks -v`
Expected: PASS（2 个测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/rag/store/chunks.go internal/rag/store/chunks_test.go internal/rag/store/vecutil.go
git commit -m "feat(rag/store): add SaveChunks with transactional insert"
```

---

## T17：Search（向量检索+JOIN+知识库过滤）

**Files:**
- Create: `internal/rag/store/search.go`
- Create: `internal/rag/store/search_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/store/search_test.go
package store

import (
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
)

func TestSearch_ReturnsTopKBySimilarity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','t')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('doc1','kb1','a.md','markdown','indexed')`)

	chunks := []rag.Chunk{
		{DocID: "doc1", KBID: "kb1", Content: "target", Seq: 0},
		{DocID: "doc1", KBID: "kb1", Content: "other", Seq: 1},
	}
	vectors := [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 1},
	}
	if _, err := s.SaveChunks(chunks, vectors); err != nil {
		t.Fatal(err)
	}

	queryVec := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	results, err := s.Search("kb1", queryVec, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Content != "target" {
		t.Errorf("expected top result 'target', got %q", results[0].Content)
	}
}

func TestSearch_FiltersByKnowledgeBase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','t')`)
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb2','t')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('d1','kb1','a','m','indexed')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('d2','kb2','b','m','indexed')`)

	s.SaveChunks([]rag.Chunk{{DocID: "d1", KBID: "kb1", Content: "in kb1"}}, [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}})
	s.SaveChunks([]rag.Chunk{{DocID: "d2", KBID: "kb2", Content: "in kb2"}}, [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}})

	q := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	res, err := s.Search("kb1", q, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Content != "in kb1" {
		t.Fatalf("kb1 search should only return kb1 chunks, got %v", res)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/rag/store/ -run TestSearch -v`
Expected: FAIL。

- [ ] **Step 3: 实现 search.go**

```go
// internal/rag/store/search.go
package store

import (
	"database/sql"
	"fmt"

	"github.com/agentforge/agentforge/internal/rag"
)

// Search 在指定知识库内按向量余弦距离检索 top-K。
func (s *Store) Search(kbID string, queryVec []float32, topK int) ([]rag.ScoredChunk, error) {
	if len(queryVec) != s.dim {
		return nil, fmt.Errorf("query dim %d != store dim %d", len(queryVec), s.dim)
	}

	rows, err := s.db.Query(`
		SELECT c.id, c.doc_id, c.kb_id, c.content, c.heading_path, c.source, c.token_count, c.seq,
		       (1.0 - v.distance) AS score
		FROM (
			SELECT rowid, distance
			FROM vec_chunks
			WHERE embedding MATCH ?
			  AND k = ?
		) v
		JOIN chunks c ON c.id = v.rowid
		WHERE c.kb_id = ?
		ORDER BY v.distance
	`, vecToBlob(queryVec), topK, kbID)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var out []rag.ScoredChunk
	for rows.Next() {
		var sc rag.ScoredChunk
		var docID, kbIDRead, content string
		var headingPath, source sql.NullString
		var tokenCount, seq int
		var id int64
		if err := rows.Scan(&id, &docID, &kbIDRead, &content, &headingPath, &source, &tokenCount, &seq, &sc.Score); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		sc.ID = id
		sc.DocID = docID
		sc.KBID = kbIDRead
		sc.Content = content
		sc.HeadingPath = headingPath.String
		sc.Source = source.String
		sc.TokenCount = tokenCount
		sc.Seq = seq
		out = append(out, sc)
	}
	return out, rows.Err()
}
```

> **若 KNN 语法报错：** sqlite-vec 不同版本语法略异。以实际 modernc 版本官方文档为准。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/rag/store/ -run TestSearch -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/rag/store/search.go internal/rag/store/search_test.go
git commit -m "feat(rag/store): add vector Search with KB filter and JOIN"
```

---

## T18：知识库/文档 CRUD

**Files:**
- Create: `internal/rag/store/crud.go`
- Create: `internal/rag/store/crud_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/store/crud_test.go
package store

import (
	"path/filepath"
	"testing"
)

func TestCreateKnowledgeBaseAndGet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()

	id, err := s.CreateKnowledgeBase("我的笔记", "text-embedding-3-small", 512, 50)
	if err != nil {
		t.Fatalf("CreateKnowledgeBase: %v", err)
	}
	kb, err := s.GetKnowledgeBase(id)
	if err != nil {
		t.Fatalf("GetKnowledgeBase: %v", err)
	}
	if kb.Name != "我的笔记" || kb.ChunkSize != 512 {
		t.Errorf("unexpected kb: %+v", kb)
	}
}

func TestListKnowledgeBases(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	s.CreateKnowledgeBase("a", "m", 512, 50)
	s.CreateKnowledgeBase("b", "m", 512, 50)
	kbs, _ := s.ListKnowledgeBases()
	if len(kbs) != 2 {
		t.Fatalf("expected 2 kbs, got %d", len(kbs))
	}
}

func TestGetDocumentByHash(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("a", "m", 512, 50)
	s.CreateDocument(kbID, "a.md", "markdown", "hash123")

	existing, _ := s.GetDocumentByHash("hash123")
	if existing == nil {
		t.Fatal("expected to find doc by hash")
	}
	none, _ := s.GetDocumentByHash("nonexist")
	if none != nil {
		t.Error("expected nil for nonexist")
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 crud.go**

```go
// internal/rag/store/crud.go
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/agentforge/agentforge/internal/rag"
)

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateKnowledgeBase(name, embeddingModel string, chunkSize, overlap int) (string, error) {
	id := newID()
	_, err := s.db.Exec(`INSERT INTO knowledge_bases(id,name,embedding_model,chunk_size,overlap) VALUES(?,?,?,?,?)`,
		id, name, embeddingModel, chunkSize, overlap)
	if err != nil {
		return "", fmt.Errorf("create kb: %w", err)
	}
	return id, nil
}

func (s *Store) GetKnowledgeBase(id string) (*rag.KnowledgeBase, error) {
	var kb rag.KnowledgeBase
	var created string
	err := s.db.QueryRow(`SELECT id,name,embedding_model,chunk_size,overlap,created_at FROM knowledge_bases WHERE id=?`, id).
		Scan(&kb.ID, &kb.Name, &kb.EmbeddingModel, &kb.ChunkSize, &kb.Overlap, &created)
	if err != nil {
		return nil, err
	}
	kb.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	return &kb, nil
}

func (s *Store) ListKnowledgeBases() ([]rag.KnowledgeBase, error) {
	rows, err := s.db.Query(`SELECT id,name,embedding_model,chunk_size,overlap,created_at FROM knowledge_bases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rag.KnowledgeBase
	for rows.Next() {
		var kb rag.KnowledgeBase
		var created string
		if err := rows.Scan(&kb.ID, &kb.Name, &kb.EmbeddingModel, &kb.ChunkSize, &kb.Overlap, &created); err != nil {
			return nil, err
		}
		kb.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		out = append(out, kb)
	}
	return out, rows.Err()
}

func (s *Store) CreateDocument(kbID, filePath, fileType, contentHash string) (string, error) {
	id := newID()
	_, err := s.db.Exec(`INSERT INTO documents(id,kb_id,file_path,file_type,status,content_hash) VALUES(?,?,?,?,?,?)`,
		id, kbID, filePath, fileType, "pending", contentHash)
	if err != nil {
		return "", fmt.Errorf("create doc: %w", err)
	}
	return id, nil
}

func (s *Store) GetDocument(id string) (*rag.Document, error) {
	var d rag.Document
	var created string
	err := s.db.QueryRow(`SELECT id,kb_id,file_path,file_type,chunk_count,status,error_msg,content_hash,created_at FROM documents WHERE id=?`, id).
		Scan(&d.ID, &d.KBID, &d.FilePath, &d.FileType, &d.ChunkCount, &d.Status, &d.ErrorMsg, &d.ContentHash, &created)
	if err != nil {
		return nil, err
	}
	d.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	return &d, nil
}

func (s *Store) GetDocumentByHash(hash string) (*rag.Document, error) {
	var d rag.Document
	var created string
	err := s.db.QueryRow(`SELECT id,kb_id,file_path,file_type,chunk_count,status,error_msg,content_hash,created_at FROM documents WHERE content_hash=?`, hash).
		Scan(&d.ID, &d.KBID, &d.FilePath, &d.FileType, &d.ChunkCount, &d.Status, &d.ErrorMsg, &d.ContentHash, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	return &d, nil
}

func (s *Store) UpdateDocumentStatus(docID, status string, chunkCount int, errMsg string) error {
	_, err := s.db.Exec(`UPDATE documents SET status=?, chunk_count=?, error_msg=? WHERE id=?`, status, chunkCount, errMsg, docID)
	return err
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/rag/store/ -v`
Expected: PASS（所有 store 测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/rag/store/crud.go internal/rag/store/crud_test.go
git commit -m "feat(rag/store): add knowledge base and document CRUD"
```

---

### Phase 2A 检查点（存储层完成）

```bash
go test ./internal/rag/store/ -v
```
预期：插入向量 + 余弦检索 + 知识库过滤 全部通过。

---

## T19：Chunker 接口 + dispatch

**Files:**
- Create: `internal/rag/chunker/chunker.go`
- Create: `internal/rag/chunker/markdown.go`（占位）
- Create: `internal/rag/chunker/pdf.go`（占位）
- Create: `internal/rag/chunker/office.go`（占位）

- [ ] **Step 1: 写 chunker.go（接口 + dispatch）**

```go
// internal/rag/chunker/chunker.go
package chunker

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/agentforge/agentforge/internal/rag"
)

type Chunker interface {
	Chunk(doc rag.RawDocument) ([]rag.Chunk, error)
}

type Options struct {
	ChunkSize int
	Overlap   int
}

func New(filePath string, opts Options) (Chunker, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 512
	}
	if opts.Overlap < 0 {
		opts.Overlap = 50
	}
	switch ext {
	case ".md", ".markdown", ".txt":
		return &TextChunker{opts: opts}, nil
	case ".pdf":
		return &PDFChunker{opts: opts}, nil
	case ".docx", ".pptx", ".xlsx":
		return &OfficeChunker{opts: opts}, nil
	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}
}
```

- [ ] **Step 2: 占位实现（后续 Task 填充）**

```go
// internal/rag/chunker/markdown.go
package chunker

import "github.com/agentforge/agentforge/internal/rag"

type TextChunker struct{ opts Options }

func (c *TextChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	return nil, nil // T21 实现
}
```

```go
// internal/rag/chunker/pdf.go
package chunker

import (
	"fmt"
	"github.com/agentforge/agentforge/internal/rag"
)

type PDFChunker struct{ opts Options }

func (c *PDFChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	return nil, fmt.Errorf("PDF chunker not implemented yet (T33)")
}
```

```go
// internal/rag/chunker/office.go
package chunker

import (
	"fmt"
	"github.com/agentforge/agentforge/internal/rag"
)

type OfficeChunker struct{ opts Options }

func (c *OfficeChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	return nil, fmt.Errorf("Office chunker not implemented yet (T34)")
}
```

- [ ] **Step 3: 验证编译** → `go build ./internal/rag/chunker/`

- [ ] **Step 4: Commit**

```bash
git add internal/rag/chunker/
git commit -m "feat(rag/chunker): add Chunker interface and dispatch by extension"
```

---

## T20：Token 估算

**Files:**
- Create: `internal/rag/chunker/tokenizer.go`
- Create: `internal/rag/chunker/tokenizer_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/chunker/tokenizer_test.go
package chunker

import "testing"

func TestEstimateTokens_Chinese(t *testing.T) {
	got := EstimateTokens("你好世界你好世界你好世界") // 10 中文字
	if got < 12 || got > 20 {
		t.Errorf("chinese 10 chars: expected 12-20, got %d", got)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("empty: expected 0, got %d", got)
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 tokenizer.go**

```go
// internal/rag/chunker/tokenizer.go
package chunker

import "unicode"

func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var tokens int
	var cjkCount, wordChars int
	flushWord := func() {
		if wordChars > 0 {
			tokens += wordChars/4 + 1
			wordChars = 0
		}
	}
	flushCJK := func() {
		if cjkCount > 0 {
			tokens += int(float64(cjkCount) * 1.5)
			cjkCount = 0
		}
	}
	for _, r := range text {
		if isCJK(r) {
			flushWord()
			cjkCount++
		} else if unicode.IsSpace(r) {
			flushWord()
			flushCJK()
		} else {
			flushCJK()
			wordChars++
		}
	}
	flushWord()
	flushCJK()
	return tokens
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x3040 && r <= 0x30FF)
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/chunker/tokenizer.go internal/rag/chunker/tokenizer_test.go
git commit -m "feat(rag/chunker): add token estimation for CJK and latin"
```

---

## T21：TextChunker —— 标题解析 + HeadingPath

**Files:**
- Modify: `internal/rag/chunker/markdown.go`
- Create: `internal/rag/chunker/markdown_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/chunker/markdown_test.go
package chunker

import (
	"strings"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
)

func TestTextChunker_SplitsByHeading(t *testing.T) {
	input := `# 安装

这是安装说明。

## Windows

双击 exe 即可。

## macOS

brew install agentforge。
`
	c := &TextChunker{opts: Options{ChunkSize: 512, Overlap: 50}}
	chunks, err := c.Chunk(rag.RawDocument{FilePath: "guide.md", FileType: "markdown", Content: []byte(input)})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected >= 3 chunks, got %d", len(chunks))
	}

	var winChunk *rag.Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Content, "双击") {
			winChunk = &chunks[i]
		}
	}
	if winChunk == nil {
		t.Fatal("no chunk containing '双击'")
	}
	if winChunk.HeadingPath != "安装 > Windows" {
		t.Errorf("HeadingPath = %q, want %q", winChunk.HeadingPath, "安装 > Windows")
	}
}

func TestTextChunker_PureTxtNoHeading(t *testing.T) {
	input := "第一段内容。\n\n第二段内容。\n\n第三段内容。"
	c := &TextChunker{opts: Options{ChunkSize: 512, Overlap: 50}}
	chunks, err := c.Chunk(rag.RawDocument{FilePath: "a.txt", FileType: "text", Content: []byte(input)})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL（返回 nil）

- [ ] **Step 3: 实现 markdown.go**

```go
// internal/rag/chunker/markdown.go
package chunker

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/agentforge/agentforge/internal/rag"
)

type TextChunker struct{ opts Options }

func (c *TextChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	text := string(doc.Content)
	isMD := strings.EqualFold(filepath.Ext(doc.FilePath), ".md") ||
		strings.EqualFold(filepath.Ext(doc.FilePath), ".markdown")

	var chunks []rag.Chunk
	seq := 0
	headingStack := []string{}
	var currentHeading string
	var paraBuf bytes.Buffer

	flushPara := func() {
		content := strings.TrimSpace(paraBuf.String())
		paraBuf.Reset()
		if content == "" {
			return
		}
		chunks = append(chunks, rag.Chunk{
			Content:     content,
			HeadingPath: currentHeading,
			Source:      doc.FilePath,
			TokenCount:  EstimateTokens(content),
			Seq:         seq,
		})
		seq++
	}

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if isMD && strings.HasPrefix(trimmed, "#") {
			flushPara()
			level, title := parseHeading(trimmed)
			headingStack = trimToLevel(headingStack, level)
			headingStack = append(headingStack, title)
			currentHeading = strings.Join(headingStack, " > ")
			continue
		}
		if trimmed == "" {
			flushPara()
			continue
		}
		if paraBuf.Len() > 0 {
			paraBuf.WriteByte('\n')
		}
		paraBuf.WriteString(line)
	}
	flushPara()

	return applySizeLimit(chunks, c.opts), nil
}

func parseHeading(line string) (level int, title string) {
	level = 0
	for _, r := range line {
		if r == '#' {
			level++
		} else {
			break
		}
	}
	title = strings.TrimSpace(line[level:])
	return
}

func trimToLevel(stack []string, level int) []string {
	if len(stack) > level-1 {
		return stack[:level-1]
	}
	return stack
}

// applySizeLimit 包级函数（T22 实现真实逻辑，先 passthrough）。
func applySizeLimit(chunks []rag.Chunk, opts Options) []rag.Chunk {
	return chunks
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/chunker/markdown.go internal/rag/chunker/markdown_test.go
git commit -m "feat(rag/chunker): split markdown by heading with HeadingPath"
```

---

## T22：TextChunker —— token 兜底二次切分

**Files:**
- Modify: `internal/rag/chunker/markdown.go`（实现 applySizeLimit）
- Modify: `internal/rag/chunker/markdown_test.go`

- [ ] **Step 1: 写失败测试（追加到 markdown_test.go）**

```go
func TestTextChunker_SplitsOversizedChunk(t *testing.T) {
	longPara := strings.Repeat("这是一个很长的句子。", 10)
	c := &TextChunker{opts: Options{ChunkSize: 20, Overlap: 5}}
	chunks, err := c.Chunk(rag.RawDocument{FilePath: "a.txt", FileType: "text", Content: []byte(longPara)})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) <= 1 {
		t.Fatalf("expected oversized chunk to be split, got %d", len(chunks))
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL（未切分）

- [ ] **Step 3: 实现 applySizeLimit + splitBySentence**

替换 markdown.go 中的 `applySizeLimit`：

```go
func applySizeLimit(chunks []rag.Chunk, opts Options) []rag.Chunk {
	var out []rag.Chunk
	seq := 0
	for _, ch := range chunks {
		if ch.TokenCount <= opts.ChunkSize*2 {
			ch.Seq = seq
			out = append(out, ch)
			seq++
			continue
		}
		subs := splitBySentence(ch.Content, opts.ChunkSize)
		for _, sub := range subs {
			out = append(out, rag.Chunk{
				Content:     sub,
				HeadingPath: ch.HeadingPath,
				Source:      ch.Source,
				TokenCount:  EstimateTokens(sub),
				Seq:         seq,
			})
			seq++
		}
	}
	return out
}

func splitBySentence(text string, target int) []string {
	sentences := splitSentences(text)
	var out []string
	var buf string
	bufTokens := 0
	for _, s := range sentences {
		st := EstimateTokens(s)
		if bufTokens+st > target && buf != "" {
			out = append(out, strings.TrimSpace(buf))
			buf = s
			bufTokens = st
		} else {
			if buf != "" {
				buf += "\n"
			}
			buf += s
			bufTokens += st
		}
	}
	if strings.TrimSpace(buf) != "" {
		out = append(out, strings.TrimSpace(buf))
	}
	return out
}

func splitSentences(text string) []string {
	var out []string
	cur := ""
	for _, r := range text {
		cur += string(r)
		if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' || r == '\n' {
			if strings.TrimSpace(cur) != "" {
				out = append(out, cur)
			}
			cur = ""
		}
	}
	if strings.TrimSpace(cur) != "" {
		out = append(out, cur)
	}
	return out
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/rag/chunker/ -v` → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/chunker/markdown.go internal/rag/chunker/markdown_test.go
git commit -m "feat(rag/chunker): add token-bounded secondary splitting"
```

---

## T23：OpenAIEmbedder（真实 API）

**Files:**
- Create: `internal/rag/embedder/openai.go`
- Create: `internal/rag/embedder/openai_test.go`

- [ ] **Step 1: 写测试（httptest mock）**

```go
// internal/rag/embedder/openai_test.go
package embedder

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedder_EmbedBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		inputs := req["input"].([]any)
		data := []map[string]any{}
		for i := range inputs {
			data = append(data, map[string]any{
				"object": "embedding", "index": i,
				"embedding": []float64{0.1 * float64(i+1), 0.2, 0.3, 0.4},
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(srv.URL, "sk-fake", "text-embedding-3-small")
	vecs, err := e.Embed([]string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 4 {
		t.Fatalf("unexpected result")
	}
}

func TestOpenAIEmbedder_ApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "invalid api key"}})
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder(srv.URL, "sk-fake", "text-embedding-3-small")
	_, err := e.Embed([]string{"hi"})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 openai.go**

```go
// internal/rag/embedder/openai.go
package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type OpenAIEmbedder struct {
	BaseURL string
	APIKey  string
	Model   string
	mu      sync.Mutex
	dim     int
}

func NewOpenAIEmbedder(baseURL, apiKey, model string) *OpenAIEmbedder {
	return &OpenAIEmbedder{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func (e *OpenAIEmbedder) Dim() int { return e.dim }

type embedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct{ Message string } `json:"error,omitempty"`
}

func (e *OpenAIEmbedder) Embed(texts []string) ([][]float32, error) {
	body, _ := json.Marshal(embedRequest{Input: texts, Model: e.Model})
	req, _ := http.NewRequest("POST", e.BaseURL+"/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var er embedResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != 200 {
		msg := "unknown"
		if er.Error != nil {
			msg = er.Error.Message
		}
		return nil, fmt.Errorf("embedding API status %d: %s", resp.StatusCode, msg)
	}

	out := make([][]float32, len(er.Data))
	for i, d := range er.Data {
		v := make([]float32, len(d.Embedding))
		for j, f := range d.Embedding {
			v[j] = float32(f)
		}
		out[i] = v
	}

	e.mu.Lock()
	if e.dim == 0 && len(out) > 0 {
		e.dim = len(out[0])
	}
	e.mu.Unlock()

	return out, nil
}

func (e *OpenAIEmbedder) EmbedOne(text string) ([]float32, error) {
	vecs, err := e.Embed([]string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/embedder/openai.go internal/rag/embedder/openai_test.go
git commit -m "feat(rag/embedder): add OpenAIEmbedder for compatible API"
```

---

## T24：导入流水线 pipeline.ImportDocument

**Files:**
- Create: `internal/rag/pipeline.go`
- Create: `internal/rag/pipeline_test.go`

- [ ] **Step 1: 写失败测试（FakeEmbedder + 内存 store）**

```go
// internal/rag/pipeline_test.go
package rag

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/store"
)

func TestImportDocument_Markdown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := store.New(dbPath, 32)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("test", "fake", 512, 50)

	mdPath := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(mdPath, []byte("# 笔记\n\n这是第一段内容。\n\n## 细节\n\n第二段内容。"), 0644)

	p := NewPipeline(s, &embedder.FakeEmbedder{Dim: 32})
	result, err := p.ImportDocument(kbID, mdPath)
	if err != nil {
		t.Fatalf("ImportDocument: %v", err)
	}
	if result.Status != "indexed" {
		t.Errorf("expected indexed, got %s", result.Status)
	}
	if result.ChunkCount < 2 {
		t.Errorf("expected >=2 chunks, got %d", result.ChunkCount)
	}
}

func TestImportDocument_Dedup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := store.New(dbPath, 32)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("test", "fake", 512, 50)

	mdPath := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(mdPath, []byte("# 标题\n\n内容"), 0644)

	p := NewPipeline(s, &embedder.FakeEmbedder{Dim: 32})
	r1, _ := p.ImportDocument(kbID, mdPath)
	if r1.Skipped {
		t.Fatal("first import should not be skipped")
	}
	r2, _ := p.ImportDocument(kbID, mdPath)
	if !r2.Skipped {
		t.Error("second import of same file should be skipped")
	}
}

func TestImportDocument_UnsupportedType(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := store.New(dbPath, 32)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("test", "fake", 512, 50)

	p := NewPipeline(s, &embedder.FakeEmbedder{Dim: 32})
	_, err := p.ImportDocument(kbID, "foo.xyz")
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 pipeline.go**

```go
// internal/rag/pipeline.go
package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentforge/agentforge/internal/rag/chunker"
	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/store"
)

type Pipeline struct {
	store    *store.Store
	embedder embedder.Embedder
}

func NewPipeline(s *store.Store, e embedder.Embedder) *Pipeline {
	return &Pipeline{store: s, embedder: e}
}

type ImportResult struct {
	DocID      string
	Status     string
	ChunkCount int
	ErrorMsg   string
	Skipped    bool
}

func (p *Pipeline) ImportDocument(kbID, filePath string) (ImportResult, error) {
	result := ImportResult{}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return result, fmt.Errorf("read file: %w", err)
	}
	hash := hashContent(content)

	existing, err := p.store.GetDocumentByHash(hash)
	if err != nil {
		return result, fmt.Errorf("dedup check: %w", err)
	}
	if existing != nil {
		result.DocID = existing.ID
		result.Skipped = true
		result.Status = existing.Status
		result.ChunkCount = existing.ChunkCount
		return result, nil
	}

	kb, err := p.store.GetKnowledgeBase(kbID)
	if err != nil {
		return result, fmt.Errorf("get kb: %w", err)
	}

	fileType := typeFromExt(filePath)
	docID, err := p.store.CreateDocument(kbID, filePath, fileType, hash)
	if err != nil {
		return result, fmt.Errorf("create doc: %w", err)
	}
	result.DocID = docID

	ch, err := chunker.New(filePath, chunker.Options{ChunkSize: kb.ChunkSize, Overlap: kb.Overlap})
	if err != nil {
		p.markFailed(docID, err)
		return result, err
	}
	rawDoc := RawDocument{FilePath: filePath, Content: content, FileType: fileType}
	parts, err := ch.Chunk(rawDoc)
	if err != nil {
		p.markFailed(docID, err)
		return result, err
	}
	if len(parts) == 0 {
		p.markFailed(docID, fmt.Errorf("no chunks produced"))
		return result, fmt.Errorf("no chunks")
	}

	for i := range parts {
		parts[i].KBID = kbID
		parts[i].DocID = docID
	}

	texts := make([]string, len(parts))
	for i, c := range parts {
		texts[i] = c.Content
	}
	vectors, err := p.embedder.Embed(texts)
	if err != nil {
		p.markFailed(docID, err)
		return result, fmt.Errorf("embed: %w", err)
	}

	if _, err := p.store.SaveChunks(parts, vectors); err != nil {
		p.markFailed(docID, err)
		return result, fmt.Errorf("save chunks: %w", err)
	}

	if err := p.store.UpdateDocumentStatus(docID, "indexed", len(parts), ""); err != nil {
		return result, fmt.Errorf("update status: %w", err)
	}
	result.Status = "indexed"
	result.ChunkCount = len(parts)
	return result, nil
}

func (p *Pipeline) markFailed(docID string, err error) {
	p.store.UpdateDocumentStatus(docID, "failed", 0, err.Error())
}

func hashContent(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func typeFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return "markdown"
	case ".txt":
		return "text"
	case ".pdf":
		return "pdf"
	case ".docx":
		return "docx"
	case ".pptx":
		return "pptx"
	case ".xlsx":
		return "xlsx"
	default:
		return ext
	}
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/pipeline.go internal/rag/pipeline_test.go
git commit -m "feat(rag): add ImportDocument pipeline with dedup"
```

---

## T25：retrieval.Retrieve（检索编排）

**Files:**
- Create: `internal/rag/retrieval.go`
- Create: `internal/rag/retrieval_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/retrieval_test.go
package rag

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/store"
)

func TestRetrieve_ReturnsRelevant(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := store.New(dbPath, 32)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("test", "fake", 512, 50)

	mdPath := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(mdPath, []byte("# 数据库\n\nAgentForge 使用 sqlite-vec 存储向量。"), 0644)

	emb := &embedder.FakeEmbedder{Dim: 32}
	p := NewPipeline(s, emb)
	p.ImportDocument(kbID, mdPath)

	r := NewRetriever(s, emb)
	results, err := r.Retrieve(kbID, "数据库", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 retrieval.go**

```go
// internal/rag/retrieval.go
package rag

import (
	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/store"
)

type Retriever struct {
	store    *store.Store
	embedder embedder.Embedder
}

func NewRetriever(s *store.Store, e embedder.Embedder) *Retriever {
	return &Retriever{store: s, embedder: e}
}

func (r *Retriever) Retrieve(kbID, query string, topK int) ([]ScoredChunk, error) {
	if topK <= 0 {
		topK = 5
	}
	qv, err := r.embedder.EmbedOne(query)
	if err != nil {
		return nil, err
	}
	return r.store.Search(kbID, qv, topK)
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/retrieval.go internal/rag/retrieval_test.go
git commit -m "feat(rag): add Retriever for query→embed→search"
```

---

## T26：prompt 组装

**Files:**
- Create: `internal/rag/prompt.go`
- Create: `internal/rag/prompt_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/prompt_test.go
package rag

import (
	"strings"
	"testing"
)

func TestBuildRAGPrompt(t *testing.T) {
	chunks := []ScoredChunk{
		{Chunk: Chunk{Content: "AgentForge 用 sqlite-vec。", HeadingPath: "数据库"}, Score: 0.9},
	}
	p := BuildRAGPrompt("用什么数据库？", chunks)
	if !strings.Contains(p, "用什么数据库？") {
		t.Error("prompt should contain question")
	}
	if !strings.Contains(p, "sqlite-vec") {
		t.Error("prompt should contain retrieved content")
	}
	if !strings.Contains(p, "[数据库]") {
		t.Error("prompt should contain heading path context")
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 prompt.go**

```go
// internal/rag/prompt.go
package rag

import (
	"fmt"
	"strings"
)

func BuildRAGPrompt(query string, chunks []ScoredChunk) string {
	var sb strings.Builder
	sb.WriteString("以下是从知识库检索到的相关资料：\n\n")
	for i, c := range chunks {
		if c.HeadingPath != "" {
			sb.WriteString(fmt.Sprintf("[%s]\n", c.HeadingPath))
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, c.Content))
	}
	sb.WriteString("请基于上述资料回答问题。如果资料中没有答案，请说明。\n\n")
	sb.WriteString("问题：")
	sb.WriteString(query)
	return sb.String()
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/prompt.go internal/rag/prompt_test.go
git commit -m "feat(rag): add BuildRAGPrompt for context injection"
```

---

## T27：RAGService（高层门面）

**Files:**
- Create: `internal/rag/service.go`
- Create: `internal/rag/service_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/service_test.go
package rag

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag/embedder"
)

func TestService_ImportAndRetrieve(t *testing.T) {
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

	mdPath := filepath.Join(t.TempDir(), "a.md")
	os.WriteFile(mdPath, []byte("# Q\n\n答案在这里。"), 0644)
	svc.ImportDocument(kbID, mdPath)

	res, err := svc.Retrieve(kbID, "Q", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("expected results")
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 service.go**

```go
// internal/rag/service.go
package rag

import (
	"fmt"

	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/store"
)

type ServiceConfig struct {
	DBPath         string
	EmbedDim       int
	Embedder       embedder.Embedder
	EmbeddingModel string
}

type Service struct {
	store     *store.Store
	embedder  embedder.Embedder
	pipeline  *Pipeline
	retriever *Retriever
	modelName string
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.EmbedDim <= 0 {
		return nil, fmt.Errorf("EmbedDim must be > 0")
	}
	if cfg.Embedder == nil {
		return nil, fmt.Errorf("Embedder is required")
	}
	s, err := store.New(cfg.DBPath, cfg.EmbedDim)
	if err != nil {
		return nil, err
	}
	model := cfg.EmbeddingModel
	if model == "" {
		model = "default"
	}
	return &Service{
		store: s, embedder: cfg.Embedder,
		pipeline: NewPipeline(s, cfg.Embedder),
		retriever: NewRetriever(s, cfg.Embedder),
		modelName: model,
	}, nil
}

func (s *Service) Close() error { return s.store.Close() }

func (s *Service) CreateKnowledgeBase(name string, chunkSize, overlap int) (string, error) {
	return s.store.CreateKnowledgeBase(name, s.modelName, chunkSize, overlap)
}

func (s *Service) ListKnowledgeBases() ([]KnowledgeBase, error) {
	return s.store.ListKnowledgeBases()
}

func (s *Service) ImportDocument(kbID, filePath string) (ImportResult, error) {
	return s.pipeline.ImportDocument(kbID, filePath)
}

func (s *Service) Retrieve(kbID, query string, topK int) ([]ScoredChunk, error) {
	return s.retriever.Retrieve(kbID, query, topK)
}

func (s *Service) BuildPrompt(query string, chunks []ScoredChunk) string {
	return BuildRAGPrompt(query, chunks)
}

func (s *Service) Store() *store.Store             { return s.store }
func (s *Service) Embedder() embedder.Embedder     { return s.embedder }
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/service.go internal/rag/service_test.go
git commit -m "feat(rag): add Service as high-level facade for binding"
```

---

### Phase 2B 检查点（RAG 核心引擎完成，仅 Markdown）

```bash
go test ./internal/rag/... -v
```
预期：导入文档 → 检索 → 组装 prompt 完整闭环。

---

## T28：评测 CRUD（问题 + 期望命中）

**Files:**
- Create: `internal/rag/eval/crud.go`
- Create: `internal/rag/eval/crud_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/eval/crud_test.go
package eval

import (
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
	"github.com/agentforge/agentforge/internal/rag/store"
)

func TestAddEvalQuestionAndExpected(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := store.New(dbPath, 8)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("t", "m", 512, 50)
	ids, _ := s.SaveChunks(
		[]rag.Chunk{
			{DocID: "d1", KBID: kbID, Content: "chunk A"},
			{DocID: "d1", KBID: kbID, Content: "chunk B"},
		},
		[][]float32{{1, 0, 0, 0, 0, 0, 0, 0}, {0, 1, 0, 0, 0, 0, 0, 0}},
	)

	qID, err := AddQuestion(s, kbID, "测试问题", "manual")
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := SetExpected(s, qID, []int64{ids[0], ids[1]}); err != nil {
		t.Fatalf("SetExpected: %v", err)
	}

	qs, _ := ListQuestions(s, kbID)
	if len(qs) != 1 || qs[0].Question != "测试问题" {
		t.Errorf("unexpected questions: %+v", qs)
	}

	exp, _ := GetExpected(s, qID)
	if len(exp) != 2 {
		t.Errorf("expected 2 expected chunks, got %d", len(exp))
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 crud.go**

```go
// internal/rag/eval/crud.go
package eval

import (
	"github.com/agentforge/agentforge/internal/rag"
	"github.com/agentforge/agentforge/internal/rag/store"
)

type Question struct {
	ID       int64
	KBID     string
	Question string
	Source   string
}

func AddQuestion(s *store.Store, kbID, question, source string) (int64, error) {
	res, err := s.DB().Exec(`INSERT INTO eval_questions(kb_id,question,source) VALUES(?,?,?)`, kbID, question, source)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func ListQuestions(s *store.Store, kbID string) ([]Question, error) {
	rows, err := s.DB().Query(`SELECT id,kb_id,question,source FROM eval_questions WHERE kb_id=? ORDER BY id`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.KBID, &q.Question, &q.Source); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func SetExpected(s *store.Store, questionID int64, chunkIDs []int64) error {
	tx, err := s.DB().Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM eval_expected WHERE question_id=?`, questionID); err != nil {
		tx.Rollback()
		return err
	}
	for _, cid := range chunkIDs {
		if _, err := tx.Exec(`INSERT INTO eval_expected(question_id,chunk_id) VALUES(?,?)`, questionID, cid); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func GetExpected(s *store.Store, questionID int64) ([]int64, error) {
	rows, err := s.DB().Query(`SELECT chunk_id FROM eval_expected WHERE question_id=?`, questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var cid int64
		if err := rows.Scan(&cid); err != nil {
			return nil, err
		}
		out = append(out, cid)
	}
	return out, rows.Err()
}

func GetChunk(s *store.Store, chunkID int64) (rag.Chunk, error) {
	var c rag.Chunk
	err := s.DB().QueryRow(`SELECT id,doc_id,kb_id,content,heading_path,source,token_count,seq FROM chunks WHERE id=?`, chunkID).
		Scan(&c.ID, &c.DocID, &c.KBID, &c.Content, &c.HeadingPath, &c.Source, &c.TokenCount, &c.Seq)
	return c, err
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/eval/crud.go internal/rag/eval/crud_test.go
git commit -m "feat(rag/eval): add eval question and expected-chunk CRUD"
```

---

## T29：metrics.go（Recall@K / Precision@K / MRR 纯函数）

**Files:**
- Create: `internal/rag/eval/metrics.go`
- Create: `internal/rag/eval/metrics_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/eval/metrics_test.go
package eval

import (
	"math"
	"testing"
)

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestRecallAtK(t *testing.T) {
	r := RecallAtK([]int64{3, 1, 4, 2, 5}, map[int64]bool{1: true, 2: true}, 5)
	if !approxEqual(r, 1.0) {
		t.Errorf("expected 1.0, got %f", r)
	}
	r = RecallAtK([]int64{3, 4, 1}, map[int64]bool{1: true, 2: true}, 3)
	if !approxEqual(r, 0.5) {
		t.Errorf("expected 0.5, got %f", r)
	}
}

func TestPrecisionAtK(t *testing.T) {
	p := PrecisionAtK([]int64{1, 2, 3}, map[int64]bool{1: true, 2: true}, 3)
	if !approxEqual(p, 2.0/3.0) {
		t.Errorf("expected 0.667, got %f", p)
	}
}

func TestMRR(t *testing.T) {
	m := MRR([]int64{1, 2, 3}, map[int64]bool{2: true})
	if !approxEqual(m, 0.5) {
		t.Errorf("expected 0.5, got %f", m)
	}
	m = MRR([]int64{4, 5}, map[int64]bool{1: true})
	if !approxEqual(m, 0.0) {
		t.Errorf("expected 0.0, got %f", m)
	}
}

func TestAggregate(t *testing.T) {
	scores := []QuestionScore{
		{RecallAtK: 1.0}, {RecallAtK: 0.5}, {RecallAtK: 0.0},
	}
	agg := Aggregate(scores)
	if !approxEqual(agg.RecallAtK, 0.5) {
		t.Errorf("agg recall = %f, want 0.5", agg.RecallAtK)
	}
}

func TestAggregate_Empty(t *testing.T) {
	agg := Aggregate(nil)
	if !approxEqual(agg.RecallAtK, 0) || !approxEqual(agg.MRR, 0) {
		t.Error("empty aggregate should be zeros")
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 metrics.go**

```go
// internal/rag/eval/metrics.go
package eval

func RecallAtK(retrieved []int64, expected map[int64]bool, k int) float64 {
	if len(expected) == 0 {
		return 0
	}
	hit := 0
	for i, id := range retrieved {
		if i >= k {
			break
		}
		if expected[id] {
			hit++
		}
	}
	return float64(hit) / float64(len(expected))
}

func PrecisionAtK(retrieved []int64, expected map[int64]bool, k int) float64 {
	if k <= 0 {
		return 0
	}
	hit := 0
	for i, id := range retrieved {
		if i >= k {
			break
		}
		if expected[id] {
			hit++
		}
	}
	return float64(hit) / float64(k)
}

func MRR(retrieved []int64, expected map[int64]bool) float64 {
	for i, id := range retrieved {
		if expected[id] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

type QuestionScore struct {
	QuestionID   int64
	RecallAtK    float64
	PrecisionAtK float64
	MRR          float64
}

type AggregateResult struct {
	RecallAtK    float64
	PrecisionAtK float64
	MRR          float64
	Count        int
}

func Aggregate(scores []QuestionScore) AggregateResult {
	if len(scores) == 0 {
		return AggregateResult{}
	}
	var r, p, m float64
	for _, s := range scores {
		r += s.RecallAtK
		p += s.PrecisionAtK
		m += s.MRR
	}
	n := float64(len(scores))
	return AggregateResult{RecallAtK: r / n, PrecisionAtK: p / n, MRR: m / n, Count: len(scores)}
}
```

- [ ] **Step 4: 运行确认通过** → PASS（6 个测试）

- [ ] **Step 5: Commit**

```bash
git add internal/rag/eval/metrics.go internal/rag/eval/metrics_test.go
git commit -m "feat(rag/eval): add Recall@K/Precision@K/MRR metrics"
```

---

## T30：RunEvaluation（跑评测 + 存 eval_runs）

**Files:**
- Create: `internal/rag/eval/eval.go`
- Create: `internal/rag/eval/eval_test.go`

- [ ] **Step 1: 写失败测试**

```go
// internal/rag/eval/eval_test.go
package eval

import (
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/store"
)

func TestRunEvaluation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := store.New(dbPath, 8)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("t", "m", 512, 50)

	ids, _ := s.SaveChunks(
		[]rag.Chunk{{DocID: "d1", KBID: kbID, Content: "hello world"}},
		[][]float32{{1, 0, 0, 0, 0, 0, 0, 0}},
	)
	qID, _ := AddQuestion(s, kbID, "what is it", "manual")
	SetExpected(s, qID, []int64{ids[0]})

	emb := &embedder.FakeEmbedder{Dim: 8}
	result, err := Run(s, emb, kbID, EvalParams{TopK: 5, ChunkSize: 512})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.QuestionCount != 1 {
		t.Errorf("question count = %d", result.QuestionCount)
	}
	if result.RecallAtK < 0 || result.RecallAtK > 1 {
		t.Errorf("recall out of range: %f", result.RecallAtK)
	}

	var n int
	s.DB().QueryRow("SELECT count(*) FROM eval_runs WHERE kb_id=?", kbID).Scan(&n)
	if n != 1 {
		t.Errorf("expected 1 eval_run, got %d", n)
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 eval.go**

```go
// internal/rag/eval/eval.go
package eval

import (
	"encoding/json"

	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/store"
)

type EvalParams struct {
	TopK      int `json:"top_k"`
	ChunkSize int `json:"chunk_size"`
}

type EvalResult struct {
	RecallAtK     float64         `json:"recall_at_k"`
	MRR           float64         `json:"mrr"`
	PrecisionAtK  float64         `json:"precision_at_k"`
	QuestionCount int             `json:"question_count"`
	PerQuestion   []QuestionScore `json:"per_question"`
}

func Run(s *store.Store, emb embedder.Embedder, kbID string, params EvalParams) (EvalResult, error) {
	if params.TopK <= 0 {
		params.TopK = 5
	}
	result := EvalResult{PerQuestion: []QuestionScore{}}

	questions, err := ListQuestions(s, kbID)
	if err != nil {
		return result, err
	}

	for _, q := range questions {
		expected, err := GetExpected(s, q.ID)
		if err != nil {
			return result, err
		}
		expectedSet := map[int64]bool{}
		for _, e := range expected {
			expectedSet[e] = true
		}

		qv, err := emb.EmbedOne(q.Question)
		if err != nil {
			return result, err
		}
		chunks, err := s.Search(kbID, qv, params.TopK)
		if err != nil {
			return result, err
		}
		retrieved := make([]int64, len(chunks))
		for i, c := range chunks {
			retrieved[i] = c.ID
		}

		score := QuestionScore{
			QuestionID:   q.ID,
			RecallAtK:    RecallAtK(retrieved, expectedSet, params.TopK),
			PrecisionAtK: PrecisionAtK(retrieved, expectedSet, params.TopK),
			MRR:          MRR(retrieved, expectedSet),
		}
		result.PerQuestion = append(result.PerQuestion, score)
	}

	agg := Aggregate(result.PerQuestion)
	result.RecallAtK = agg.RecallAtK
	result.MRR = agg.MRR
	result.PrecisionAtK = agg.PrecisionAtK
	result.QuestionCount = agg.Count

	paramsJSON, _ := json.Marshal(params)
	s.DB().Exec(`INSERT INTO eval_runs(kb_id,params_json,recall_at_k,mrr,precision_at_k,question_count) VALUES(?,?,?,?,?,?)`,
		kbID, string(paramsJSON), result.RecallAtK, result.MRR, result.PrecisionAtK, result.QuestionCount)

	return result, nil
}

type RunRecord struct {
	ID            int64
	ParamsJSON    string
	RecallAtK     float64
	MRR           float64
	PrecisionAtK  float64
	QuestionCount int
	RunAt         string
}

func ListRuns(s *store.Store, kbID string) ([]RunRecord, error) {
	rows, err := s.DB().Query(`SELECT id,params_json,recall_at_k,mrr,precision_at_k,question_count,run_at FROM eval_runs WHERE kb_id=? ORDER BY id DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		var r RunRecord
		if err := rows.Scan(&r.ID, &r.ParamsJSON, &r.RecallAtK, &r.MRR, &r.PrecisionAtK, &r.QuestionCount, &r.RunAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/eval/eval.go internal/rag/eval/eval_test.go
git commit -m "feat(rag/eval): add Run for full KB evaluation with persistence"
```

---

### Phase 2C 检查点（评测 L1+L2 完成）

```bash
go test ./internal/rag/... -v
```
预期：标注测试问题、跑评测、看指标、查历史。Markdown 全链路 demo 跑通。

---

## T31：PDFChunker

**Files:**
- Modify: `internal/rag/chunker/pdf.go`
- Create: `internal/rag/chunker/pdf_test.go`

- [ ] **Step 1: 添加依赖**

```bash
go get github.com/ledongthuc/pdf@latest
```

- [ ] **Step 2: 写测试**

```go
// internal/rag/chunker/pdf_test.go
package chunker

import (
	"os"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
)

func TestPDFChunker_BasicExtraction(t *testing.T) {
	if _, err := os.Stat("testdata/sample.pdf"); err != nil {
		t.Skip("no sample.pdf testdata, skipping")
	}
	data, _ := os.ReadFile("testdata/sample.pdf")
	c := &PDFChunker{opts: Options{ChunkSize: 512}}
	chunks, err := c.Chunk(rag.RawDocument{FilePath: "sample.pdf", FileType: "pdf", Content: data})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks from PDF")
	}
}
```

- [ ] **Step 3: 实现 pdf.go**

```go
// internal/rag/chunker/pdf.go
package chunker

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/agentforge/agentforge/internal/rag"
	"github.com/ledongthuc/pdf"
)

type PDFChunker struct{ opts Options }

func (c *PDFChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	r := bytes.NewReader(doc.Content)
	_, reader, err := pdf.NewReader(r, int64(len(doc.Content)))
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}

	var chunks []rag.Chunk
	seq := 0
	for pageNum := 1; pageNum <= reader.NumPage(); pageNum++ {
		page := reader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		for _, para := range strings.Split(text, "\n\n") {
			para = strings.TrimSpace(para)
			if para == "" {
				continue
			}
			chunks = append(chunks, rag.Chunk{
				Content:    para,
				Source:     fmt.Sprintf("%s#page=%d", doc.FilePath, pageNum),
				TokenCount: EstimateTokens(para),
				Seq:        seq,
			})
			seq++
		}
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no text extracted (possibly scanned PDF without text layer)")
	}

	return applySizeLimit(chunks, c.opts), nil
}
```

- [ ] **Step 4: 准备 testdata 并运行测试**

```bash
mkdir internal/rag/chunker/testdata
# 用浏览器打印一段文字为 PDF 放入 testdata/sample.pdf
go test ./internal/rag/chunker/ -run TestPDF -v
```
Expected: PASS（若有 testdata，否则 skip）。

- [ ] **Step 5: Commit**

```bash
git add internal/rag/chunker/pdf.go internal/rag/chunker/pdf_test.go go.mod go.sum
git commit -m "feat(rag/chunker): add PDF chunker with paragraph splitting"
```

---

## T32：OfficeChunker（docx/pptx/xlsx）

**Files:**
- Modify: `internal/rag/chunker/office.go`
- Create: `internal/rag/chunker/office_test.go`

- [ ] **Step 1: 写测试**

```go
// internal/rag/chunker/office_test.go
package chunker

import (
	"os"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
)

func TestOfficeChunker_DOCX(t *testing.T) {
	if _, err := os.Stat("testdata/sample.docx"); err != nil {
		t.Skip("no sample.docx")
	}
	data, _ := os.ReadFile("testdata/sample.docx")
	c := &OfficeChunker{opts: Options{ChunkSize: 512}}
	chunks, err := c.Chunk(rag.RawDocument{FilePath: "sample.docx", FileType: "docx", Content: data})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
}

func TestOfficeChunker_PPTX(t *testing.T) {
	if _, err := os.Stat("testdata/sample.pptx"); err != nil {
		t.Skip("no sample.pptx")
	}
	data, _ := os.ReadFile("testdata/sample.pptx")
	c := &OfficeChunker{opts: Options{ChunkSize: 512}}
	chunks, err := c.Chunk(rag.RawDocument{FilePath: "sample.pptx", FileType: "pptx", Content: data})
	if err != nil {
		t.Fatalf("Chunk: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
}
```

- [ ] **Step 2: 实现 office.go**

```go
// internal/rag/chunker/office.go
package chunker

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/agentforge/agentforge/internal/rag"
)

type OfficeChunker struct{ opts Options }

func (c *OfficeChunker) Chunk(doc rag.RawDocument) ([]rag.Chunk, error) {
	switch doc.FileType {
	case "docx":
		return c.chunkDOCX(doc)
	case "pptx":
		return c.chunkPPTX(doc)
	case "xlsx":
		return c.chunkXLSX(doc)
	default:
		return nil, fmt.Errorf("unsupported office type: %s", doc.FileType)
	}
}

func readZipEntry(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("entry not found: %s", name)
}

func (c *OfficeChunker) chunkDOCX(doc rag.RawDocument) ([]rag.Chunk, error) {
	data, err := readZipEntry(doc.Content, "word/document.xml")
	if err != nil {
		return nil, err
	}
	paragraphs := extractDOCXParagraphs(data)
	var chunks []rag.Chunk
	for i, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		chunks = append(chunks, rag.Chunk{
			Content:    p,
			Source:     fmt.Sprintf("%s#para=%d", doc.FilePath, i),
			TokenCount: EstimateTokens(p),
			Seq:        i,
		})
	}
	return applySizeLimit(chunks, c.opts), nil
}

func extractDOCXParagraphs(xmlData []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(xmlData))
	var paragraphs []string
	var cur []string
	inP := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch e := tok.(type) {
		case xml.StartElement:
			if e.Name.Local == "p" && e.Name.Space == "http://schemas.openxmlformats.org/wordprocessingml/2006/main" {
				inP = true
				cur = nil
			} else if e.Name.Local == "t" && inP {
				var txt string
				dec.DecodeElement(&txt, &e)
				cur = append(cur, txt)
			}
		case xml.EndElement:
			if e.Name.Local == "p" && inP {
				inP = false
				paragraphs = append(paragraphs, strings.Join(cur, ""))
			}
		}
	}
	return paragraphs
}

func (c *OfficeChunker) chunkPPTX(doc rag.RawDocument) ([]rag.Chunk, error) {
	zr, err := zip.NewReader(bytes.NewReader(doc.Content), int64(len(doc.Content)))
	if err != nil {
		return nil, err
	}
	var slideFiles []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideFiles = append(slideFiles, f.Name)
		}
	}
	sort.Strings(slideFiles)

	var chunks []rag.Chunk
	for i, name := range slideFiles {
		rc, err := zr.Open(name)
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		text := extractPPTXText(data)
		if strings.TrimSpace(text) == "" {
			continue
		}
		chunks = append(chunks, rag.Chunk{
			Content:    strings.TrimSpace(text),
			Source:     fmt.Sprintf("%s#slide=%d", doc.FilePath, i+1),
			TokenCount: EstimateTokens(text),
			Seq:        i,
		})
	}
	return applySizeLimit(chunks, c.opts), nil
}

func extractPPTXText(xmlData []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(xmlData))
	var texts []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if e, ok := tok.(xml.StartElement); ok && e.Name.Local == "t" {
			var txt string
			dec.DecodeElement(&txt, &e)
			if txt != "" {
				texts = append(texts, txt)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func (c *OfficeChunker) chunkXLSX(doc rag.RawDocument) ([]rag.Chunk, error) {
	shared, _ := readSharedStrings(doc.Content)
	data, err := readZipEntry(doc.Content, "xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	rows := extractXLSXRows(data, shared)
	var chunks []rag.Chunk
	for i, row := range rows {
		if strings.TrimSpace(row) == "" {
			continue
		}
		chunks = append(chunks, rag.Chunk{
			Content:    row,
			Source:     fmt.Sprintf("%s#row=%d", doc.FilePath, i+1),
			TokenCount: EstimateTokens(row),
			Seq:        i,
		})
	}
	return chunks, nil
}

func readSharedStrings(zipData []byte) ([]string, error) {
	data, err := readZipEntry(zipData, "xl/sharedStrings.xml")
	if err != nil {
		return nil, err
	}
	var strs []string
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if e, ok := tok.(xml.StartElement); ok && e.Name.Local == "t" {
			var s string
			dec.DecodeElement(&s, &e)
			strs = append(strs, s)
		}
	}
	return strs, nil
}

func extractXLSXRows(xmlData []byte, shared []string) []string {
	dec := xml.NewDecoder(bytes.NewReader(xmlData))
	var rows []string
	var rowCells []string
	inRow := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch e := tok.(type) {
		case xml.StartElement:
			if e.Name.Local == "row" {
				inRow = true
				rowCells = nil
			} else if e.Name.Local == "v" && inRow {
				var v string
				dec.DecodeElement(&v, &e)
				idx := 0
				fmt.Sscanf(v, "%d", &idx)
				if idx >= 0 && idx < len(shared) {
					rowCells = append(rowCells, shared[idx])
				}
			}
		case xml.EndElement:
			if e.Name.Local == "row" && inRow {
				inRow = false
				rows = append(rows, strings.Join(rowCells, ", "))
			}
		}
	}
	return rows
}
```

- [ ] **Step 3: 运行测试**（需 testdata，否则 skip）

- [ ] **Step 4: Commit**

```bash
git add internal/rag/chunker/office.go internal/rag/chunker/office_test.go
git commit -m "feat(rag/chunker): add Office chunker (docx/pptx/xlsx) via zip+xml"
```

---

## T33：judge.go（LLM-as-judge）

**Files:**
- Create: `internal/rag/eval/judge.go`
- Create: `internal/rag/eval/judge_test.go`

- [ ] **Step 1: 写测试（mock HTTP）**

```go
// internal/rag/eval/judge_test.go
package eval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJudge_Relevance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "relevant"}},
			},
		})
	}))
	defer srv.Close()

	j := NewJudge(srv.URL, "sk-fake", "gpt-4o-mini")
	score, err := j.Judge("什么数据库", "AgentForge 用 sqlite-vec")
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if score != Relevant {
		t.Errorf("expected Relevant, got %v", score)
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 judge.go**

```go
// internal/rag/eval/judge.go
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Relevance int

const (
	Irrelevant Relevance = iota
	PartiallyRelevant
	Relevant
)

func parseRelevance(s string) Relevance {
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "partially"):
		return PartiallyRelevant
	case strings.Contains(lower, "irrelevant"):
		return Irrelevant
	case strings.Contains(lower, "relevant"):
		return Relevant
	default:
		return Irrelevant
	}
}

type Judge struct {
	BaseURL string
	APIKey  string
	Model   string
}

func NewJudge(baseURL, apiKey, model string) *Judge {
	return &Judge{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func (j *Judge) Judge(question, chunkContent string) (Relevance, error) {
	prompt := fmt.Sprintf(`判断以下资料是否与问题相关。只回答一个词：relevant / partially / irrelevant。

问题：%s
资料：%s`, question, chunkContent)

	body, _ := json.Marshal(map[string]any{
		"model": j.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, _ := http.NewRequest("POST", j.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+j.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Irrelevant, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return Irrelevant, fmt.Errorf("judge API status %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Irrelevant, err
	}
	if len(parsed.Choices) == 0 {
		return Irrelevant, fmt.Errorf("no choices in judge response")
	}
	return parseRelevance(parsed.Choices[0].Message.Content), nil
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/eval/judge.go internal/rag/eval/judge_test.go
git commit -m "feat(rag/eval): add LLM-as-judge for relevance scoring"
```

---

## T34：generator.go（LLM 自动生成测试集）

**Files:**
- Create: `internal/rag/eval/generator.go`
- Create: `internal/rag/eval/generator_test.go`

- [ ] **Step 1: 写测试**

```go
// internal/rag/eval/generator_test.go
package eval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
	"github.com/agentforge/agentforge/internal/rag/store"
)

func TestGenerateQuestions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "1. AgentForge 用什么数据库？\n2. 切片默认多大？"}},
			},
		})
	}))
	defer srv.Close()

	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := store.New(dbPath, 8)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("t", "m", 512, 50)
	ids, _ := s.SaveChunks(
		[]rag.Chunk{{DocID: "d1", KBID: kbID, Content: "AgentForge 用 sqlite-vec，切片默认 512。"}},
		[][]float32{{1, 0, 0, 0, 0, 0, 0, 0}},
	)

	g := NewGenerator(srv.URL, "sk-fake", "gpt-4o-mini")
	n, err := g.Generate(s, kbID, ids, 3)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 questions, got %d", n)
	}
}
```

- [ ] **Step 2: 运行确认失败** → Expected: FAIL

- [ ] **Step 3: 实现 generator.go**

```go
// internal/rag/eval/generator.go
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/agentforge/agentforge/internal/rag/store"
)

type Generator struct {
	BaseURL string
	APIKey  string
	Model   string
}

func NewGenerator(baseURL, apiKey, model string) *Generator {
	return &Generator{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func (g *Generator) Generate(s *store.Store, kbID string, chunkIDs []int64, perChunk int) (int, error) {
	if perChunk <= 0 {
		perChunk = 3
	}
	total := 0
	for _, cid := range chunkIDs {
		ch, err := GetChunk(s, cid)
		if err != nil {
			continue
		}
		questions, err := g.askLLM(ch.Content, perChunk)
		if err != nil {
			continue
		}
		for _, q := range questions {
			qID, err := AddQuestion(s, kbID, q, "llm_generated")
			if err != nil {
				continue
			}
			SetExpected(s, qID, []int64{cid})
			total++
		}
	}
	return total, nil
}

func (g *Generator) askLLM(content string, n int) ([]string, error) {
	prompt := fmt.Sprintf("针对以下内容，生成 %d 个用户可能会问的问题，每行一个，用数字编号：\n\n%s", n, content)
	body, _ := json.Marshal(map[string]any{
		"model": g.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, _ := http.NewRequest("POST", g.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("generate API status %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no choices")
	}
	return parseQuestions(parsed.Choices[0].Message.Content), nil
}

var numPrefix = regexp.MustCompile(`^\s*\d+[.、)]\s*`)

func parseQuestions(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = numPrefix.ReplaceAllString(line, "")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
```

- [ ] **Step 4: 运行确认通过** → PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rag/eval/generator.go internal/rag/eval/generator_test.go
git commit -m "feat(rag/eval): add LLM-powered test question generator"
```

---

### Phase 2D 检查点（评测 L3 + 全格式完成）

```bash
go test ./internal/rag/... -v
```
预期：后端 RAG 全部完成（PDF/Office 切片 + L3 judge + 自动测试集）。

---

## T35：Wails Binding（暴露 RAG 方法）

**目标：** 把 RAG Service 暴露给前端。

> **前置：** 需先 `wails init -n . -t react-ts` 生成 Wails 工程结构（cmd/gui/, frontend/, wails.json）。
> ```bash
> go install github.com/wailsapp/wails/v2/cmd/wails@latest
> wails init -n . -t react-ts
> ```

**Files:**
- Modify: `cmd/gui/app.go`

- [ ] **Step 1: 实现 binding**

```go
// cmd/gui/app.go
package app

import (
	"context"

	"github.com/agentforge/agentforge/internal/rag"
	"github.com/agentforge/agentforge/internal/rag/embedder"
	"github.com/agentforge/agentforge/internal/rag/eval"
)

type App struct {
	ctx    context.Context
	ragSvc *rag.Service
	cfg    Config
}

type Config struct {
	BaseURL    string
	APIKey     string
	EmbedModel string
	EmbedDim   int
	ChatModel  string
	DBPath     string
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) initRagSvc(cfg Config) error {
	emb := embedder.NewOpenAIEmbedder(cfg.BaseURL, cfg.APIKey, cfg.EmbedModel)
	svc, err := rag.NewService(rag.ServiceConfig{
		DBPath: cfg.DBPath, EmbedDim: cfg.EmbedDim,
		Embedder: emb, EmbeddingModel: cfg.EmbedModel,
	})
	if err != nil {
		return err
	}
	if a.ragSvc != nil {
		a.ragSvc.Close()
	}
	a.ragSvc = svc
	a.cfg = cfg
	return nil
}

// === 知识库管理 ===
func (a *App) CreateKnowledgeBase(name string, chunkSize, overlap int) (string, error) {
	return a.ragSvc.CreateKnowledgeBase(name, chunkSize, overlap)
}

func (a *App) ListKnowledgeBases() ([]rag.KnowledgeBase, error) {
	return a.ragSvc.ListKnowledgeBases()
}

func (a *App) ImportDocument(kbID, filePath string) (rag.ImportResult, error) {
	return a.ragSvc.ImportDocument(kbID, filePath)
}

// === 检索 + 对话 ===
func (a *App) Retrieve(kbID, query string, topK int) ([]rag.ScoredChunk, error) {
	return a.ragSvc.Retrieve(kbID, query, topK)
}

func (a *App) ChatWithRAG(kbID, query string) error {
	chunks, err := a.ragSvc.Retrieve(kbID, query, 5)
	if err != nil {
		return err
	}
	prompt := a.ragSvc.BuildPrompt(query, chunks)
	// 流式 LLM 调用接入点：复用 internal/llm/ 的 Provider
	// llm.StreamChat(a.ctx, a.cfg.ChatModel, prompt, func(token) { runtime.EventsEmit(a.ctx, "chat:chunk", token) })
	_ = prompt
	return nil
}

// === 评测 ===
func (a *App) RunEvaluation(kbID string, topK, chunkSize int) (eval.EvalResult, error) {
	return eval.Run(a.ragSvc.Store(), a.ragSvc.Embedder(), kbID, eval.EvalParams{TopK: topK, ChunkSize: chunkSize})
}

func (a *App) ListEvalRuns(kbID string) ([]eval.RunRecord, error) {
	return eval.ListRuns(a.ragSvc.Store(), kbID)
}

func (a *App) AddEvalQuestion(kbID, question string, expectedChunkIDs []int64) error {
	qID, err := eval.AddQuestion(a.ragSvc.Store(), kbID, question, "manual")
	if err != nil {
		return err
	}
	return eval.SetExpected(a.ragSvc.Store(), qID, expectedChunkIDs)
}

func (a *App) ListEvalQuestions(kbID string) ([]eval.Question, error) {
	return eval.ListQuestions(a.ragSvc.Store(), kbID)
}

func (a *App) GenerateEvalQuestions(kbID string, chunkIDs []int64, perChunk int) (int, error) {
	g := eval.NewGenerator(a.cfg.BaseURL, a.cfg.APIKey, a.cfg.ChatModel)
	return g.Generate(a.ragSvc.Store(), kbID, chunkIDs, perChunk)
}
```

> **注意：** `ChatWithRAG` 的流式 LLM 调用依赖 `internal/llm/` 的真实 Provider 实现（Phase 1 仅有接口+Fake）。接入点标注清楚，不阻塞 RAG/评测功能。

- [ ] **Step 2: 验证编译** → `go build ./cmd/gui/`

- [ ] **Step 3: Commit**

```bash
git add cmd/gui/app.go
git commit -m "feat(gui): expose RAG bindings"
```

---

## T36：前端知识库页面

**Files:**
- Create: `frontend/src/pages/KnowledgeBase.tsx`
- Create: `frontend/src/types/rag.ts`
- Create: `frontend/src/api/rag.ts`

- [ ] **Step 1: 实现 KnowledgeBase.tsx**

```tsx
// frontend/src/pages/KnowledgeBase.tsx
import { useState, useEffect } from 'react';
import { KnowledgeBase, ImportResult, ScoredChunk } from '../types/rag';
import { CreateKnowledgeBase, ListKnowledgeBases, ImportDocument, Retrieve } from '../api/rag';

export function KnowledgeBasePage() {
  const [kbs, setKbs] = useState<KnowledgeBase[]>([]);
  const [selectedKb, setSelectedKb] = useState<string>('');
  const [newName, setNewName] = useState('');
  const [filePath, setFilePath] = useState('');
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<ScoredChunk[]>([]);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);

  useEffect(() => { refreshKbs(); }, []);

  async function refreshKbs() {
    const list = await ListKnowledgeBases();
    setKbs(list || []);
    if (list && list.length > 0 && !selectedKb) setSelectedKb(list[0].id);
  }

  async function handleCreate() {
    if (!newName.trim()) return;
    await CreateKnowledgeBase(newName, 512, 50);
    setNewName('');
    refreshKbs();
  }

  async function handleImport() {
    if (!selectedKb || !filePath) return;
    const r = await ImportDocument(selectedKb, filePath);
    setImportResult(r);
  }

  async function handleSearch() {
    if (!selectedKb || !query) return;
    const r = await Retrieve(selectedKb, query, 5);
    setResults(r || []);
  }

  return (
    <div className="kb-page">
      <h2>知识库管理</h2>
      <section className="kb-list">
        <h3>我的知识库</h3>
        <select value={selectedKb} onChange={(e) => setSelectedKb(e.target.value)}>
          {kbs.map((kb) => <option key={kb.id} value={kb.id}>{kb.name}</option>)}
        </select>
        <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="新知识库名" />
        <button onClick={handleCreate}>创建</button>
      </section>
      <section className="kb-import">
        <h3>导入文档</h3>
        <input value={filePath} onChange={(e) => setFilePath(e.target.value)} placeholder="文件路径" />
        <button onClick={handleImport}>导入</button>
        {importResult && (
          <div>状态：{importResult.status}，切片数：{importResult.chunkCount}{importResult.skipped && '（已存在，跳过）'}</div>
        )}
      </section>
      <section className="kb-test">
        <h3>检索测试</h3>
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="问个问题" />
        <button onClick={handleSearch}>检索</button>
        <ul className="results">
          {results.map((r) => (
            <li key={r.id}>
              <span className="score">{(r.score * 100).toFixed(1)}%</span>
              {r.headingPath && <em>[{r.headingPath}]</em>}
              {r.content}
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
```

- [ ] **Step 2: 类型与 API 封装**

```ts
// frontend/src/types/rag.ts
export interface KnowledgeBase {
  id: string; name: string; embeddingModel: string;
  chunkSize: number; overlap: number; createdAt: string;
}
export interface ImportResult {
  docId: string; status: string; chunkCount: number;
  skipped: boolean; errorMsg?: string;
}
export interface ScoredChunk {
  id: number; content: string; headingPath: string;
  source: string; tokenCount: number; score: number;
}
```

```ts
// frontend/src/api/rag.ts
const isWails = typeof window !== 'undefined' && (window as any).go?.main?.App;

export async function ListKnowledgeBases() {
  if (isWails) return (window as any).go.main.App.ListKnowledgeBases();
  return [];
}
export async function CreateKnowledgeBase(name: string, chunkSize: number, overlap: number) {
  if (isWails) return (window as any).go.main.App.CreateKnowledgeBase(name, chunkSize, overlap);
}
export async function ImportDocument(kbId: string, path: string) {
  if (isWails) return (window as any).go.main.App.ImportDocument(kbId, path);
  return { status: 'mock', chunkCount: 0, skipped: false };
}
export async function Retrieve(kbId: string, query: string, topK: number) {
  if (isWails) return (window as any).go.main.App.Retrieve(kbId, query, topK);
  return [];
}
```

- [ ] **Step 3: 在 App.tsx 挂载页面**

- [ ] **Step 4: 验证前端编译** → `cd frontend && npm run build`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/KnowledgeBase.tsx frontend/src/types/rag.ts frontend/src/api/rag.ts frontend/src/App.tsx
git commit -m "feat(frontend): add KnowledgeBase page"
```

---

## T37：前端评测页面

**Files:**
- Create: `frontend/src/pages/Evaluation.tsx`
- Create: `frontend/src/types/eval.ts`
- Create: `frontend/src/api/eval.ts`

- [ ] **Step 1: 实现 Evaluation.tsx**（详见原 rag-plan T28 的完整实现，结构：控制栏 + 三栏）

- [ ] **Step 2: 类型与 API 封装**

```ts
// frontend/src/types/eval.ts
export interface EvalResult {
  recallAtK: number; mrr: number; precisionAtK: number; questionCount: number;
}
export interface EvalQuestion { id: number; question: string; source: string; }
export interface EvalRun {
  id: number; paramsJson: string; recallAtK: number; mrr: number;
  precisionAtK: number; questionCount: number; runAt: string;
}
```

```ts
// frontend/src/api/eval.ts
const isWails = typeof window !== 'undefined' && (window as any).go?.main?.App;
export async function ListKnowledgeBases() {
  if (isWails) return (window as any).go.main.App.ListKnowledgeBases();
  return [];
}
export async function ListEvalQuestions(kbId: string) {
  if (isWails) return (window as any).go.main.App.ListEvalQuestions(kbId);
  return [];
}
export async function RunEvaluation(kbId: string, topK: number, chunkSize: number) {
  if (isWails) return (window as any).go.main.App.RunEvaluation(kbId, topK, chunkSize);
  return { recallAtK: 0, mrr: 0, precisionAtK: 0, questionCount: 0 };
}
export async function ListEvalRuns(kbId: string) {
  if (isWails) return (window as any).go.main.App.ListEvalRuns(kbId);
  return [];
}
export async function AddEvalQuestion(kbId: string, q: string, expected: number[]) {
  if (isWails) return (window as any).go.main.App.AddEvalQuestion(kbId, q, expected);
}
export async function Retrieve(kbId: string, query: string, topK: number) {
  if (isWails) return (window as any).go.main.App.Retrieve(kbId, query, topK);
  return [];
}
```

- [ ] **Step 3: Evaluation.tsx 页面实现**（三栏布局：问题列表 + 对话测试 + 指标面板，参考 spec R.4.6 的 ASCII 草图）

- [ ] **Step 4: 验证编译** → `cd frontend && npm run build`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/Evaluation.tsx frontend/src/types/eval.ts frontend/src/api/eval.ts frontend/src/App.tsx
git commit -m "feat(frontend): add Evaluation page (metrics + chat test + history)"
```

---

## T38 & T39：端到端验收

> 这两个任务编号预留给端到端联调与验收测试，视实际 Wails 工程进度调整。

**T38: 后端全量测试 + 前端编译**

```bash
go test ./... -v
cd frontend && npm run build && cd ..
```

**T39: Wails 端到端验收（手动）**

```bash
wails build
```

端到端 demo 验收步骤：
1. `wails dev` 启动桌面应用
2. 设置页填入 base_url / api_key / embedding 模型 / 维度
3. 知识库页：创建知识库 → 导入 .md 文件 → 看到切片数
4. 知识库页：检索测试，输入问题，看到 top-5 相关片段
5. 评测页：添加 3-5 个测试问题 + 期望命中 → 点「运行评测」→ 看到 Recall@5 / MRR / Precision@5
6. 评测页：对话测试，输入问题，看召回片段
7. 评测页：历史评测列表

---

# 全局里程碑与检查点总览

| Phase | 任务 | 内容 | 检查点验证 |
|-------|------|------|-----------|
| **Phase 1** | T1-T11 | Tool 抽象 + Agent Loop + Command | `go test ./...` 全绿，集成测试通过 |
| **Phase 2A** | T12-T18 | RAG 存储层 + CRUD | 插入向量 + 余弦检索 + 知识库过滤 |
| **Phase 2B** | T19-T27 | 切片器(MD) + 导入 + 检索 + Service | 导入 .md → 检索 → prompt 组装闭环 |
| **Phase 2C** | T28-T30 | 评测 L1+L2 | 标注问题 → 跑评测 → 看指标 → 查历史 |
| **Phase 2D** | T31-T34 | PDF/Office 切片 + L3 judge + 自动测试集 | 全格式 + LLM 评测可用 |
| **GUI** | T35-T39 | Wails binding + 前端两页面 + 验收 | 桌面端可操作完整 demo |

---

# 全局风险登记簿

| # | 风险 | 影响任务 | 缓解措施 |
|---|------|---------|---------|
| 1 | **modernc.org/sqlite 的 sqlite-vec 支持** | T15（阻塞性） | 必须先验证 `SELECT vec_version()` 通过。若不支持，升级版本或换 ncruces 方案。这是整个 RAG 的技术地基 |
| 2 | sqlite-vec KNN 查询语法版本差异 | T17 | `MATCH ... AND k=?` vs `ORDER BY distance`，以实际版本官方文档为准 |
| 3 | PDF/Office 测试依赖 testdata | T31, T32 | 测试用 `t.Skip` 兜底，无 testdata 不阻塞。需手动准备样本 |
| 4 | Wails 工程脚手架缺失 | T35 | 项目当前无 Wails 结构，需先 `wails init`。这是 GUI 模式前置依赖 |
| 5 | 流式对话接入 | T35 `ChatWithRAG` | 依赖 `internal/llm/` 真实 Provider。Phase 1 仅有接口+Fake。RAG/评测功能不受影响，仅对话流推送待接入 |
| 6 | embedding 维度跨模型不一致 | 运行时 | Service 初始化探测维度建库；跨模型检索需 binding 层校验并提示重建 |
| 7 | T3 与 T6 的 Manager 演进 | T3→T6 | T3 先写简化版 ForRequest（直接返回），T6 替换为含压缩的版本。注意 T6 的 NewManager 签名已是 variadic opts |

---

**文档结束。** 按顺序从 T1 执行到 T39，每个 Task 严格遵循 TDD 五步循环，每个 Phase 结束跑检查点。遇风险登记簿中的项优先验证。
