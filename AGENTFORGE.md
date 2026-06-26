# AgentForge

一个本地优先（local-first）的 AI Agent 桌面应用：在 Wails（Go 后端 + React/TS 前端）中运行一个配备工具集、知识库、技能与跨会话记忆的智能体，所有数据落在本地 SQLite，无需云端。

## 特性

- **多模型供应商**：OpenAI 兼容协议，可配置多个 Provider（对话 / 嵌入），按对话切换。
- **Agent 工具循环**：多轮工具调用、流式输出、工具确认门控（手动 / 自动）、调用次数硬上限。
- **内置工具**：`bash`、`file_read`、`file_write`、`file_edit`、`grep`，在当前工作目录操作。
- **MCP 扩展**：接入 Model Context Protocol 服务器，补充视觉、联网搜索、网页阅读等能力。
- **Skills 技能系统**：可启用的技能指令注入。
- **RAG 知识库**：文档入库、分块、向量检索（vec0），对话时按相似度阈值注入。
- **@ 文件 mention**：把工作目录中的文件 / 文件夹加入对话上下文。
- **跨会话记忆**：Agent 自主记录「值得长期记住的事实」，每轮对话注入记忆索引，可在设置页管理。
- **计划模式**：只读调研 + 产出结构化实施计划。
- **明暗主题**：语义令牌设计系统，零 emoji，统一 Lucide 风图标。

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.26、Wails v2、chi v5、mattn/go-sqlite3（CGO + vec0） |
| 前端 | React、TypeScript、Vite、zustand、Tailwind |
| 通信 | 前端 ↔ 后端：in-process HTTP + SSE（不使用 Wails Bindings 传业务数据） |

## 项目结构

```
main.go                  # Wails 桌面入口（embed 前端 + 内嵌 core HTTP 服务）
cmd/core/                # core 服务入口（CLI / 调试，make run）
internal/
  agent/                 # Agent 编排：工具循环、系统提示词分层注入
  llm/                   # OpenAI 兼容客户端（chat / embed / 流式 / 重试）
  memory/                # 跨会话记忆：markdown 文件 + 索引 + 读写工具
  mcp/                   # MCP 服务器管理
  rag/                   # 知识库向量检索（分块、嵌入、cosine 召回）
  server/                # chi HTTP API + SSE 处理器
  skills/                # 技能系统
  store/                 # SQLite 数据层（schema + 增量迁移）
  tools/                 # 内置工具 + 确认门控
frontend/                # React/TS 前端（stores / components / lib）
```

## 快速开始

### 环境要求

- Go ≥ 1.26（若用 `.g` 等版本管理器，确保 GOROOT 与 toolchain 版本一致）
- Node.js（前端构建）
- Wails CLI：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- CGO（go-sqlite3 需要，macOS 自带 clang 即可）

### 安装与运行

```bash
# 安装依赖
go mod tidy
cd frontend && npm install && cd ..

# 桌面应用开发模式（热重载 + WebView 窗口）
make dev

# 或仅运行 core 服务（CLI）
make run
```

首次启动后，在「设置 → 模型」配置一个 OpenAI 兼容供应商（BaseURL / APIKey / 模型名），即可开始对话。

## 开发命令

| 命令 | 说明 |
|---|---|
| `make dev` | Wails 桌面开发模式（前端 HMR + Go 热重载） |
| `make run` | 运行 core 服务（cmd/core） |
| `make build` | 编译全部 Go 包（带 `sqlite_load_extension` tag） |
| `make test` | 运行全部 Go 测试 |
| `make tidy` | `go mod tidy` |

> 所有构建 / 测试都带 `-tags sqlite_load_extension`——vec0 向量扩展必需，不加会在运行时报 `no such module: vec0`（见 Makefile）。

## 数据存储

- 应用数据：`~/Library/Application Support/agent-rust/`（macOS），含 `app.db`（SQLite）与 `port.lock`。
- 跨会话记忆：`<工作目录>/.agentforge/memory/`（跟随工作目录，可纳入项目 git）；无工作目录时回退到应用数据目录。
