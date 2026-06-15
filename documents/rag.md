# AgentForge RAG 功能设计文档

> 状态：设计定稿，待评审
> 日期：2026-06-15
> 适用范围：AgentForge 项目新增 RAG（检索增强生成）能力

---

## 0. 文档目的

本文档记录 AgentForge 新增 RAG 功能的完整设计决策，涵盖数据库选型、切片策略、召回评测、整体架构与分阶段交付计划。文档作为后续实现计划（plan）的输入依据。

---

## 1. 背景与定位

### 1.1 场景

RAG 在 AgentForge 中的定位：**个人知识库问答**。

用户导入个人文档（Markdown / PDF / Office），AgentForge 基于这些资料回答问题，成为「理解用户个人资料的助手」。

由此锁定以下技术方向：
- 切片粒度面向段落 / 语义（而非代码符号）
- 召回评测重在「相关片段是否进入 Top-K」
- 数据库选型面向单机、中小数据量，不引入独立服务端

### 1.2 规模与形态

- 数据规模：小（< 50MB，个人知识库）
- 部署形态：桌面应用单文件分发（Wails 打包），零运维
- Embedding 来源：复用项目已有的 OpenAI 兼容 API 配置（base_url / api_key），不引入本地推理依赖

### 1.3 文档类型覆盖

| 类型 | 优先级 | 说明 |
|------|--------|------|
| Markdown / 纯文本（.md / .txt） | P0（demo 起步） | 结构清晰，切片最简单 |
| PDF（.pdf） | P1 | 按文本块切，多栏 / 表格 demo 阶段不处理 |
| Office（.docx / .pptx / .xlsx） | P1 | 本质是 ZIP + XML，用标准库解析 |

---

## 2. 数据库选型

### 2.1 决策

**采用 `modernc.org/sqlite` + sqlite-vec 扩展，纯 Go 无 CGO。**

### 2.2 选型对比与理由

| 候选 | 是否采用 | 理由 |
|------|---------|------|
| **`modernc.org/sqlite` + sqlite-vec** | ✅ 采用 | 纯 Go 无 CGO，跨平台交叉编译干净；sqlite-vec 原生一等支持；单文件 `.db` 分发，契合桌面应用 |
| `ncruces/go-sqlite3` + sqlite-vec (WASM) | 不采用 | 同为无 CGO 方案，但 WASM 运行时开销与内存占用高于 modernc |
| `mattn/go-sqlite3` + sqlite-vec (CGO) | 不采用 | CGO 让交叉编译复杂化，与项目「V1 避免 CGO」决策冲突；demo 阶段不值得付此工程成本 |
| PostgreSQL + pgvector | 不采用 | 要求用户安装并运行 PG 服务端，与「桌面应用单文件分发」根本冲突 |
| 独立向量库（Qdrant / Milvus / Chroma） | 不采用 | 需分发额外进程，过度设计，违背单机零运维定位 |

### 2.3 关键事实依据（2025-2026 现状）

- sqlite-vec 官方版本（asg017/sqlite-vec）处于 pre-1.0，原作者更新放缓；社区分叉 vlasky/sqlite-vec 更活跃。但通过 `modernc.org/sqlite` 调用，使用的是 modernc 内置的扩展加载，受 modernc 自身维护节奏保障。
- sqlite-vec 当前仅暴力扫描（brute-force），无 ANN 索引（HNSW 在 roadmap）。**对本项目 < 50MB 规模，暴力扫描毫秒级，足够。**
- `modernc.org/sqlite` 已原生支持 sqlite-vec 扩展（经 Gorse 项目验证），无需手动拼接 WASM binding。

### 2.4 迁移路径（将来性能不够时）

`modernc.org/sqlite` 与 `mattn/go-sqlite3` 均实现 `database/sql` 驱动接口。若未来需切换至 CGO 版以获取原生性能，或迁移至 `go-libsql`（DiskANN 索引），代码改动为机械替换驱动，不伤业务逻辑。**当前不为这个将来可能的需求提前付工程成本（YAGNI）。**

---

## 3. 切片策略（Chunking）

### 3.1 核心原则

**以语义边界为主（段落 / 标题 / 幻灯片页），字符数 / token 数仅作兜底约束。**

禁止「固定字符数硬切」——会从句子 / 段落中间切断，导致 embedding 捕捉残缺语义，召回质量塌方。

### 3.2 各格式切片规则

#### 3.2.1 Markdown / 纯文本

- 按 `#` / `##` / `###` 标题层级 + 空行分段切分
- 每个切片额外存储「祖先标题路径」（HeadingPath）作为上下文
- embedding 的是 `Content`；喂给 LLM 的是 `HeadingPath + Content`

示例：
```
文档：
## 安装
### Windows
双击 exe 安装包即可。

切片结果：
Content: "双击 exe 安装包即可。"
HeadingPath: "安装 > Windows"
```

#### 3.2.2 PDF

- 按文本块（text block）+ 视觉行间距（大间距 = 段落分隔）切分
- **demo 阶段限制（明确不处理）**：
  - 多栏排版（学术论文常见）：解析顺序会错乱，需按 X 坐标分栏重组，暂不实现
  - 表格：转为「键值对文本」
  - 图片：忽略（OCR 为后期增强）
  - 扫描件 PDF（无文本层）：不支持，返回明确错误
- 解析库：`ledongthuc/pdf`（纯 Go）或 `pdfcpu`

#### 3.2.3 Office 文档（.docx / .pptx / .xlsx）

**关键认知：Office 文档本质是 ZIP 压缩包 + 内部 XML。** 用 Go 标准库 `archive/zip` + `encoding/xml` 即可提取文本，**无需引入第三方 Office 解析库**。

| 格式 | 内部结构 | 切分边界 |
|------|---------|---------|
| .docx | `word/document.xml`，`<w:p>` 为段落 | 按段落切（同 Markdown 策略） |
| .pptx | `ppt/slides/slideN.xml`，每页一个文件 | **每页幻灯片 = 一个 chunk**（天然边界） |
| .xlsx | `xl/worksheets/sheetN.xml` | 按 sheet 或按行切，单元格转「A1=值」文本 |

### 3.3 Chunk 参数

| 参数 | 推荐值 | 说明 |
|------|--------|------|
| 目标 token 数（chunk_size） | **可配置，默认 512** | embedding 模型最佳输入区间，用户可在评测页面调参对比 |
| 重叠（overlap） | 50-100 token（默认 50） | 语义边界切分为主时可调小；纯滑动窗口兜底时保持 50-100 |
| 硬上限 | 800 token | 超过则在块内按句号 / 换行二次切，保持句子完整 |

### 3.4 切片算法流程

```
1. 按标题 / 段落 / 幻灯片页切成「语义块」
2. 语义块 ≤ 目标 token 数？→ 直接采用
3. 语义块 > 目标 token 数？→ 块内按句号 / 换行二次切，保持句子完整
4. 语义块 >> 硬上限？→ 滑动窗口强制切，带 overlap
```

### 3.5 Token 数估算

中文不能用「字符数」近似 token。demo 阶段简化处理：
- 优先使用 `tiktoken-go`（当 embedding 模型为 OpenAI 系列时精确计算）
- 降级近似：中文 `字数 × 1.5`，英文按 `单词数`，误差 10-20%，对切片粒度判断足够
- embedding 调用前 truncate 到模型上限（如 8192），估算偏差不会导致报错，仅影响切片均匀度

### 3.6 数据结构

```go
type Chunk struct {
    ID            int64
    KnowledgeBase string    // 所属知识库 ID
    DocID         string    // 源文档 ID
    Content       string    // ← 这个字段去 embedding
    HeadingPath   string    // 上下文："安装 > Windows"，召回时拼到 prompt
    Source        string    // 文件路径 / 页码，如 "manual.pdf#page=3"
    TokenCount    int
    Seq           int       // 在文档内的顺序
}
```

### 3.7 切片器代码结构

```
internal/rag/chunker/
├── chunker.go       # Chunker 接口 + 按扩展名 dispatch 的工厂函数
├── text.go          # Markdown / TXT 切片（P0）
├── pdf.go           # PDF 切片（P1）
├── office.go        # docx / pptx / xlsx 切片（P1）
└── chunker_test.go  # 固定输入 → 断言切片结果
```

接口设计（遵循 CLAUDE.md「不为只用一次的代码创建抽象」原则，仅一个接口 + 按扩展名 dispatch）：

```go
type Chunker interface {
    Chunk(doc Document) ([]Chunk, error)
}

func NewChunker(ext string) (Chunker, error) {
    switch ext {
    case ".md", ".markdown", ".txt":
        return &TextChunker{}, nil
    case ".pdf":
        return &PDFChunker{}, nil
    case ".docx", ".pptx", ".xlsx":
        return &OfficeChunker{}, nil
    default:
        return nil, fmt.Errorf("unsupported file type: %s", ext)
    }
}
```

每种格式一个独立结构体，单元测试固定输入断言输出（chunk 数量、HeadingPath 正确性、token 数在范围内）。

---

## 4. 召回评测

### 4.1 评测对象厘清

RAG 有两个易混淆的环节，本设计在同一页面中分开呈现：

| 环节 | 输入 → 输出 | 评测什么 |
|------|------------|---------|
| **召回（Retrieval）** | query → top-k chunks | 召回片段「相不相关」「该出现的有没有出现」 |
| **生成（Generation）** | query + chunks → answer | 最终回答质量 |

评测页面以召回为主，对话模式附带展示端到端效果。

### 4.2 指标分层

| 层级 | 方法 | 成本 | 准确度 | demo 是否实现 |
|------|------|------|--------|--------------|
| **L1 召回透明化** | 直接展示 top-k chunks + 相似度分数 + 来源 | 0（无需标注数据） | 人工判断 | ✅ 必做 |
| **L2 有标注的指标** | 预先标注「问题→期望命中的 chunk」，算 Recall@K / Precision@K / MRR | 需少量标注 | 量化、可对比 | ✅ 必做 |
| **L3 LLM-as-judge** | 用 LLM 给每个召回结果打相关性分 | 每次 query 多一次 LLM 调用 | 自动、可规模化 | ✅ 实现（本设计已确认） |
| **L4 端到端 RAGAS** | 评测最终答案的 faithfulness / answer relevance | 多次 LLM 调用，最贵 | 最全面 | ❌ 后期 |

**本设计实现 L1 + L2 + L3。**

### 4.3 L2 指标定义

假设测试用例：问题「AgentForge 用什么数据库？」，期望命中 `chunk_42`。系统召回 top-5，结果含 `chunk_42`：

| 指标 | 计算 | 含义 |
|------|------|------|
| **Recall@K** | 期望命中的 chunk 出现在 top-K 的比例 | 核心指标：「该找到的找到了吗」 |
| **Precision@K** | top-K 中真正相关的比例 | 「找回来的有没有噪音」 |
| **MRR** | 期望 chunk 排名倒数（第 1 名得 1，第 3 名得 1/3） | 「找得准不准」 |

多条测试用例取平均，即整个知识库的召回质量。

### 4.4 测试集来源

| 来源 | 说明 | demo 阶段 |
|------|------|-----------|
| **人工标注** | 评测页面「新建测试问题」表单：输入问题 + 从知识库现有 chunk 勾选「期望命中」 | 起步方式，标 5-10 条即可跑出第一个召回率 |
| **LLM 自动生成** | 导入文档后调 LLM「针对这段内容生成 3 个用户可能问的问题」，天然知道该命中哪段 | 增强功能，一键生成批量测试集 |

### 4.5 L3 LLM-as-judge 实现约束

为避免 L3 沦为 token 黑洞：
- judge prompt 固定模板，输入「问题 + 单个 chunk」，输出「相关 / 部分相关 / 不相关」三档
- 仅对无人工标注的测试问题启用（有标注的优先用 L2 精确指标）
- 使用低成本的 chat 模型（非 embedding 模型），单次调用 token 量可控
- 评测页面提供开关，用户可关闭 L3 节省 token

### 4.6 页面信息架构

```
┌─────────────────────────────────────────────────────────────┐
│ [知识库下拉 ▼]  top-k[5] chunk_size[512] [运行评测] [导出]    │  控制栏
├──────────────┬─────────────────────┬────────────────────────┤
│  测试问题列表  │   对话 / 单测区       │   指标面板              │
│              │                     │                        │
│ ▶ Q1: 数据库? │  [Q1 展开结果]       │  整体指标：             │
│ ▶ Q2: 安装?  │   召回 top-5:        │   Recall@5:  82%       │
│ ▶ Q3: ...   │   1. chunk_42 (0.91)│   MRR:        0.74     │
│              │   2. chunk_17 (0.85)│   Precision@5: 38%     │
│ [+ 新建问题]  │   ...               │                        │
│              │   ✓ 期望命中: c_42   │  单条指标：             │
│              │   Recall@5: 100% ✓  │   （选中 Q 时显示）      │
│              │                     │                        │
│              │  [对话模式]          │                        │
│              │   你: AgentForge...  │                        │
│              │   AI: (基于召回回答)  │                        │
│              │   [展示用到的片段]    │                        │
└──────────────┴─────────────────────┴────────────────────────┘
```

**两模式（同页切换）**：
- **单测模式**：选一个测试问题，跑召回，看 top-k 片段 + 单条指标。用于调参。
- **对话模式**：自由提问，跑完整 RAG（召回 + 生成），看最终回答 + 引用的片段。用于直观感受。

### 4.7 评测代码结构

```
internal/rag/eval/
├── metrics.go       # Recall@K / MRR / Precision@K 计算（纯函数，优先写单测）
├── judge.go         # LLM-as-judge（L3，可选开关）
├── generator.go     # LLM 自动生成测试集
└── eval_test.go
```

`metrics.go` 是纯函数，输入「召回结果 + 期望结果」，输出指标。**这是整个 RAG 最该写单元测试的地方**——指标计算逻辑错误会导致整个评测不可信。

---

## 5. 整体架构

### 5.1 模块边界

RAG 作为 `internal/` 下与 `llm/`、`conversation/` 平级的新增模块：

```
internal/
├── llm/              # 已有：LLM 客户端（RAG 复用其 embedding 调用）
├── command/          # 已有
├── storage/          # 已有：config / keyring（RAG 复用配置）
├── conversation/     # 已有：会话存储
├── rag/              # ★ 新增
│   ├── chunker/      # 切片器（按格式）
│   ├── embedder/     # embedding 调用（复用 llm/ 的 client）
│   ├── store/        # sqlite-vec 存储 + 检索
│   ├── eval/         # 评测（metrics / judge / generator）
│   ├── retrieval.go  # 检索编排：query → embed → search → top-k
│   ├── pipeline.go   # 导入编排：文档 → 切片 → embedding → 入库
│   └── types.go      # Chunk / Document / KnowledgeBase 等结构体
└── toolchain/        # 已有（V3 演进）
```

### 5.2 依赖关系（单向，避免循环）

```
rag/embedder ──依赖──> llm/            （复用 HTTP client + API 配置）
rag/store ─────依赖──> modernc.org/sqlite + sqlite-vec
rag/ ────────被依赖──> conversation/    （对话时按需注入召回上下文）
rag/eval ────依赖──> rag/store + rag/retrieval + llm/（judge 用）
```

`rag/` 不反向依赖 `conversation/`。对话模块在需要时主动调用 rag 的检索接口，保持单向依赖。

### 5.3 数据流 A：导入文档（写路径）

```
用户上传 document.pdf
    │
    ▼
pipeline.go: ImportDocument(path, kbID)
    │
    ├─► chunker.NewChunker(".pdf") → PDFChunker
    │     └─► Chunk(doc) → []Chunk（带 HeadingPath、Source、TokenCount）
    │
    ├─► embedder.Embed(contents)    ←── 复用 llm/ client，调 embedding API
    │     └─► 批量调用（OpenAI embedding 支持一次多个 input，省请求数）
    │     └─► 返回 [][]float32
    │
    └─► store.SaveChunks(chunks, vectors)
          └─► INSERT INTO chunks + vec_chunks，一个事务，失败回滚
    │
    ▼
返回 doc_id，前端显示导入成功 + chunk 数量
```

**阶段分离设计**：「解析 + 切片」与「embedding + 入库」分两阶段，中间结果可缓存。embedding API 失败（限流 / 网络）时无需重新解析文档，仅重跑 embedding + 存储。

### 5.4 数据流 B：检索 + 对话（读路径）

```
用户在对话页问："AgentForge 用什么数据库？"
    │
    ▼
retrieval.go: Retrieve(query, kbID, topK)
    │
    ├─► embedder.EmbedOne(query) → []float32（query 向量）
    │
    └─► store.Search(queryVec, kbID, topK)
          └─► SELECT ... FROM vec_chunks v
              JOIN chunks c ON c.id = v.rowid
              WHERE c.kb_id = ?              ← 按知识库过滤
              ORDER BY v.embedding <=> ?     ← sqlite-vec 余弦距离
              LIMIT ?                        ← top-k
          └─► 返回 []ScoredChunk（带相似度分数）
    │
    ▼
conversation 层拿到 top-k chunks
    │
    ├─► 组装 prompt：
    │     "以下是从知识库检索到的相关资料：\n{chunk1}\n{chunk2}...\n
    │      请基于上述资料回答问题：{query}"
    │
    └─► llm/ client.Chat(prompt, stream=true) → 流式返回
          └─► GUI: EventsEmit("chat:chunk", token)
```

**检索与对话解耦**：`retrieval.go` 只负责「query → top-k chunks」，不关心下游用途。对话页注入上下文、评测页算指标、将来 toolchain 当工具调用，都复用同一个检索接口。

---

## 6. 数据库 Schema

完整 schema（与 §3.6 Chunk 结构、§4 评测表对齐）：

```sql
-- 知识库（一个用户可有多个）
CREATE TABLE knowledge_bases (
    id TEXT PRIMARY KEY,              -- UUID
    name TEXT NOT NULL,
    embedding_model TEXT,             -- 记录用哪个 embedding 模型建的（换模型需重建索引）
    chunk_size INTEGER DEFAULT 512,
    overlap INTEGER DEFAULT 50,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 文档导入记录
CREATE TABLE documents (
    id TEXT PRIMARY KEY,
    kb_id TEXT NOT NULL REFERENCES knowledge_bases(id),
    file_path TEXT NOT NULL,
    file_type TEXT,                   -- 'markdown' / 'pdf' / 'docx' ...
    chunk_count INTEGER,
    status TEXT,                      -- 'pending' / 'indexed' / 'failed'
    error_msg TEXT,
    content_hash TEXT,                -- 防重复导入（同文件不重建索引）
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 向量虚表（sqlite-vec），只存向量本身
-- 注意：维度必须在建库时确定，取决于所选 embedding 模型。
--   text-embedding-3-small / ada-002 = 1536
--   text-embedding-3-large           = 3072
--   bge-m3                           = 1024
-- 切换 embedding 模型 → 维度变化 → 必须重建知识库（参见 knowledge_bases.embedding_model 字段）。
-- 实现时：建库时先调一次 embedding API 探测维度，再动态执行 CREATE VIRTUAL TABLE 语句，
--         不在代码里硬编码 1536。
CREATE VIRTUAL TABLE vec_chunks USING vec0(
    embedding float[1536]                     -- 示例值，实际运行时按模型动态确定
);

-- 切片元数据，与 vec_chunks 通过 rowid(id) 关联
CREATE TABLE chunks (
    id INTEGER PRIMARY KEY,          -- rowid，与 vec_chunks 对应
    doc_id TEXT NOT NULL REFERENCES documents(id),
    kb_id TEXT NOT NULL,
    content TEXT NOT NULL,           -- ← 此字段去 embedding
    heading_path TEXT,               -- 上下文："安装 > Windows"
    source TEXT,                     -- "manual.pdf#page=3"
    token_count INTEGER,
    seq INTEGER                      -- 在文档内的顺序
);

-- 评测：测试问题
CREATE TABLE eval_questions (
    id INTEGER PRIMARY KEY,
    kb_id TEXT NOT NULL,
    question TEXT NOT NULL,
    source TEXT,                     -- 'manual' 人工 / 'llm_generated' LLM 生成
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 评测：期望命中（一个问题可期望命中多个 chunk）
CREATE TABLE eval_expected (
    question_id INTEGER NOT NULL,
    chunk_id INTEGER NOT NULL,
    PRIMARY KEY (question_id, chunk_id)
);

-- 评测：历次结果（留痕，对比调参效果）
CREATE TABLE eval_runs (
    id INTEGER PRIMARY KEY,
    kb_id TEXT NOT NULL,
    params_json TEXT,                -- {"top_k":5,"chunk_size":512}
    recall_at_k REAL,
    mrr REAL,
    precision_at_k REAL,
    question_count INTEGER,
    run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**设计说明**：
- `vec_chunks`（虚表）与 `chunks`（普通表）分两张，是 sqlite-vec 官方推荐模式：虚表只存向量，元数据放普通表，用 `rowid` 关联。
- `content_hash` 防止同一文件重复导入重建索引。
- `embedding_model` 记录建库所用模型，换模型必须重建（维度 / 语义空间不一致）。
- `eval_runs` 留存历次评测结果，支撑页面绘制「参数 vs 召回率」趋势对比。

---

## 7. Wails Binding 接口

GUI 通过 Wails binding 调用 Go 方法（`cmd/gui/app.go`）：

```go
// 知识库管理
func (a *App) CreateKnowledgeBase(name string) (string, error)
func (a *App) ListKnowledgeBases() ([]KnowledgeBase, error)
func (a *App) ImportDocument(kbID, filePath string) (ImportResult, error)

// 检索
func (a *App) Retrieve(kbID, query string, topK int) ([]ScoredChunk, error)

// 对话（增强版：带 RAG）
func (a *App) ChatWithRAG(kbID, query string)  // 流式，EventsEmit 推送 token

// 评测
func (a *App) RunEvaluation(kbID string, params EvalParams) (EvalResult, error)
func (a *App) AddEvalQuestion(kbID, question string, expectedChunkIDs []int64) error
func (a *App) GenerateEvalQuestions(kbID string, count int) error  // LLM 自动生成
```

前端对应页面：
- `frontend/src/pages/KnowledgeBase.tsx` — 知识库管理 + 文档导入
- `frontend/src/pages/Evaluation.tsx` — 召回评测页面

---

## 8. 错误处理

| 场景 | 处理 |
|------|------|
| embedding API 失败（限流 / 网络） | 「解析 + 切片」与「embedding + 入库」分阶段，重试仅需重跑后者，不重新解析 |
| PDF 解析失败（损坏 / 加密） | 返回明确错误「PDF 无法解析：加密的 PDF 暂不支持」，不静默吞掉 |
| 扫描件 PDF（无文本层） | 返回明确错误「扫描件 PDF 暂不支持，需 OCR」 |
| 批量导入中单个文件失败 | 不阻塞其他文件，收集错误列表返回前端 |
| 不支持的文件类型 | `NewChunker` 返回明确错误，前端提示 |
| embedding 维度与库不一致 | 检测 `knowledge_bases.embedding_model` 字段，与当前配置模型比对，拒绝跨模型检索，提示「embedding 模型不匹配，需重建知识库索引」 |
| 建库时维度探测 | 创建知识库时先调一次 embedding API 探测返回维度，据此动态 `CREATE VIRTUAL TABLE vec_chunks USING vec0(embedding float[N])`，代码不硬编码维度 |
| 重复导入同一文件 | 通过 `content_hash` 检测，跳过或提示用户 |

---

## 9. 分阶段交付计划

每个里程碑都是可独立运行的完整状态，非半成品。**M1-M5 为 demo 跑通最小集（仅 Markdown），M6-M7 为完善。**

| 里程碑 | 内容 | 验证标准 |
|--------|------|---------|
| **M1：存储层** | `rag/store/` + SQLite schema + sqlite-vec 跑通 | 插入假向量，按余弦检索出正确顺序 |
| **M2：切片器（Markdown）** | `rag/chunker/` TextChunker（Markdown + TXT） | 单测：固定 .md 切出预期 chunk，HeadingPath 正确 |
| **M3：导入流水线** | `pipeline.go`：文档 → 切片 → embedding → 入库 | 导入一个 .md 文件，数据库有 chunk 和向量 |
| **M4：检索 + 对话** | `retrieval.go` + 接入 conversation | 问问题，基于召回 chunks 回答 |
| **M5：评测 L1 + L2** | 评测页面骨架 + metrics.go + 人工标注 | 标 5 条问题，跑出 Recall@5 数字 |
| **M6：补全格式** | PDF + Office chunker | 导入 PDF / docx 能正常切片入库 |
| **M7：评测 L3 + 自动测试集** | LLM-as-judge + LLM 生成测试集 | 一键评测无标注问题 |

**策略说明**：先用 Markdown 跑通全链路（M1-M5），再补 PDF / Office（M6）。牺牲初期格式覆盖换取全链路早跑通，避免卡在某个格式解析上动弹不得。

---

## 10. 决策汇总

| # | 决策项 | 结论 |
|---|--------|------|
| 1 | 场景 | 个人知识库问答 |
| 2 | 数据规模 | 小（< 50MB），单机 |
| 3 | 数据库 | `modernc.org/sqlite` + sqlite-vec（纯 Go 无 CGO） |
| 4 | Embedding 来源 | 调 OpenAI 兼容 API（复用已有配置） |
| 5 | 文档格式 | Markdown/TXT（P0）+ PDF（P1）+ Office（P1） |
| 6 | 切片原则 | 语义边界优先（标题/段落/页），token 数兜底 |
| 7 | chunk_size | 可配置，默认 512 token |
| 8 | overlap | 默认 50 token |
| 9 | Office 解析 | 标准库 `archive/zip` + `encoding/xml`，不引入第三方库 |
| 10 | 评测深度 | L1（透明化）+ L2（Recall@K/MRR/Precision@K）+ L3（LLM-as-judge） |
| 11 | 测试集来源 | 人工标注（起步）+ LLM 自动生成（增强） |
| 12 | 评测形态 | GUI 专门页面，对话式测试 + 指标面板 |
| 13 | 模块位置 | `internal/rag/`，与 `llm/`、`conversation/` 平级 |
| 14 | 依赖方向 | rag/ 单向被 conversation/ 依赖，不反向 |
| 15 | 交付顺序 | M1-M5（Markdown 全链路）→ M6（PDF/Office）→ M7（L3 + 自动测试集） |

---

## 11. 未覆盖（明确不在本设计范围）

以下项本设计明确不涉及，留待后续演进，避免范围蔓延：

- **OCR**：扫描件 PDF / 图片的文字识别。当前 PDF 仅处理有文本层的。
- **多模态 embedding**：图片 / 表格的向量化。
- **重排（reranker）**：召回后的二次精排模型。当前仅靠向量相似度。
- **混合检索（hybrid search）**：向量检索 + BM25 关键词检索融合。当前仅向量检索。
- **多用户 / 多机同步**：当前为单机个人使用，无并发与同步需求。
- **RAGAS 端到端评测（L4）**：最终答案的 faithfulness / answer relevance 评测。
- **ANN 索引（HNSW）**：sqlite-vec 暴力扫描在当前规模下足够。

---

## 参考资料

- [sqlite-vec 官方 Go 指南](https://alexgarcia.xyz/sqlite-vec/go.html)
- [modernc.org/sqlite 原生支持 sqlite-vec（Gorse 博文）](https://gorse.io/zh/posts/sqlite-vec)
- [sqlite-vec 生产就绪状态讨论 Issue #221](https://github.com/asg017/sqlite-vec/issues/221)
- [Choosing an Embeddable Vector Database for a Go Application](https://shaharia.com/blog/choosing-embeddable-vector-database-go-application/)
- [Implementing a RAG System: SQLite and Postgres (sqlite-vec & pgvector)](https://dev.to/jonbiz/implementing-a-rag-system-inside-an-rdbms-sqlite-and-postgres-with-sqlite-vec-pgvector-4d5h)
