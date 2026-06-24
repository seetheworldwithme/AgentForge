# 跨会话 Memory（记忆）— 设计规格

- 日期：2026-06-24
- 分支：agent-go
- 状态：已与用户确认，待编写实现计划

## 1. 背景与目标

当前 AgentForge 的 agent 每次会话都是「无状态」的：上一轮确认的偏好、踩过的环境坑、项目约定，下一轮一概不知，只能靠用户反复说明或靠 @ 文件 mention 临时注入。

本规格引入**跨会话 Memory**：让 agent 像 Claude Code 那样维护一个**事实笔记本**——在对话中自主判断哪些事实值得长期记住并写入，下次会话开头再把记忆索引导入上下文，按需读取细节。用户也可在 UI 中查看/编辑/删除记忆条目。

核心命题（用户提问，已评判）：「Agent 自动写 + 人工可管理」是否正确？
**结论：方向正确**。记忆的价值在于 agent 自主积累（用户不必手动维护），但必须允许人工纠正（agent 可能记错或记入噪声）。两者结合：agent 用专用工具写、用户用 UI 管理，产物都是同一批 markdown 文件。

## 2. 决策摘要（已与用户逐项确认）

| 维度 | 选定方案 | 理由 |
|---|---|---|
| 产生方式 | **Agent 自动写 + 人工可管理** | 自主积累 + 可纠正，最贴近 Claude Code 体验 |
| 存储形式 | **Markdown 文件**（frontmatter + 正文） | 可 git、可手编、透明、忠实复刻 |
| 存储位置 | **`<workdir>/.agentforge/memory/`**，无 workdir 时全局 fallback | 跟项目走、可进 git、团队可共享 |
| 管理 UI | **SettingsModal 加 Memory tab** | 与现有 Skills/MCP 设置一致，改动小、入口统一 |
| 写入机制 | **专用工具** memory_save / memory_read / memory_delete | 后端保证 frontmatter + 索引一致性，防写坏 |

### 2.1 采纳的默认值（如无异议即生效）

| 项 | 默认 |
|---|---|
| 分类（type） | 照搬 `user` / `feedback` / `project` / `reference` 四类 |
| 注入优先级 | Skills 之后、baseSystemPrompt 之前（中低优先级背景） |
| 时间戳 | 用文件 mtime，不入 frontmatter（保持 frontmatter 精简） |
| frontmatter 解析 | 手写极简解析器（仅 `key: value`），**零新依赖** |

## 3. 范围

### 范围内
- `<workdir>/.agentforge/memory/` 下的 markdown 记忆库（frontmatter + 正文 + MEMORY.md 索引）。
- 后端 `internal/memory` 包：CRUD、frontmatter 序列化/解析、索引自动同步、workdir/fallback 路径解析。
- agent 读取：每次 Run 注入记忆索引；`memory_read` 工具读单条。
- agent 写入：`memory_save` / `memory_delete` 工具，后端维护一致性。
- 前端：memoryStore + api + SettingsModal Memory tab（查看/编辑/删除/新建）。
- 切换 workdir 时记忆目录随之切换（按项目隔离）。

### 范围外（非目标，YAGNI）
- 向量/语义召回（用「索引全量注入 + 按需读」，不接 RAG）。
- 跨记忆语义去重（仅按 `name` 去重：同名覆盖）。
- 记忆的版本历史 / 时间线。
- 记忆条目之间的 `[[link]]` 解析渲染（正文里可写，但不做跳转）。
- 多用户 / 权限隔离（单机桌面应用）。

## 4. 文件布局与数据模型

### 4.1 目录结构
```
<workdir>/.agentforge/memory/
├── MEMORY.md              # 索引：后端从扫描结果自动生成（勿手编）
├── frontend-design.md     # 单条记忆
└── go-env.md
```
- 无 workdir 时 fallback：`<appdata>/memory/`（macOS `~/Library/Application Support/agent-rust/memory/`）。
- MEMORY.md 是**后端扫描所有 `*.md` 后生成的投影**，Save/Delete/列表加载时刷新；手编会被下次刷新覆盖，故用户应直接编辑条目 `.md`，不要手编 MEMORY.md。

### 4.2 单条记忆格式
```markdown
---
name: frontend-design
description: AgentForge 前端设计方向（靛蓝主题、语义令牌、禁 emoji 图标）
type: feedback
---
（正文：事实本身）

**Why:** 徐先生要求「别一眼看上去就是 AI 写的」。
**How to apply:** 后续前端改动复用语义令牌类名，不写裸 bg-gray-*。
```
- frontmatter 字段固定三列：`name` / `description` / `type`。
- `type` 为 `feedback` / `project` 时，正文末尾应有 `**Why:**` / `**How to apply:**`（提示词约束，非强制校验）。
- frontmatter 之后第一个空行分隔正文；正文为 markdown。

### 4.3 MEMORY.md 格式（后端生成）
```markdown
# Memory Index

- [AgentForge 前端设计方向：靛蓝/语义令牌/禁 emoji](frontend-design.md) · feedback
- [.g 与 toolchain 版本冲突的解法](go-env.md) · project
```
- 每行 = `- [{description}]({name}.md) · {type}`；显示文本统一取 `description`（frontmatter 已含，**不从正文推导标题**，避免 body 无 `#` 时无标题可用）。
- 按文件 mtime 倒序（最近更新在前）。
- `IndexContext()`（注入 agent 用）= 引导提示 + 与上文同源的索引行（纯文本版，去掉 markdown 链接括号），运行时生成、不落盘。

## 5. 架构与数据流

三条数据流，共用同一批 `.md` 文件：

```
A. 读取/注入（每轮对话自动）
  ChatHandler.Chat → agent.Run
    → deps.Memory.IndexContext()（运行时从 List() 生成索引文本）
    → prependSystemContext(history, 索引)  # Skills 后、base 前
    → 模型看到索引；按需调 memory_read(name) 取细节

B. 写入（agent 自主）
  模型决定记忆 → 调 memory_save(name, description, type, body)
    → MemoryStore.Save：校验 name → 序列化 frontmatter → 写 <name>.md → Reindex 刷新 MEMORY.md
    → 返回成功（工具结果回灌模型）

C. 人工管理（UI）
  SettingsModal Memory tab
    → GET /api/memory → 列表（含 body）
    → 编辑 → PUT /api/memory/{name}（走同一个 Save）
    → 删除 → DELETE /api/memory/{name}（走同一个 Delete）
```

包依赖（新增 `internal/memory`，仿 skills/rag 的 Provider 注入模式）：
```
server ─→ memory ──（读文件）
  │       ↑
  ├──→ agent ──(MemoryProvider 接口)──→ memory
  └──→ tools ──(memory 工具持有 *MemoryStore)──→ memory
```

## 6. 后端改动

### 6.1 新增 `internal/memory` 包
核心类型与接口：
```go
package memory

type Type string
const (
    TypeUser      Type = "user"
    TypeFeedback  Type = "feedback"
    TypeProject   Type = "project"
    TypeReference Type = "reference"
)

// Entry 一条记忆：frontmatter + 正文。
type Entry struct {
    Name        string    // kebab-case，同时是文件名（不含 .md）
    Description string    // 一行摘要，召回相关性判断依据
    Type        Type
    Body        string    // frontmatter 之后的 markdown 正文
    UpdatedAt   time.Time // 来自文件 mtime，不序列化
}

// Provider 给 agent 的最小接口（DI，仿 SkillProvider）。
type Provider interface {
    IndexContext() string // 注入用索引文本；无记忆返回空串
}

// Store 完整读写能力（server / 工具 / API 用）。
type Store interface {
    Provider
    List() ([]Entry, error)
    Get(name string) (Entry, error)
    Save(e Entry) error       // upsert：写 .md + Reindex
    Delete(name string) error // 删 .md + Reindex
}

type MemoryStore struct {
    workdir func() string // 返回当前 workdir；空串触发 fallback
    appdata string        // fallback 根目录
}
func New(workdir func() string, appdata string) *MemoryStore
```
要点：
- `ResolveDir()`：`workdir()` 非空 → `<workdir>/.agentforge/memory/`（目录不存在则惰性 `MkdirAll`）；否则 `<appdata>/memory/`。
- frontmatter **手写序列化/解析**（仅支持 `key: value` 行，`---` 包裹），零新依赖；解析失败的条目跳过并 `log.Printf`，不中断 List。
- `name` 校验：正则 `^[a-z0-9]+(-[a-z0-9]+)*$`，长度 1–64；含路径分隔符/`..`一律拒绝（防穿越）。
- `Save` 先写临时文件再 `os.Rename`（原子写），成功后 `Reindex` 重写 MEMORY.md。
- `Reindex`：扫 `*.md`（排除 `MEMORY.md`）→ 解析 → 按 mtime 倒序生成 MEMORY.md。
- `IndexContext()`：复用 `Reindex` 的扫描结果，返回 §8.1 的注入文本（不落盘，运行时生成）。
- 体积上限：`body` ≤ `MaxBodyBytes`（8 KB），`description` ≤ `MaxDescRunes`（200）；超出在 Save 时截断或拒绝（见 §11）。

### 6.2 agent 集成
- `internal/agent/types.go`：`Deps` 新增 `Memory memory.Provider`（may be nil）。
- `internal/agent/agent.go` `Run()`：在 Skills 注入块**之后**、`baseSystemPrompt` **之前**插入：
  ```go
  if a.deps.Memory != nil {
      if idx := a.deps.Memory.IndexContext(); strings.TrimSpace(idx) != "" {
          history = prependSystemContext(history, idx)
      }
  }
  ```
- 最终 system 内容顺序（前→后）：`…, RAG, plan, base, memory-索引, skills`。记忆作为背景事实，优先级低于工具路由策略（base），高于技能指令（skills）。

### 6.3 memory 工具（注册到 `tools.Engine`）
三个工具，**持有 `*memory.MemoryStore` 引用**（带状态，故在 main.go 装配时注入，不进 builtin 包级注册）：

| 工具 | 参数 | 行为 | 返回 |
|---|---|---|---|
| `memory_save` | `name`, `description`, `type`, `body` | `Store.Save`（upsert + reindex） | 成功/失败文本 |
| `memory_read` | `name` | `Store.Get` | frontmatter + 正文 |
| `memory_delete` | `name` | `Store.Delete` | 成功/失败文本 |

- `memory_save` 的 `description` 写进工具说明：仅记「跨会话有用、代码/git 查不到」的事实；重复事实用同名更新而非新建。
- 仿 `internal/tools/builtin/*.go` 的 `Tool` 接口实现（`Spec()` + `Execute(ctx, args)`），参数用现有 args 解析模式。
- 计划模式（PlanMode）**不禁用** memory 工具（记忆读写无破坏性，且计划模式也需要记忆）。

### 6.4 HTTP API（仅前端 UI 用）
新增 `internal/server/handler_memory.go`，`MemoryHandler{Store: *memory.MemoryStore}`：
| 方法 路径 | 行为 |
|---|---|
| `GET /api/memory` | `{entries: [{name, description, type, body, updated_at}]}`（含 body，供编辑） |
| `GET /api/memory/{name}` | 单条 full |
| `PUT /api/memory/{name}` | upsert（body: `{description, type, body}`） |
| `DELETE /api/memory/{name}` | 删除 |

- `name` 路径参数同样走 §6.1 的校验；非法返回 400。
- 在 `router.go` 的 `/api` route 组注册 `(&MemoryHandler{Store: d.Memory}).Routes(r)`。

### 6.5 装配（main.go / router）
- `main.go`：构造 `memStore := memory.New(func(){ return workdir.Get() }, appDataDir)`。
- 注入三处：`agent.Deps{..., Memory: memStore}`、memory 工具注册进 `tools.Engine`、`server.Deps{..., Memory: memStore}`。

## 7. 前端改动

| 文件 | 改动 |
|---|---|
| `frontend/src/types.ts` | 新增 `MemoryType = 'user'\|'feedback'\|'project'\|'reference'`、`MemoryEntry = {name, description, type, body, updated_at}` |
| `frontend/src/lib/api.ts` | 新增 `listMemory / getMemory / saveMemory / deleteMemory` |
| `frontend/src/stores/memoryStore.ts`（新） | zustand：`entries / loaded / load / save(name,entry) / remove(name)` |
| `frontend/src/components/MemoryPanel.tsx`（新） | 左：按 type 分组的条目列表（显示 description）；右：编辑区（type 下拉 / name / description / body textarea）+ 保存/删除；顶部新建按钮 |
| `frontend/src/components/SettingsModal.tsx` | 子菜单加「Memory」项，内容区渲染 `<MemoryPanel/>`（打开时 `memoryStore.load()`，触发后端 reindex） |

- 复用 `.field` / `.card` / `.btn-*` / `.status-pill` 与 `Icon.tsx`（新增 `brain` 或 `bookmark` 图标，禁 emoji）。
- 按 type 分组用语义色：user/feedback/project/reference 各一色（复用现有令牌，不引入新色）。
- body 编辑用 `<textarea class="field">`；保存走 `saveMemory`，成功后刷新列表。

## 8. 注入格式

### 8.1 索引注入（agent.Run，来自 IndexContext）
```
你拥有一份跨会话的记忆库。以下是当前所有记忆的索引（按最近更新排序）：

- AgentForge 前端设计方向：靛蓝/语义令牌/禁 emoji · feedback（frontend-design.md）
- .g 与 toolchain 版本冲突的解法 · project（go-env.md）
…

若某条与当前任务相关，调用 memory_read(name) 读取完整内容后再使用。
当用户给出值得长期记住的事实（偏好/约定/环境坑/外部资源），调用 memory_save 记录；重复事实用同名更新。
```

### 8.2 memory_read 返回
原样返回 frontmatter + 正文（即文件内容），便于模型看到 Why / How to apply。

## 9. 错误处理

| 情况 | 行为 |
|---|---|
| 无 workdir 且 appdata 不可写 | `IndexContext()` 返回空串，agent 不注入；API 返回 500 并提示 |
| `name` 非法（含 `/`、`..`、大写、空） | Save/API 拒绝，返回错误 |
| frontmatter 解析失败 | List 跳过该条 + `log.Printf`，其余正常 |
| 单条 body 超 `MaxBodyBytes` | Save 拒绝并提示（UI/工具结果回显原因） |
| 磁盘读写错误 | 返回错误；工具结果 `is_error=true`；UI toast 提示 |
| 记忆目录不存在 | 首次 Save 时 `MkdirAll` 惰性创建 |

## 10. 安全

- `name` 严格 kebab-case 校验，杜绝 `../` 穿越（记忆目录之外读写）。
- `ResolveDir()` 解析后校验结果必须落在 `<workdir>/.agentforge/memory/` 或 `<appdata>/memory/` 之内。
- 记忆内容来自 agent/用户输入，写入前不做 HTML 转义（markdown 文件，前端渲染走现有 markdown-body 管道）。
- 不执行记忆正文中的任何指令（纯文本注入到 system context）。

## 11. 默认值（可调）

| 项 | 默认值 |
|---|---|
| 记忆目录名 | `.agentforge/memory/` |
| 索引文件名 | `MEMORY.md` |
| type 枚举 | `user` / `feedback` / `project` / `reference` |
| `MaxBodyBytes`（单条正文上限） | 8 KB |
| `MaxDescRunes`（description 上限） | 200 |
| `name` 规则 | `^[a-z0-9]+(-[a-z0-9]+)*$`，长度 1–64 |
| 注入位置 | Skills 后、base 前 |
| MEMORY.md 排序 | 文件 mtime 倒序 |

## 12. 测试

### 后端（Go 测试，`internal/memory/` + 集成）
1. frontmatter 序列化 → 解析往返一致（含中文、特殊字符、多行正文）。
2. `Save` 写 `.md` 且 MEMORY.md 索引含该条；`Delete` 后索引移除。
3. `List` 扫描 + 解析正确，跳过解析失败条目，排除 MEMORY.md。
4. `Reindex` / `IndexContext` 文本格式正确，按 mtime 倒序。
5. `ResolveDir`：workdir 非空 → workdir 目录；空 → appdata 目录；目录不存在惰性创建。
6. `name` 校验：合法通过，`../`、`/`、大写、空拒绝。
7. body/description 超限：拒绝并返回明确错误。
8. `memory_save/read/delete` 工具：正确落盘/读取/删除 + reindex。
9. agent 注入：`deps.Memory` 非空时索引进入 history（位于 skills 与 base 之间）；为空时不注入。

### 前端（若沿用现有测试设施）
10. memoryStore CRUD 调用正确 API。
11. MemoryPanel：列表分组、编辑保存、删除、新建。

### 端到端
12. `make dev`：UI 新建一条记忆 → 新对话提问相关内容 → 验证索引被注入、agent 能 `memory_read` → 让 agent `memory_save` 一条 → UI 刷新可见。

## 13. 风险与权衡

- **agent 可能记入噪声/重复**：初版仅按 `name` 去重（同名覆盖），不做语义去重；靠 `memory_save` 工具说明约束「只记跨会话有用、git 查不到的事实」。可接受，后续可加 description 近似查重。
- **索引随条目增多变长**：暂不截断（个人记忆条目通常 < 几十条）；若未来膨胀，再加相关性过滤或分页。
- **工具写 vs 手编两路径**：都落同一批 `.md`；agent 用工具写会即时 reindex；外部手编/新增 `.md` 需等下次 UI 加载或 agent 注入时（IndexContext 运行时扫描）才进索引——这是预期，因为 MEMORY.md 本就是扫描投影。
- **workdir 切换**：切换工作目录后记忆目录随之改变，不同项目记忆隔离——这是设计意图（按项目隔离），非 bug。
- **手写 frontmatter 解析的局限**：仅支持扁平 `key: value`，不支持嵌套/多行值；当前三字段足够，换取零依赖。若未来需复杂结构再引 yaml.v3。
