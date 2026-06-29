# AgentForge

一个本地优先（local-first）的 AI Agent 桌面应用：在 Wails（Go 后端 + React/TS 前端）中运行一个配备工具集、知识库、技能、项目规则与跨会话记忆的智能体，所有数据落在本地 SQLite，无需云端。

## 特性

### 核心能力

- **多模型供应商**：OpenAI 兼容协议，可配置多个 Provider（对话 / 嵌入），支持视觉（VL）模型与纯文本模型分类，按对话切换。
- **Agent 工具循环**：多轮工具调用、流式输出、工具确认门控（手动 / 自动放行 / 会话内记忆）、调用次数硬上限。
- **内置工具**：`bash`、`file_read`、`file_write`、`file_edit`、`grep`、`read_skill`，在当前工作目录操作；另有 `todo_create/update/list/delete`（任务跟踪）、`dispatch_agent`（派生子 agent）与 `ask_user`（拿不准时向用户抛结构化选项）。
- **MCP 扩展**：接入 Model Context Protocol 服务器，补充视觉理解、联网搜索、网页阅读等能力。按会话临时限定可用 MCP server。
- **上下文窗口管理**：自动裁剪较早的 tool 输出（保留配对完整性），必要时自动摘要历史对话，防长会话爆炸。
- **子 agent（多 agent 协作）**：主 agent 通过 `dispatch_agent` 派生命名子 agent——`explorer`（只读探索）、`reviewer`（代码审查）、`planner`（产出实施计划），各自独立上下文窗口 + 只读工具白名单，只把结果摘要回传。对标 Claude Code / opencode Subagents。
- **Todo 任务跟踪**：复杂多步任务用 `todo_create/update/list/delete` 显式跟踪进度（pending→in_progress→completed，一次只允许一个进行中，支持 blocks/blockedBy 任务依赖），右侧面板通过 SSE 实时显示进度看板。

### 知识与记忆

- **RAG 知识库**：文档入库（PDF / DOCX / XLSX 等）、分层语义切分（代码块/表格保护、中文 rune 安全）、内容去重（sha256 跳过重复文档）、**query 改写扩展 + 混合检索**（chat 模型生成子查询 → sqlite-vec 向量 + FTS5 trigram 全文，跨 query RRF 融合）、可选 **rerank 重排**（Jina/Cohere 兼容），对话时按相似度阈值注入，低质量片段自动过滤。
- **Skills 技能系统**：全局 + 工作目录两层技能，精简索引常驻注入，模型按需 `read_skill` 加载全文，降低常驻 token 开销。
- **项目规则**：`AGENTFORGE.md`（全局 `~/.agentforge/` + 项目根）两层规则自动注入；支持兼容导入 `CLAUDE.md` / `AGENTS.md`（可开关）。
- **跨会话记忆**：Agent 自主记录「值得长期记住的事实」，以 markdown（frontmatter + 正文）存储，每轮对话注入记忆索引，可在设置页管理。四种类型：用户偏好 / 工作指导 / 项目约束 / 外部资源。

### 交互体验

- **@ 文件 mention**：把工作目录中的文件 / 文件夹加入对话上下文。
- **/ 技能选择**：通过斜杠菜单快速勾选技能。
- **计划模式**：只读调研 + 产出结构化实施计划，禁止写入。
- **结构化提问（ask_user）**：Agent 遇到必须用户拍板、且无合理默认的决策时，主动给出 2~4 个选项让用户单选或填「其他」，对标 Claude Code 的 AskUserQuestion；与工具确认门控共用「阻塞→SSE→HTTP 回传→恢复」链路。
- **视觉模型支持**：VL 模型可直接粘贴图片，模型直接看图作答，不重复调用图片识别类 MCP。
- **推理过程展示**：支持显示推理模型（reasoning model）的思考过程，可折叠。
- **内嵌终端**：在应用内打开终端面板，与当前工作目录同步。
- **token 速率估算**：实时估算 + 最终统计生成速率（tokens/s）。
- **明暗主题**：语义令牌设计系统，零 emoji，统一 Lucide 风图标。

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.26、Wails v2、chi v5、mattn/go-sqlite3（CGO + vec0） |
| 前端 | React 18、TypeScript、Vite、zustand、Tailwind CSS |
| 通信 | 前端 ↔ 后端：in-process HTTP + SSE（不使用 Wails Bindings 传业务数据） |
| 向量 / 全文 | sqlite-vec vec0（向量，CGO 动态加载）+ FTS5 trigram（全文检索） |
| 文档解析 | ledongthuc/pdf（PDF）、excelize（XLSX）、pdfcpu（PDF 处理） |

## 项目结构

```
main.go                  # Wails 桌面入口（embed 前端 + 内嵌 core HTTP 服务）
cmd/core/                # core 服务入口（CLI / 调试，make run）
internal/
  agent/                 # Agent 编排：工具循环、系统提示词分层注入、上下文压缩、子 agent 派生（dispatch.go）
  llm/                   # OpenAI 兼容客户端（chat / embed / 流式 / 重试）
  memory/                # 跨会话记忆：markdown 文件 + frontmatter + 索引 + 读写工具
  mcp/                   # MCP 服务器管理（stdio 客户端 + 工具挂载）
  rag/                   # 知识库检索（分层切分、嵌入、向量+FTS5 双路 RRF 融合、可选 rerank）
    parser/              # 文档解析器（PDF / DOCX / XLSX → 纯文本）
  rules/                 # 项目规则（AGENTFORGE.md 全局+项目，兼容 CLAUDE.md/AGENTS.md）
  server/                # chi HTTP API + SSE 处理器
  skills/                # 技能系统（全局 + 工作目录，索引注入 + 按需加载）
  store/                 # SQLite 数据层（schema + vec0 向量表 + FTS5 全文表 + 增量迁移）
  todo/                  # 会话内待办任务（状态机 + 任务依赖 + SSE 进度推送）
  tools/                 # 内置工具引擎 + 确认门控（Gate）+ 结构化提问（Asker）
    builtin/             # bash / file_read / file_write / file_edit / grep / read_skill
frontend/                # React/TS 前端
  src/
    components/          # ChatView / Sidebar / SettingsModal / KnowledgeWorkbench 等
    stores/              # zustand stores（session / config / theme / memory / rules 等）
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
| `make build` | 编译全部 Go 包（带 `sqlite_load_extension fts5` tag） |
| `make test` | 运行全部 Go 测试 |
| `make tidy` | `go mod tidy` |

> 所有构建 / 测试都带 `-tags "sqlite_load_extension fts5"`——vec0 向量扩展与 FTS5 全文检索必需，不加会在运行时报 `no such module: vec0` / `no such module: fts5`（见 Makefile）。

## 架构设计

### 前后端通信

桌面模式下，Go 后端在 `127.0.0.1` 随机端口启动 chi HTTP 服务，前端通过 in-process HTTP（REST API）+ SSE 与后端通信。Wails Bindings 仅用于传递端口号和原生目录选择器，不传输业务数据。

### 系统提示词分层注入

每轮对话按优先级从高到低注入多层 system context：

```
计划模式提示词（如启用）
  ↓
视觉能力提示词（VL 模型 + 含图片时）
  ↓
基础工具路由策略（内置工具 vs MCP 工具的选择规则）
  ↓
项目规则（AGENTFORGE.md / CLAUDE.md / AGENTS.md）
  ↓
跨会话记忆索引
  ↓
技能索引（精简列表，按需 read_skill 加载全文）
  ↓
RAG 知识库片段（相似度 ≥ 0.3 的片段）
```

### 上下文窗口管理

长会话自动压缩，两级策略按需触发：

1. **裁剪**（Prune）：倒序截断较早的 tool 输出正文（保留消息配对完整性），保留头部前 1500 字符。
2. **摘要**（Summarize）：裁剪后仍超预算时，将更早的整段对话压成文本摘要注入 system，保留尾部含最近 tool 配对的原文。

### 工具确认门控（Gate）

- **自动放行**：全局开关，跳过所有确认。
- **记忆规则**：用户对某工具+参数选择「本次会话 / 永远允许」后，后续匹配的调用自动通过。
- **手动确认**：通过 SSE 推送确认请求到前端，用户决定后通过 HTTP 回传。

### 结构化提问（ask_user）

与工具确认门控对称的另一条「阻塞→SSE→HTTP 回传→恢复」链路，但语义独立：Agent 拿不准、需用户拍板时调用 `ask_user`（与 `dispatch_agent` 一样在工具循环里特判，不走工具引擎），经独立的 `Asker` 发 `ask_user_req` 事件——`Asker` 不受 Gate 的自动放行 / 记忆规则影响（问题必须到达用户，否则模型拿不到答案）。前端 `AskUserDialog` 让用户单选某项或填「其他」，答案作为 `tool_result` 回传模型，循环继续。子 agent 不暴露此工具。

## 数据存储

| 数据 | 位置 | 说明 |
|---|---|---|
| 应用数据库 | `~/Library/Application Support/agent-rust/app.db`（macOS） | SQLite，含 sessions、messages、providers、knowledge_bases、chunks、settings 等表 |
| 端口锁 | `~/Library/Application Support/agent-rust/port.lock` | 当前 core HTTP 服务端口 |
| 跨会话记忆 | `<工作目录>/.agentforge/memory/` | 跟随工作目录，可纳入项目 git；无工作目录时回退到应用数据目录 |
| MCP 配置 | `~/.agentforge/mcp.json` | MCP server 列表（命令、参数、环境变量、启用状态） |
| 全局技能 | `~/.agentforge/skills/` | 所有项目共享的技能 |
| 工作目录技能 | `<工作目录>/.agentforge/skills/` | 项目专属技能 |
| 全局规则 | `~/.agentforge/AGENTFORGE.md` | 所有项目共享的行为规则 |
| 项目规则 | `<工作目录>/AGENTFORGE.md` | 项目专属行为规则 |

## License

MIT
