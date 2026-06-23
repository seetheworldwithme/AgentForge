# @ 文件 mention — 设计规格

- 日期：2026-06-23
- 分支：agent-go
- 状态：已与用户确认，待编写实现计划

## 1. 背景与目标

对话框已支持 `/` 触发 Skills / MCP 菜单。本规格新增 `@`：在输入框以 `@` 开头时，打开**工作目录的文件选择菜单**，让用户把选中的文件或文件夹**加入本次对话的上下文**，使大模型能看见用户指向的内容。

核心命题（用户提问，已评判）：「把选中的东西加入上下文」是否正确？
**结论：方向正确**。`@` mention 的本质就是显式指定上下文来源——不注入，模型不知道用户指什么。但「全量注入」会重蹈此前「rag 导致上下文爆炸」的覆辙，必须分层防护（文件注入内容 + 大小上限，文件夹只注入目录树）。

## 2. 决策摘要（已与用户逐项确认）

| 维度 | 选定方案 | 理由 |
|---|---|---|
| 文件夹注入策略 | **注入目录树**（所有文件相对路径），内容不注入 | 省 token、永不爆炸、保留结构感 |
| 交互范式 | **开头触发 + chip**（`@` 必须在输入框开头） | 复用现有 `/` 模式，实现简洁一致 |
| 导航深度 | **两级双列**（顶层 + 高亮文件夹的二级） | 覆盖绝大多数场景，组件复杂度可控 |
| 注入载体 | **后端读取**（前端只传相对路径数组） | Wails 前端不便读文件；后端有 WorkDir 可做越界校验；复用文件读取逻辑 |
| 注入位置 | **prepend 到本次 user message**，随消息存 DB | 跨轮持久化零额外成本，来源清晰 |
| 单文件 | 读取内容 + 大小上限（超出截断 + 标注） | 防止大文件撑爆上下文 |

## 3. 范围

### 范围内
- `@` 开头触发双列两级文件菜单（基于 WorkDir）。
- 选中文件 / 文件夹后渲染 chip，随消息发送。
- 后端按 attachments 读取并注入上下文。
- 安全校验（路径越界）、大小 / 条数截断、排除规则。

### 范围外（非目标，YAGNI）
- 任意位置 inline mention（句子中嵌入 `@token`）。
- 无限级下钻（类 Finder 列视图）。
- 附件内容的独立缓存 / 去重接口。
- symlink 跟随（基础校验即可）。
- 前端富文本 token 渲染。

## 4. 架构与数据流

```
用户输入 @
  → FileMenu 打开，GET /api/workdir/tree?path=          （列 WorkDir 顶层）
  → ↑↓ 高亮文件夹 → 右列 GET /api/workdir/tree?path=internal （列二级）
  → 回车选中 → 写入 attachments[] + 渲染 chip，清空 @ 文本（@ 仍可再次输入多选）
发送
  → streamChat(id, text, { ...opts, attachments: ["main.go","internal"] })
  → ChatHandler.Chat：遍历 attachments
       文件   → os.ReadFile（>256KB 截断 + 标注）
       文件夹 → filepath.Walk 出文件相对路径树（排除 + >2000 截断）
       拼成 <attachments> 块，prepend 到 req.Message
  → agent.Run(history, userMessage=<attachments 块> + 原文)
```

## 5. 前端改动

| 文件 | 改动 |
|---|---|
| `frontend/src/components/FileMenu.tsx`（新） | 双列两级导航，复用 `SlashMenu` 的 `forwardRef + useImperativeHandle(handleKey)` 模式 |
| `frontend/src/components/ChatInput.tsx` | `text.startsWith('@')` 触发 FileMenu；新增 `attachments: string[]` 状态 + chip 区；`send` 透传 `attachments` |
| `frontend/src/stores/sessionStore.ts` | `send` 的 opts 增加 `attachments?: string[]` |
| `frontend/src/lib/sse.ts` | `streamChat` opts 增加 `attachments?: string[]` |
| `frontend/src/lib/api.ts` | 新增 `listTree(path?: string): Promise<TreeItem[]>` |
| `frontend/src/types.ts` | 新增 `TreeItem = { name: string; is_dir: boolean; path: string }` |

### 键盘交互（两级双列）

```
输入 @
┌──────────────┬──────────────┐
│ ▸ cmd        │ ▸ core       │
│ ▶ internal ◀ │ ▸ server     │ ← 左列高亮 internal
│ ▸ frontend   │   main.go    │   右列 = internal 子项
│   main.go    │   types.go   │
└──────────────┴──────────────┘
↑↓ 移左列 │ → 进右列选子项 │ ← 退回左列 │ Enter 选当前列高亮项 │ Esc 关闭
左列 Enter(internal) → 选中整个 internal/        → chip [📁 internal ×]
右列 Enter(main.go)  → 选中 internal/server/main.go → chip [📂 main.go ×]
```

- 左列列出 WorkDir 顶层；右列实时反映左列高亮文件夹的直接子项。
- 文件夹在前、按名字排序，复用现有 `Icon`（folder / file 图标），不使用 emoji。
- `@` 之后输入的文字作为**当前列过滤词**（按 name 大小写不敏感包含匹配，无匹配时该列显示空）。
- chip 展示相对路径；文件夹 chip 用 folder 图标，文件 chip 用 file 图标，点击 × 移除。

## 6. 后端改动

### 6.1 新增 `GET /api/workdir/tree?path=<相对路径>`
位置：`internal/server/handler_workdir.go` 扩展。
- `path` 省略 / 空 = WorkDir 根。
- 返回该层子项数组：`[{name, is_dir, path}]`，文件夹在前、按名排序，`path` 为相对 WorkDir 的路径。
- 安全：`filepath.Join(workdir, path)` → `filepath.Clean` → 校验结果必须以 `workdir + 路径分隔符` 为前缀（或等于 workdir），否则返回 400 拒绝（防 `../` 越界）。

### 6.2 `chatRequest` 新增 `Attachments []string`
位置：`internal/server/handler_chat.go`。
- `Chat` 在持久化 user message 之前，遍历每个相对路径：
  - 越界校验同 6.1，越界项跳过并标注。
  - 是文件 → `os.ReadFile`，超过 `MaxFileBytes` 截断并标注原大小。
  - 是文件夹 → `filepath.Walk` 收集所有**文件**的相对路径，跳过排除目录与二进制文件，超过 `MaxTreeEntries` 截断并标注。
- 将结果拼成 `<attachments>` 块（见 §7），前置拼接（prepend）到 `req.Message`，再走原有持久化与 `agent.Run` 流程。

## 7. 注入格式

让模型清晰看到来源、边界与截断状态：

```
<attachments>
### 文件 main.go
```go
<文件内容；若超出上限：…已截断（原 1.2MB，保留前 256KB）…>
```

### 目录 internal/（共 42 个文件）
internal/server/main.go
internal/server/handler_chat.go
…
</attachments>

（以下是用户的实际问题）
<req.Message 原文>
```

- 文件用代码块包裹并标注语言（按扩展名映射，未知则不标）。
- 目录树只列**文件**相对路径（每行一条），前缀一行注明总数。
- 该块随 user message 存入 DB，下一轮 history 自动带上。

## 8. 错误处理

| 情况 | 行为 |
|---|---|
| WorkDir 为空 | 前端 `@` 菜单提示「请先选择工作目录」，不发请求 |
| 列目录：路径越界 | 后端 400 拒绝 |
| 注入：路径越界 | 跳过该项，在 `<attachments>` 标注「（已跳过：路径越界）」 |
| 文件 / 文件夹不存在或无权限 | 跳过，标注「（读取失败：原因）」，不中断发送 |
| 单文件超 `MaxFileBytes` | 截断 + 标注 |
| 目录树超 `MaxTreeEntries` | 截断 + 标注 |

## 9. 安全

- 所有相对路径在后端解析为绝对路径后，必须落在 WorkDir 之内（`filepath.Clean` + 前缀校验），杜绝 `../` 越界读取工作目录之外的文件。
- 读取走服务端，前端无任意文件读取权限。
- 不跟随 symlink（基础校验，超范围）。

## 10. 默认值（可调）

| 项 | 默认值 |
|---|---|
| `MaxFileBytes`（单文件上限） | 256 KB |
| `MaxTreeEntries`（目录树条数上限） | 2000 |
| 目录树排除目录 | `.git node_modules dist build target vendor .next .idea .vscode` |
| 二进制检测 | 按扩展名或前若干字节含 NUL 判定，跳过 |
| 列目录深度 | 两级（前端控制，接口本身不限） |

## 11. 测试

### 后端（Go 测试，`internal/server/` 与工具层）
1. 列目录：返回项的 `is_dir` 正确、文件夹在前、按名排序。
2. 列目录：`../` 等越界 path 返回 400。
3. 单文件注入：内容正确 prepend，超 `MaxFileBytes` 时截断且标注。
4. 文件夹注入：walk 结果排除规则生效、二进制被跳过、超 `MaxTreeEntries` 截断并标注。
5. 注入：越界路径被跳过且标注，正常路径拼接为 `<attachments>` 块。
6. chat 端到端：带 `attachments` 的请求，user message 持久化后含 `<attachments>` 块。

### 前端（可选，项目现有前端测试较少）
- 双列键盘导航状态机（左列 / 右列切换、Enter 选中、Esc 关闭）。
- chip 增删、`attachments` 随 send 正确透传。

## 12. 风险与权衡

- **文件夹只进目录树**：模型看到结构但看不到内容，需自行调用 file_read 工具读取。这是有意为之，换取 token 安全。用户已确认接受。
- **两级深度**：无法直接选中三级以上的深层文件。可通过先选父文件夹、再由模型读取，或后续扩展为无限级。MVP 范围内可接受。
- **`@` 开头触发**：无法在句子中间引用文件。与 `/` 一致，用户已确认。
