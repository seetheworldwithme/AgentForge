# Agent 客户端平台 - 设计文档

**状态**: 已确认，待实现
**日期**: 2026-06-16
**类型**: 架构设计 / 需求规格

---

## 1. 项目概述

### 1.1 定位

构建一个**本地单机运行的 AI Agent 桌面客户端**，形态类似 Claude Code / Cursor。用户接入自带的大模型 API，实现：

1. **LLM 对话**：接入用户自有的 OpenAI 兼容协议 API，进行多轮对话
2. **RAG 知识库**：上传个人文档，向量化后检索，对知识库进行问答
3. **本地 Tool 执行**：让 Agent 使用 bash、file read/write/edit、grep 等工具操作本地电脑，编写和运行代码

无需多租户、用户系统、计费——纯本地应用，数据全部存在用户机器上。

### 1.2 核心决策汇总

| 维度 | 决策 | 理由 |
|------|------|------|
| 产品定位 | 本地单机客户端 | 类 Claude Code，无需服务端 |
| 技术栈 | **Wails + Go**（后端）+ React（前端） | Go 生态成熟，单二进制分发 |
| 架构形态 | **嵌入式 HTTP Server** | Go 核心服务化，UI/CLI/SDK 均为客户端 |
| LLM 接入 | 仅 OpenAI 兼容协议 | 一个适配器覆盖几乎所有主流厂商 |
| Embedding | 云端 Embedding API | 与"自带 API"定位一致 |
| 向量存储 | **SQLite + sqlite-vec** | 单文件零运维，匹配本地场景 |
| Tool 安全 | **人工确认模式** | 危险操作弹窗，可临时/永久允许 |

---

## 2. 整体架构

### 2.1 分层视图

```
┌─────────────────────────────────────────────────────────────────┐
│                         客户端层                                  │
│  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐    │
│  │  Wails GUI     │  │  CLI（未来）    │  │  SDK（未来）    │    │
│  │  React + Vite  │  │  cobra         │  │  Go pkg        │    │
│  └───────┬────────┘  └───────┬────────┘  └───────┬────────┘    │
│          │   localhost HTTP + SSE (127.0.0.1:随机端口)            │
├──────────┼──────────────────────┼─────────────────┼─────────────┤
│          ▼                      ▼                 ▼             │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    API Gateway 层（chi router）             │  │
│  │   /api/chat · /api/kb · /api/tools · /api/config · /events │  │
│  └───────────────────────────┬───────────────────────────────┘  │
│                              ▼                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                       Agent 编排层                          │  │
│  │   对话循环 · 意图分流(普通对话/RAG/Tool) · 消息历史管理        │  │
│  └───┬──────────────────┬───────────────────┬─────────────────┘  │
│      ▼                  ▼                   ▼                    │
│  ┌────────┐      ┌────────────┐      ┌────────────┐             │
│  │ LLM    │      │ RAG 引擎    │      │ Tool 引擎   │             │
│  │ Client │      │ Embedding  │      │ 执行器+确认闸│             │
│  │(OpenAI)│      │ sqlite-vec │      │ bash/r/w/g  │             │
│  └────────┘      └────────────┘      └────────────┘             │
│                              ▼                                  │
│                      ┌────────────────┐                         │
│                      │  存储层         │                         │
│                      │ SQLite(配置/会话)│                         │
│                      │ sqlite-vec(向量)│                         │
│                      │ 本地文件系统     │                         │
│                      └────────────────┘                         │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 五个职责单元

| 层 | 职责 | 对外接口 |
|----|------|----------|
| **API Gateway** | HTTP 路由、请求校验、SSE 推送 | REST + SSE |
| **Agent 编排层** | 对话循环、决定调用哪个能力、维护上下文 | Go interface |
| **LLM Client** | 调 OpenAI 协议、流式解析、tool_calls 解析 | Go interface |
| **RAG 引擎** | 文档解析→切块→embedding→入库；查询时检索→拼 prompt | Go interface |
| **Tool 引擎** | 注册工具、执行命令、确认闸门、工作目录约束 | Go interface |

### 2.3 关键架构原则

1. **端口随机化**：每次启动绑定 `127.0.0.1:0`，系统分配空闲端口写入 `port.lock`，前端启动时读取。避免端口冲突，也避免被外部程序访问。
2. **SSE 用于流式**：LLM 流式输出、Tool 执行日志、确认请求都走 SSE 通道，前端订阅。
3. **核心与 UI 解耦**：Go 核心是独立 `internal/`，不依赖任何 Wails 类型。Wails 只负责把前端 WebView 嵌进来并发请求到本地 HTTP。
4. **接口驱动**：Agent 通过依赖注入持有 `LLMClient`、`RAGEngine`、`ToolEngine` 三个接口，方便单测 mock。

---

## 3. 模块与目录结构

```
agent-rust/
├── cmd/
│   ├── desktop/                     # Wails 桌面入口
│   │   └── main.go                  # 启动 HTTP Server + 嵌入 WebView
│   └── agent-cli/                   # 未来 CLI 入口（MVP 可不实现）
│       └── main.go
│
├── internal/                        # 私有实现，不对外暴露
│   ├── server/                      # API Gateway 层
│   │   ├── router.go                # chi 路由注册
│   │   ├── handler_chat.go          # POST /api/chat (SSE)
│   │   ├── handler_kb.go            # 知识库 CRUD + 上传
│   │   ├── handler_config.go        # Provider/模型配置
│   │   ├── handler_tools.go         # 工具列表 + 确认回调
│   │   └── sse.go                   # SSE 推送封装
│   │
│   ├── agent/                       # Agent 编排层
│   │   ├── agent.go                 # 对话循环主逻辑
│   │   ├── orchestrator.go          # 意图分流：普通/RAG/Tool
│   │   ├── message.go               # 消息/历史管理
│   │   └── session.go               # 会话状态
│   │
│   ├── llm/                         # LLM Client 层
│   │   ├── client.go                # Provider 接口
│   │   ├── openai.go                # OpenAI 兼容实现（流式 + tool_calls）
│   │   ├── types.go                 # Message/Tool/Chunk 类型
│   │   └── retry.go                 # 重试/超时
│   │
│   ├── rag/                         # RAG 引擎层
│   │   ├── ingest.go                # 文档解析→切块
│   │   ├── embed.go                 # 调 Embedding API
│   │   ├── store.go                 # sqlite-vec 存取
│   │   ├── retrieve.go              # 相似度检索 top-K
│   │   └── parser/                  # 文档解析器
│   │       ├── pdf.go
│   │       ├── markdown.go
│   │       └── txt.go
│   │
│   ├── tools/                       # Tool 引擎层
│   │   ├── registry.go              # 工具注册表
│   │   ├── executor.go              # 执行器（调度具体工具）
│   │   ├── gate.go                  # 确认闸门（阻塞等待 UI 回调）
│   │   └── builtin/                 # 内置工具
│   │       ├── bash.go              # 命令执行
│   │       ├── file_read.go
│   │       ├── file_write.go
│   │       ├── file_edit.go         # 精确字符串替换
│   │       └── grep.go
│   │
│   └── store/                       # 存储层
│       ├── sqlite.go                # 连接管理、迁移
│       ├── schema.sql               # 表结构
│       └── vec.go                   # sqlite-vec 扩展加载
│
├── pkg/                             # 对外可复用能力（未来 SDK）
│   └── api/                         # Go 客户端 SDK
│
├── frontend/                        # Wails 前端
│   ├── src/
│   │   ├── App.tsx
│   │   ├── components/              # 对话框/知识库/设置/确认弹窗
│   │   ├── stores/                  # zustand 状态
│   │   └── api/                     # 封装本地 HTTP + SSE
│   ├── package.json
│   └── vite.config.ts
│
├── wails.json
├── go.mod
└── README.md
```

### 3.1 关键接口定义

```go
// internal/llm/client.go
type LLMClient interface {
    ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error)
}

// internal/rag/retrieve.go
type RAGEngine interface {
    Ingest(ctx context.Context, kbID string, doc Document) error
    Retrieve(ctx context.Context, kbID, query string, k int) ([]Chunk, error)
}

// internal/tools/executor.go
type ToolEngine interface {
    Execute(ctx context.Context, call ToolCall) (Result, error)
    List() []ToolSpec
}
```

Agent 只依赖这三个接口，完全可 mock 测试。

---

## 4. 核心数据流

### 4.1 普通对话 + Tool 调用

```
前端 → POST /api/sessions/{id}/chat → Agent
  Agent → LLMClient.ChatStream(history, tools)
    流式接收 chunks：text → SSE delta；tool_call → 收集
  若有 tool_calls：
    遍历每个 tool_call → ToolEngine.Execute（内部走确认闸门）
      危险操作 → 闸门 RequestConfirm → SSE 推 confirm_req → 前端弹窗
      前端 POST /api/tools/confirm → 闸门 Resolve → 执行/拒绝
    tool_result → SSE 推送 → 回灌历史
    回到 ChatStream 让 LLM 看到 tool 结果继续生成
  无 tool_calls：循环结束，SSE 推 done
```

### 4.2 Agent 循环伪代码

```go
func (a *Agent) Run(ctx context.Context, userMsg string) {
    history.Append(userMsg)
    for iter := 0; iter < a.maxIter; iter++ {
        stream, _ := a.llm.ChatStream(ctx, history, a.tools)
        var toolCalls []ToolCall
        for chunk := range stream {
            switch {
            case chunk.IsText:  sse.Emit("delta", chunk.Text)
            case chunk.IsToolCall: toolCalls = append(toolCalls, chunk.ToolCall)
            }
        }
        if len(toolCalls) == 0 { break }

        for _, tc := range toolCalls {
            result := a.tools.Execute(ctx, tc)  // 内部走确认闸门
            sse.Emit("tool_result", result)
            history.AppendToolResult(tc, result)
        }
    }
    sse.Emit("done")
}
```

### 4.3 RAG 问答流

```
用户问 "我上传的设计文档里怎么定义权限模型？"
  → Agent 判定：会话关联了知识库且 use_rag=true
  → RAGEngine.Retrieve("权限模型定义", k=5)
      ├── embed.Embed([query]) → 云端拿 query 向量
      └── vec.Search(queryVec, topK=5, kb_id 过滤)
      └── 返回 5 个文档片段
  → 拼接 system prompt：知识库片段 + "基于此回答，无则明说"
  → LLMClient.ChatStream(拼好的 prompt) → 流式回答
  → SSE 推送（同时带引用的 chunk ID，前端展示来源）
```

**RAG 触发方式（MVP）**：不靠 LLM 判断意图，而是用户在新建会话时**显式关联知识库**。该会话所有问题（`use_rag=true`）都先走检索。二期再考虑自动意图判断。

### 4.4 文档异步入库

```
前端拖拽上传 report.pdf → POST /api/kb/{id}/documents (multipart)
  → handler 接收文件 → 立即返回 job_id → 异步入库
  → RAGEngine.Ingest(job):
      ├── parser.Parse("report.pdf") → 纯文本
      ├── chunk.Split(text, size=800, overlap=100) → []Chunk
      ├── embed.Embed(chunks) → 分批调云端（每批 ≤64）
      ├── store.Insert(kb_id, chunks, vectors, metadata)
      └── SSE 推送进度（event:ingest_progress）
```

入库走异步 + 进度推送，因为大文档 embedding 慢。

### 4.5 确认闸门（安全模型核心）

```go
// internal/tools/gate.go
type Gate struct {
    pending map[string]chan Decision  // requestID → 等待结果
    emit    func(event ConfirmEvent)  // 推给前端
}

func (g *Gate) Request(req ConfirmRequest) Decision {
    ch := make(chan Decision, 1)
    g.pending[req.ID] = ch
    g.emit(req)                    // SSE 推给前端
    defer delete(g.pending, req.ID)
    return <-ch                    // 阻塞直到前端回调
}

func (g *Gate) Resolve(id string, d Decision) {
    if ch, ok := g.pending[id]; ok { ch <- d }
}
```

**确认范围**：
- `bash`：每次确认（除非命中会话级 allowlist）
- `file_write` / `file_edit`：写入时确认（读取不确认）
- `file_read` / `grep`：不确认（只读安全）

**会话内免确认**：确认时可勾选"本会话不再询问"，记入 allowlist（按命令前缀或文件路径 glob 匹配）。

---

## 5. API 设计

### 5.1 约定

- 监听 `127.0.0.1:<随机端口>`，端口写入 `port.lock`
- 无鉴权（本机单进程）
- 时间用 RFC3339，ID 用 ULID
- 错误统一：`{"error": {"code": "...", "message": "..."}}`

### 5.2 对话 API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/sessions` | 新建会话（可关联知识库） |
| GET | `/api/sessions` | 会话列表 |
| GET | `/api/sessions/{id}` | 会话详情（含消息历史） |
| DELETE | `/api/sessions/{id}` | 删除会话 |
| POST | `/api/sessions/{id}/chat` | **发送消息（SSE 响应）** |

**`POST /api/sessions/{id}/chat` 请求**：
```json
{
  "message": "帮我看看当前目录的代码结构",
  "tools_enabled": true,
  "use_rag": true
}
```

**响应（SSE 流）**：
```
event: started      data: {"message_id": "01HX..."}
event: delta        data: {"text": "我来"}
event: delta        data: {"text": "查看一下"}
event: tool_call    data: {"tool": "bash", "input": {"command": "ls"}, "call_id": "call_1"}
event: confirm_req  data: {"request_id": "req_1", "tool": "bash", "input": {"command": "ls"}}
event: tool_result  data: {"call_id": "call_1", "output": "main.go\ngo.mod\n..."}
event: delta        data: {"text": "当前目录包含..."}
event: done         data: {"message_id": "01HX...", "usage": {...}}
```

### 5.3 知识库 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/kb` | 知识库列表 |
| POST | `/api/kb` | 新建知识库 |
| DELETE | `/api/kb/{id}` | 删除（含向量） |
| POST | `/api/kb/{id}/documents` | **上传文档（multipart，异步入库）** |
| GET | `/api/kb/{id}/documents` | 文档列表（含入库状态） |
| DELETE | `/api/kb/{id}/documents/{doc_id}` | 删除单文档 |
| GET | `/api/kb/{id}/documents/{doc_id}/status` | 入库进度 |

**上传返回**：
```json
{ "document_id": "01HX...", "job_id": "job_1", "status": "processing" }
```

### 5.4 配置 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/config/providers` | Provider 列表 |
| POST | `/api/config/providers` | 新增 Provider |
| PUT | `/api/config/providers/{id}` | 修改 |
| DELETE | `/api/config/providers/{id}` | 删除 |
| POST | `/api/config/providers/{id}/test` | 测试连通性 |
| GET/PUT | `/api/config/settings` | 全局设置 |

**Provider 结构**：
```json
{
  "id": "prov_1",
  "name": "我的智谱",
  "base_url": "https://open.bigmodel.cn/api/paas/v4",
  "api_key": "sk-***（前端掩码显示）",
  "chat_model": "glm-4-plus",
  "embed_model": "embedding-3",
  "is_default": true
}
```

### 5.5 工具确认 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/tools` | 工具清单 |
| GET | `/api/tools/allowlist` | 免确认清单 |
| POST | `/api/tools/confirm` | **用户确认回调** |
| DELETE | `/api/tools/allowlist` | 清空清单 |

**`POST /api/tools/confirm`**：
```json
{
  "request_id": "req_1",
  "decision": "allow",       // allow | deny
  "remember": "session"      // never | session | always
}
```

### 5.6 全局事件流

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/events` | 全局 SSE：入库进度、确认请求、错误 |

确认请求走全局流（确认窗是应用级浮层，非某对话内）。

### 5.7 设计取舍

- **SSE 而非 WebSocket**：单向推送够用，浏览器原生支持断线重连，curl 可调试。
- **API Key 明文存 SQLite**：本地单机，加密意义不大；前端展示掩码。OS keychain 作为未来增强。
- **`tools_enabled` / `use_rag` 在请求里**：让用户每条消息可控（"这个问题别动我文件"）。

---

## 6. 数据模型与存储

### 6.1 数据库文件布局

```
Windows: %APPDATA%\agent-rust\
macOS:   ~/Library/Application Support/agent-rust/
Linux:   ~/.config/agent-rust/
    ├── app.db              # SQLite 主库 + sqlite-vec 向量
    ├── app.db-journal / -wal
    ├── port.lock           # 当前实例 HTTP 端口
    └── uploads/            # 上传的原始文档（可选缓存）
```

主库和向量**同一个 SQLite 文件**——sqlite-vec 是扩展，加载后在同一 `.db` 建虚拟表。

### 6.2 表结构（schema.sql）

```sql
-- 1. LLM Provider 配置
CREATE TABLE providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    base_url    TEXT NOT NULL,
    api_key     TEXT NOT NULL,
    chat_model  TEXT NOT NULL,
    embed_model TEXT,
    is_default  INTEGER DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- 2. 全局设置（key-value）
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- 3. 会话
CREATE TABLE sessions (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL DEFAULT '新对话',
    provider_id   TEXT REFERENCES providers(id),
    kb_id         TEXT,
    tools_enabled INTEGER DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- 4. 消息（人/助手/工具调用结果统一存）
CREATE TABLE messages (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,          -- user | assistant | tool | system
    content      TEXT,
    tool_calls   TEXT,                   -- JSON: 助手发起的 tool 调用
    tool_call_id TEXT,                   -- 工具结果消息对应的 call_id
    citations    TEXT,                   -- JSON: RAG 引用的 chunk ID
    tokens_in    INTEGER,
    tokens_out   INTEGER,
    created_at   TEXT NOT NULL
);
CREATE INDEX idx_messages_session ON messages(session_id, created_at);

-- 5. 知识库
CREATE TABLE knowledge_bases (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    description       TEXT,
    embed_provider_id TEXT REFERENCES providers(id),
    chunk_size        INTEGER DEFAULT 800,
    chunk_overlap     INTEGER DEFAULT 100,
    doc_count         INTEGER DEFAULT 0,
    created_at        TEXT NOT NULL
);

-- 6. 文档（入库状态跟踪）
CREATE TABLE documents (
    id          TEXT PRIMARY KEY,
    kb_id       TEXT NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    file_size   INTEGER,
    mime_type   TEXT,
    status      TEXT NOT NULL,           -- processing | ready | failed
    chunk_count INTEGER DEFAULT 0,
    error       TEXT,
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_documents_kb ON documents(kb_id);

-- 7. chunk 元数据
CREATE TABLE chunks (
    id          TEXT PRIMARY KEY,
    doc_id      TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    kb_id       TEXT NOT NULL,
    ordinal     INTEGER NOT NULL,
    text        TEXT NOT NULL,
    token_count INTEGER,
    metadata    TEXT
);
CREATE INDEX idx_chunks_doc ON chunks(doc_id);
CREATE INDEX idx_chunks_kb ON chunks(kb_id);

-- 8. 工具免确认清单
CREATE TABLE tool_allowlist (
    id         TEXT PRIMARY KEY,
    scope      TEXT NOT NULL,            -- session | always
    tool       TEXT NOT NULL,
    pattern    TEXT NOT NULL,            -- 命令前缀 / 文件路径 glob
    created_at TEXT NOT NULL
);
```

### 6.3 向量表（sqlite-vec 虚拟表）

```sql
CREATE VIRTUAL TABLE vec_chunks USING vec0(
    embedding float[1024],
    chunk_id  TEXT PRIMARY KEY
);
```

**为什么分两张表**：`vec_chunks` 只存向量 + chunk_id，检索快；chunk 的文本/来源/位置存在 `chunks`，通过 chunk_id JOIN。

**维度动态**：不同 provider 维度不同（OpenAI 1536、智谱 1024）。MVP 约束：**一个知识库绑定一个 embed provider，维度固定**。

### 6.4 检索 SQL

```sql
SELECT c.id, c.text, c.doc_id, d.filename, v.distance
FROM vec_chunks v
JOIN chunks c ON c.id = v.chunk_id
JOIN documents d ON d.id = c.doc_id
WHERE v.embedding MATCH ?    -- query 向量
  AND c.kb_id = ?
ORDER BY v.distance ASC
LIMIT ?;
```

### 6.5 设计取舍

- **同一 SQLite 存向量与元数据**：事务保证文档/向量同生共死，避免双库一致性问题。
- **不用 ORM，也不用 sqlc**：手写 SQL + `database/sql` + 扫描辅助函数（`scanProvider`/`scanDocument` 等）。表不多，sqlc 的额外构建步骤收益有限。
- **迁移**：内嵌 `schema.sql`，`PRAGMA user_version` 标记版本，启动时一次性 `Exec`（MVP 单版本，多版本时再改顺序迁移）。

---

## 7. 错误处理

| 错误类型 | 处理 | 用户表现 |
|---------|------|---------|
| LLM API 错误（401/429/500） | 指数退避重试（最多 3 次，仅 429/5xx） | SSE 推 error，提示"模型暂时不可用" |
| 网络超时 | 单请求 60s，流式按 chunk 间隔 30s | 提示"连接超时，检查 baseURL" |
| Tool 执行失败（非 0 退出） | 不重试，结果原样回灌 LLM | 工具区显示错误，LLM 自主调整 |
| Tool 确认被拒/超时 | 回灌"用户拒绝了该操作" | LLM 改用别的方式 |
| Embedding 入库失败 | 单 chunk 跳过记录，整文档标 failed | 知识库页显示原因，可重试 |
| SQLite 锁/损坏 | WAL 模式 + busy_timeout | 启动检查，引导从备份恢复 |
| 配置缺失 | 启动检查，API 返回 422 | 首次使用引导配置 Provider |

**核心原则**：Tool/LLM 错误**不中断对话循环**——错误信息作为 tool result 回灌 LLM，让它自主决策。只有结构性错误（会话不存在、配置缺失）才返回 HTTP 错误。

**Agent 循环防护栏**：
- 最大迭代次数（默认 20，可配）——防 LLM 死循环调 tool
- 单会话 token 预算上限——超了自动压缩历史
- 每个工具调用有 context 超时——防单个 bash hang 住

---

## 8. 测试策略

| 层 | 方式 |
|----|------|
| `internal/llm` | `httptest` 起本地 server 模拟 OpenAI 协议（流式、tool_calls、错误码），测解析与重试 |
| `internal/rag` | 切块逻辑、检索 SQL 用内存 SQLite + 固定向量测；embedding 调用 mock |
| `internal/tools` | 每个工具单测：bash 执行/超时、file 读写、grep；闸门用假 channel 测阻塞/超时/deny |
| `internal/agent` | mock LLM/RAG/Tool 接口，测循环各分支（纯文本/单轮 tool/多轮 tool/RAG/拒绝/超迭代） |
| `internal/server` | 端到端 HTTP 测试，chi router + 内存 store，验证 SSE 事件序列 |
| `internal/store` | 临时文件 SQLite 测迁移、CRUD |

**必须有测试**：核心循环、确认闸门（最易出 bug）。**不测试**：真实 provider API、前端视觉。

---

## 9. 安全模型

虽选"人工确认"，仍守以下底线：

1. **工作目录默认约束**：文件工具默认仅限当前工作目录及其子目录；跨盘符/系统目录（`/etc`、`C:\Windows`）即使有 allowlist 也拦截（第二道防线）。
2. **命令黑名单**（即使允许也警告）：
   - `rm -rf /`、`del /s /q C:\*`、`format`、`shutdown`、磁盘格式化类
   - `curl | sh`、`wget | bash` 类远程执行
   - 确认窗标红警告，不提供"永久允许"选项。
3. **环境变量隔离**：bash 子进程启动时过滤敏感环境变量，只显式传入必要变量。
4. **API Key 不进日志**：日志和 SSE 事件对 `api_key`、`Authorization` 头脱敏。
5. **子进程资源限制**：bash 工具设 CPU/内存/时长上限（进程组 + context），防 fork bomb。

---

## 10. 发布形态

- **Wails build** 产出原生包：Windows `.exe`/`.msi`、macOS `.dmg`、Linux `.deb`/`.AppImage`
- 单二进制，前端资源嵌入（Wails 默认行为）
- SQLite + sqlite-vec 静态链接，无外部运行时
- **更新机制**：MVP 不做自动更新，手动覆盖；二期可加 GitHub Release + 自更新

---

## 11. 技术选型清单

| 类别 | 选型 | 理由 |
|------|------|------|
| 桌面框架 | **Wails v2** | Go 后端 + WebView，轻量 |
| HTTP 路由 | **chi** | 轻量、标准库兼容 |
| HTTP/SSE | 标准库 `net/http` + 自封装 | 无需框架 |
| SQLite 驱动 | **mattn/go-sqlite3**（CGO） | **加载 sqlite-vec 需 CGO 版** |
| 向量扩展 | **sqlite-vec** | 轻量，静态链接 |
| SQL 代码生成 | 手写 SQL（database/sql） | 表不多，sqlc 引入额外构建步骤收益有限，MVP 用手写 SQL + 扫描辅助函数 |
| LLM 协议 | 手写 HTTP client | 协议简单，避免 go-openai 耦合 |
| 文档解析 | MVP：txt/markdown；PDF：pdfcpu 或 unidoc | 按需扩展 |
| 前端 | **React + Vite + TypeScript** | 生态成熟 |
| 前端状态 | **zustand** | 轻量，配 SSE 简单 |
| 前端样式 | **Tailwind CSS + shadcn/ui** | 快速出美观界面 |

### 11.1 关键风险点：CGO 依赖（已验证）

**问题**：sqlite-vec 是 C 扩展，需要 CGO 编译环境。纯 Go 的 `modernc.org/sqlite` 不支持加载 C 扩展。

**决策**：采用 **mattn/go-sqlite3（CGO 版）+ sqlite-vec**。
- Wails 桌面打包本就依赖 CGO，桌面场景可接受
- 交叉编译需配置目标平台工具链（macOS/Linux 待 Plan 3 验证）
- **备选方案**：若 CGO 打包遇阻，改用 `modernc.org/sqlite` + 自实现 hnswlib 内存索引（放弃 sqlite-vec，向量不持久化或自管文件）

**已验证结果（Plan 1，2026-06-16）**：
1. ✅ **Windows + CGO**：mattn/go-sqlite3 v1.14.45 + MinGW-W64 gcc 16.1.0，编译并打包为 `agent-core.exe`，启动后 vec0 加载成功（`vec_version()` = v0.1.9）。
2. ✅ **sqlite-vec 预编译库**：Windows `vec0.dll`（v0.1.9，289KB）可用，从 GitHub release `sqlite-vec-0.1.9-loadable-windows-x86_64.tar.gz` 解压。资源实际名为 `vec0.<ext>`（非 `sqlite-vec.<ext>`）。
3. ✅ **关键约束**：mattn/go-sqlite3 必须带 build tag `sqlite_load_extension` 才能加载扩展。项目用 `Makefile` 固化此约定，所有 `go build`/`go test`/`go run` 均带 `-tags "sqlite_load_extension"`。
4. ⏳ **macOS / Linux**：待 Plan 3 验证（需对应平台的 gcc/Xcode 工具链 + `vec0.dylib`/`vec0.so`）。

**关键工程细节**：vec0 扩展文件随二进制分发，放在 `ext/<os>/vec0.<ext>`，由 `internal/store.vecExtPath()` 定位（先查工作目录向上 6 层，再查 exe 同级目录）。

---

## 12. MVP 范围（建议）

为控制首发复杂度，MVP 包含：

**必做**：
- LLM 对话（OpenAI 协议，流式，单 Provider 配置）
- Tool 执行（bash + file_read + file_write + file_edit + grep）
- 确认闸门（含会话级 allowlist）
- 单知识库 + 文档上传（txt/markdown）+ RAG 问答
- 会话/消息持久化
- Wails 桌面打包（Windows 优先）

**不做（二期）**：
- CLI / SDK 客户端
- 多协议（Claude/Gemini 原生）
- 自动 RAG 意图判断
- PDF 解析
- 自动更新
- OS keychain 存 Key

---

## 附录：未决问题（实现期决策）

1. **命令黑名单的具体规则集**：需在实现时整理完整的危险命令模式清单。
2. **token 压缩策略**：超预算时如何压缩历史（截断 vs 摘要 vs 滑动窗口），待原型验证。
3. **chunking 策略**：固定字符 vs 语义切块（按标题/段落），MVP 用固定，二期优化。
4. **多文件批量上传的并发度**：embedding 并发上限依 provider 限流而定。
