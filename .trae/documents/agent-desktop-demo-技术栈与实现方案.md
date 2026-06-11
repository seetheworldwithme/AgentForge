## Summary

项目名称：**AgentForge**
定位：一个跨平台的智能 Agent 工具，支持 CLI 和桌面 GUI 双模式使用。

核心能力：
1) 接入 OpenAI 兼容的大模型 API 做对话；
2) 在本机执行命令：Windows 用 PowerShell、macOS/Linux 用 bash，并且默认采用"白名单命令"策略。

双模式分发：
- **CLI 模式**：通过 `npm install -g agentforge` 安装，终端命令行使用（类似 Claude Code）
- **GUI 模式**：打包为桌面客户端安装包，图形化界面使用

技术栈（面向长期演进，支持实时流式处理与复杂工具调用链）：
- 桌面框架：Wails v2（Go 后端 + 系统 WebView）
- 前端 UI：React + TypeScript + Vite
- 后端语言：Go（所有本地能力层均由 Go 实现）
- CLI 分发：npm wrapper + Go 预编译二进制
- 模型接入：OpenAI 兼容 API（可配置 base_url / api_key / model）
- 命令执行：Go os/exec（Shell 由平台决定）+ 白名单校验

成功标准：
- CLI 模式：能通过 `agentforge chat "你好"` 发起对话，`agentforge run <command>` 执行白名单命令
- GUI 模式：能通过桌面窗口完成上述所有操作，带流式输出展示
- 默认安全：不允许任意命令；不记录/不输出 api_key

---

## Current State Analysis

- 目录 f:/code/Go/myself/agent 当前为空（未检测到现有工程与依赖）。
- 因此以"新建工程"为起点，方案会明确脚手架、目录结构、模块边界与后续演进路径。

---

## Proposed Changes

### 1) 工程与语言/框架选型（定稿）

**Wails v2 + Go + React + TS + Vite**
- Why：Go 天然擅长并发、流式处理与系统级调用；Wails 使用系统原生 WebView 不打包 Chromium，安装包体积小（~10-20MB vs Electron ~80-150MB）；前后端通过 Go binding 直接通信，无 IPC 序列化开销。
- Tradeoff：Wails 社区比 Electron 小；Windows 上依赖 WebView2 Runtime（Win10/11 已预装，Win7/8 需额外安装）。

**运行时/版本建议**
- Go：1.22+（或最新稳定版）
- Wails：v2（wails CLI 脚手架）
- Node.js：20 LTS（仅前端构建用）
- 包管理：Go Modules + npm/pnpm（前端依赖）

### 2) 双模式架构：CLI + GUI 共享核心引擎

核心理念：**Go 业务逻辑只写一份，CLI 和 GUI 只是两个不同的"壳"。**

```
                    ┌─────────────────────────┐
                    │     internal/ (Go)       │  ← 共享业务逻辑
                    │     ├── llm/             │
                    │     ├── command/         │
                    │     ├── storage/         │
                    │     ├── conversation/    │
                    │     └── toolchain/       │
                    └───────────┬─────────────┘
                                │
                    ┌───────────┴───────────┐
                    │                       │
              ┌─────▼──────┐         ┌──────▼──────┐
              │  CLI 入口    │         │  Wails GUI  │
              │  cmd/cli/   │         │  cmd/gui/   │
              │  终端交互    │         │  桌面窗口    │
              └──────┬──────┘         └─────────────┘
                     │
              ┌──────▼──────┐
              │  npm wrapper │
              │  npm install │
              │  -g agentforge│
              └─────────────┘
```

#### CLI 模式

**npm 安装原理**：npm 包只是一个薄 wrapper，postinstall 时根据平台下载对应的 Go 预编译二进制。

```
npm install -g agentforge
       │
       ▼
postinstall 脚本检测平台
       │
       ▼
从 GitHub Releases / CDN 下载对应的 Go 预编译二进制
  - Windows → agentforge-windows-amd64.exe
  - macOS   → agentforge-darwin-amd64
  - macOS   → agentforge-darwin-arm64
  - Linux   → agentforge-linux-amd64
       │
       ▼
npm 包里的 JS 入口脚本调用这个二进制
```

**npm wrapper 示意**（packages/agentforge/bin/agentforge.js）：
```javascript
#!/usr/bin/env node
const { execFileSync } = require('child_process');
const path = require('path');

const binaryName = process.platform === 'win32' ? 'agentforge.exe' : 'agentforge';
const binaryPath = path.join(__dirname, '..', 'bin', binaryName);

execFileSync(binaryPath, process.argv.slice(2), { stdio: 'inherit' });
```

**CLI 使用示例**：
```bash
# 对话
agentforge chat "帮我分析一下当前系统的内存使用情况"
agentforge chat --stream "解释一下这段代码"

# 命令执行
agentforge run sysinfo
agentforge run diskusage

# 配置管理
agentforge config set base_url https://api.openai.com/v1
agentforge config set api_key sk-xxx
agentforge config set model gpt-4o-mini
agentforge config list
```

#### GUI 模式

Wails 打包为桌面客户端，通过图形界面完成所有操作，体验更友好。

#### 两种模式的功能对比

| 功能 | CLI 模式 | GUI 模式 |
|------|---------|---------|
| 对话（LLM） | ✅ 终端流式输出 | ✅ 窗口流式渲染 |
| 命令执行 | ✅ 直接在终端看输出 | ✅ 窗口内嵌终端区 |
| Settings 配置 | ⚠️ 命令行参数 / 配置文件 | ✅ 图形化设置页面 |
| 会话历史 | ⚠️ 本地文件存储 | ✅ 本地存储 + 可视化浏览 |
| 工具调用链 | ✅ 终端展示过程 | ✅ 可视化展示 DAG |
| 安全存储 | ⚠️ 文件加密 | ✅ 系统 Keychain |

### 3) 应用模块划分

**Frontend（前端 UI — React + TS + Vite，GUI 模式专用）**
- Chat 页面：消息列表 + 输入框 + 发送按钮 + 流式输出展示（SSE 逐 token 渲染）
- Settings 页面：base_url / api_key / model / 允许的命令集开关（或命令白名单编辑）
- Command 页面（或在 Chat 内嵌工具区）：选择白名单命令并执行，展示实时输出

**Backend（Go 后端 — 共享核心引擎）**
- LLMClient：负责请求 OpenAI 兼容 API（优先流式 SSE，支持非流式降级）
- CommandRunner：命令白名单校验 + 组装平台命令 + os/exec 执行 + 输出流实时转发
- SecureStorage：保存 api_key（优先系统 Keychain；退化到本地加密文件）
- ConversationStore：保存会话历史（SQLite 或 JSON 文件）
- ToolChain（后续演进）：多步骤工具编排、并行执行、结果聚合

**CLI 层（Go — cmd/cli/）**
- 命令行参数解析（标准库 flag 或 cobra）
- 终端输出格式化（带颜色、进度条）
- 交互模式（readline 类似体验）

**Binding 层（Wails 自动生成，GUI 模式专用）**
- Wails 通过 Go struct tag（`wails:generate`）自动将 Go 方法暴露给前端
- 前端通过 `window.go.main.App.MethodName()` 直接调用 Go 函数
- 支持返回值、错误、以及 Events 机制（Go 端 EventsEmit → 前端监听）

### 4) 大模型接入（OpenAI 兼容 API）

**配置项**
- base_url：例如 https://api.openai.com/v1 或任意兼容网关
- api_key：仅存储在系统安全存储中；不写日志；不通过 query string
- model：如 gpt-4o-mini / deepseek-chat 等（由用户输入）

**接口形态**
- 默认使用 Chat Completions API（流式 SSE）
- Go 端使用 net/http + bufio.Scanner 逐行解析 SSE 事件
- GUI 模式：通过 Wails EventsEmit 将每个 token/chunk 实时推送到前端
- CLI 模式：直接将 chunk 写入 stdout

**Go 端流式处理示意**
```
HTTP Request → Go goroutine 持续读取 SSE
  → GUI: EventsEmit("chat:chunk", data) → 前端监听并追加渲染
  → CLI: os.Stdout.Write(chunk) → 终端直接显示
```
- Go 的 goroutine 天然适合这种长连接流式读取场景，不会阻塞 UI

**建议的错误处理**
- 401：提示 api_key 无效
- 429：提示限流并建议重试
- 网络错误：提示检查 base_url/代理

### 5) 命令执行（白名单策略）

**白名单的最小形态（建议）**
- 只允许预设命令 ID（而不是用户任意输入整段命令）
- 每个命令 ID 映射到：
  - title：展示名
  - platform：windows / unix
  - shell：powershell / bash
  - argsTemplate：固定参数模板 + 可选的少量参数位（需要校验）

**平台命令落地规则**
- Windows：
  - 可执行：powershell.exe
  - 参数建议：-NoProfile -ExecutionPolicy Bypass -Command <script>
- macOS/Linux：
  - 可执行：bash
  - 参数建议：-lc <script>

**Go 端命令执行示意**
```
exec.Command(shell, args...) → stdout/stderr Pipe → goroutine 逐行读取
  → GUI: EventsEmit("command:output", line)
  → CLI: fmt.Println(line)
```

**输出回传**
- stdout/stderr 逐行实时输出（GUI 通过 Wails Events，CLI 直接写 stdout）
- 每次执行生成 run_id，支持并发执行与取消（context.Context）

**安全基线（默认）**
- 不允许白名单外命令
- 不允许带重定向/管道等高风险语法（如果必须支持，需要更严格解析与确认）
- 可选增强：在 UI 增加"执行确认"弹窗（即使白名单也确认）

### 6) 与"Agent"形态的最小闭环 → 长期演进

**Demo 阶段（V1）**
- Chat：用户问答（LLM）
- Tools：用户手动触发运行白名单命令（不让模型直接触发执行）

**V2 演进：模型建议 + 用户确认**
- 模型通过 function calling 返回"工具调用请求"（JSON 结构）
- GUI 弹出确认框 / CLI 要求用户输入 y/n
- 用户同意后由 Go 端执行，执行结果回传给模型继续对话

**V3 演进：复杂工具调用链**
- 多步骤编排：模型一次返回多个工具调用 → Go 端调度执行（支持并行）→ 结果聚合 → 继续对话
- Go 端 ToolChain 引擎：
  - 基于 goroutine + channel 的并行执行
  - context 超时控制与取消
  - 工具间依赖图（DAG）编排
  - 中间结果流式推送到前端

### 7) 目录结构（最终版）

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
│   ├── llm/                     # LLM 客户端（流式请求、SSE 解析）
│   │   ├── client.go            # HTTP 客户端、流式读取
│   │   ├── sse.go               # SSE 事件解析
│   │   └── types.go             # 请求/响应结构体
│   ├── command/                 # 命令执行器（白名单校验、os/exec）
│   │   ├── runner.go            # 命令执行 + 流式输出
│   │   ├── whitelist.go         # 白名单定义与校验
│   │   └── types.go
│   ├── storage/                 # 安全存储（api_key 加密、配置管理）
│   │   ├── keyring.go           # 系统 Keychain（优先）
│   │   ├── file.go              # 本地加密文件（退化方案）
│   │   └── config.go            # 配置读写
│   ├── conversation/            # 会话历史管理
│   │   ├── store.go             # 存储接口
│   │   └── types.go
│   └── toolchain/               # 工具链引擎（V3 演进用）
│       ├── engine.go            # 调度引擎
│       ├── dag.go               # 依赖图
│       └── types.go
│
├── frontend/                    # GUI 专用前端（React + TS + Vite）
│   ├── src/
│   │   ├── components/          # UI 组件
│   │   ├── pages/               # Chat / Settings / Command 页面
│   │   ├── hooks/               # 自定义 hooks（useChat / useCommand 等）
│   │   ├── api/                 # Wails binding 调用封装
│   │   │   └── wailsjs/         # Wails 自动生成的 binding
│   │   ├── types/               # TypeScript 类型定义
│   │   ├── App.tsx
│   │   └── main.tsx
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
│
├── packages/
│   └── agentforge/              # npm 分发包（CLI wrapper）
│       ├── package.json         # npm 包配置（name: agentforge）
│       ├── install.js           # postinstall：检测平台 + 下载二进制
│       ├── bin/
│       │   └── agentforge.js    # 入口脚本，调用 Go 二进制
│       └── README.md
│
├── build/                       # Wails 构建产物与图标资源
│   └── appicon.png
│
├── scripts/                     # 构建 & 发布脚本
│   ├── build-cli.sh             # 编译 CLI（多平台交叉编译）
│   ├── build-gui.sh             # 编译 GUI（wails build）
│   ├── build-npm.sh             # 打包 npm wrapper
│   └── release.sh               # 发布到 GitHub Releases + npm
│
├── go.mod                       # Go 模块（github.com/user/agentforge）
├── go.sum
├── wails.json                   # Wails 项目配置
├── Makefile                     # 统一构建入口
└── .goreleaser.yml              # GoReleaser 配置（可选，自动化发布）
```

### 8) 依赖选择

**前端（GUI 专用）**
- React + Vite + TypeScript
- Markdown 渲染（如需要）：react-markdown（仅展示，禁用危险 HTML）
- 代码高亮：highlight.js 或 prismjs

**Go 后端（共享）**
- HTTP 请求：net/http（标准库，无需第三方）
- JSON 处理：encoding/json（标准库）
- SSE 解析：bufio.Scanner 手动解析（标准库足够）
- 安全存储：github.com/zalando/go-keyring（系统 Keychain）
- 配置管理：自定义 JSON 文件 或 viper
- CLI 框架：github.com/spf13/cobra（命令行参数解析）
- 数据存储（会话历史）：SQLite（github.com/mattn/go-sqlite3）或 JSON 文件

说明：第一版以"最少依赖能跑"为准，优先使用 Go 标准库，后续按需引入。

### 9) 构建与发布流程

**构建命令（通过 Makefile 统一管理）**
```makefile
make build-cli          # 编译 CLI 二进制（多平台交叉编译）
make build-gui          # wails build（生成桌面安装包）
make build-npm          # 将 CLI 二进制打包进 npm wrapper
make build-all          # 以上全部
make release            # 构建并发布到 GitHub Releases + npm publish
```

**发布渠道**
- CLI：npm registry（`npm publish`）
- GUI 桌面客户端：GitHub Releases（自动上传安装包）
- 可选增强：使用 GoReleaser 自动化 CI/CD 流程

### 10) 开发工作流

**纯前端调试（Web 端）**
- `cd frontend && npm run dev` — 浏览器调试 UI，Go 方法需 Mock
- 适合调试组件、样式、交互逻辑

**桌面端调试（完整功能）**
- `wails dev` — 启动完整桌面窗口，前后端联调，支持热重载
- Go 端日志输出到终端，前端可通过 DevTools 调试

**CLI 调试**
- `go run ./cmd/cli/ chat "hello"` — 直接运行 CLI 测试
- 不需要 Wails 环境，只依赖 Go 标准库

**Web 端 Mock 适配（前端开发用）**
```typescript
const isWails = typeof window !== 'undefined' && window.go !== undefined;

export const api = {
  chat: {
    send: isWails
      ? (msg: string) => window.go.main.App.Chat(msg)
      : (msg: string) => mockChatSend(msg),
  },
  command: {
    run: isWails
      ? (id: string) => window.go.main.App.RunCommand(id)
      : (id: string) => mockCommandRun(id),
  }
};
```

---

## ⚠️ 注意事项

### 开发阶段

1. **internal/ 包的约束**：Go 的 `internal/` 目录只能被其父目录下的代码导入。所有 CLI 和 GUI 入口都在项目根目录下，因此都能访问 `internal/`，但外部项目无法导入——这正好保证了业务逻辑的封装性。

2. **Wails 与 CLI 的编译分离**：
   - CLI 编译：`go build -o agentforge ./cmd/cli/`，不需要 Wails 依赖，产物小
   - GUI 编译：`wails build`，需要 Wails SDK + WebView，产物包含 GUI 资源
   - 两者共享 `internal/` 下的所有业务逻辑代码

3. **交叉编译注意**：
   - Go CLI 交叉编译简单（`GOOS=windows GOARCH=amd64 go build`）
   - Wails GUI 交叉编译较复杂，建议在对应平台上构建（或使用 GitHub Actions 多平台 Runner）

4. **前端 Mock 开发**：纯浏览器开发时，所有 Go binding 不可用，需要在前端 `api/` 层做 Mock 适配，否则会报错。

5. **CORS 问题**：如果前端在纯浏览器模式下直接调用 OpenAI API，会遇到跨域问题。解决方案：
   - 开发时在 Vite 配置代理（`vite.config.ts` 的 `server.proxy`）
   - 生产环境由 Go 后端代理请求（GUI 模式天然无此问题）

### 安全注意事项

6. **api_key 保护**：
   - 不写入日志文件
   - 不出现在进程参数中（`ps aux` 不可见）
   - 不通过 URL query string 传递
   - GUI 模式优先使用系统 Keychain 存储
   - CLI 模式使用本地加密文件存储（AES-GCM）

7. **命令执行安全**：
   - 白名单策略：只允许预设命令，不允许用户自由输入
   - 参数校验：禁止管道 `|`、重定向 `>` `<`、命令替换 `$()` 等高风险语法
   - 执行确认：V2 开始所有模型触发的命令都必须用户确认
   - 超时控制：所有命令执行必须有 context 超时，防止进程挂起

8. **npm 包安全**：
   - postinstall 脚本需要校验下载二进制的 checksum（SHA256）
   - 建议从 GitHub Releases（带签名）下载，不要用未经验证的 CDN
   - npm 包不要包含 api_key 等敏感信息的默认值

### 跨平台注意事项

9. **Windows WebView2 依赖**：
   - Windows 10 (1803+) 和 Windows 11 已预装 WebView2
   - Windows 7/8 不支持，需提示用户安装
   - Wails 打包时可选择内置 WebView2 Bootstrapper（自动安装，但包体增大 ~2MB）

10. **Shell 差异**：
    - Windows 默认用 PowerShell（`powershell.exe -NoProfile -ExecutionPolicy Bypass -Command`）
    - macOS/Linux 默认用 bash（`bash -lc`）
    - 白名单命令需按平台分别定义，注意 `platform` 字段的过滤

11. **路径处理**：
    - 配置文件路径：使用 `os.UserConfigDir()` 获取平台标准路径
      - Windows：`%APPDATA%/agentforge/`
      - macOS：`~/Library/Application Support/agentforge/`
      - Linux：`~/.config/agentforge/`
    - 数据文件路径：使用 `os.UserHomeDir()` 或 `os.UserCacheDir()`
    - 不要硬编码路径分隔符，统一使用 `filepath.Join()`

12. **编码问题**：
    - Windows 中文环境：PowerShell 输出可能使用 GBK 编码，需要转换为 UTF-8
    - Go 端使用 `golang.org/x/text/encoding` 处理编码转换
    - 前端统一使用 UTF-8

### 架构与演进注意事项

13. **GUI 和 CLI 的功能同步**：
    - 所有新功能必须先在 `internal/` 中实现
    - CLI 和 GUI 只是调用层，不要在 `cmd/cli/` 或 `cmd/gui/` 中写业务逻辑
    - 这样可以保证两种模式的功能始终一致

14. **Wails v2 → v3 升级**：
    - Wails v3 正在开发中，API 可能有较大变化
    - 建议关注 Wails 官方迁移指南，GUI 层代码需要适配
    - `internal/` 中的业务逻辑不受影响

15. **流式输出的实现细节**：
    - SSE 解析要处理 `data: [DONE]` 标记
    - 网络断开时需要优雅降级（提示用户而非崩溃）
    - 并发对话时每个对话使用独立的 goroutine 和 context

16. **SQLite vs JSON 的选择**：
    - V1 建议先用 JSON 文件存储会话历史（简单，无需 CGO 依赖）
    - 数据量增大后再迁移到 SQLite
    - 注意 `go-sqlite3` 需要 CGO，交叉编译时需额外配置

---

## Assumptions & Decisions

- 决策：项目命名为 **AgentForge**。
- 决策：选择 Wails v2 + Go + React + TS + Vite 作为技术栈（面向长期演进）。
- 决策：支持 CLI + GUI 双模式，共享 Go 核心引擎（`internal/`）。
- 决策：CLI 通过 npm 分发（wrapper + Go 预编译二进制），GUI 通过桌面安装包分发。
- 决策：LLM 先对接 OpenAI 兼容 API（base_url/api_key/model 可配置）。
- 决策：命令执行默认白名单模式，模型不直接触发命令执行（V1）。
- 决策：V2/V3 演进支持 function calling + 工具调用链（Go 并发优势）。
- 假设：需要跨平台（Windows/macOS/Linux）。
- 假设：目标用户机器已安装 WebView2 Runtime（Win10/11 预装）。
- 假设：npm 分发面向开发者群体，GUI 桌面端面向更广泛的用户群体。

---

## Verification Steps (Acceptance)

### CLI 模式验证
- `agentforge config set base_url <url>` 能正确保存配置
- `agentforge config set api_key <key>` 能加密存储
- `agentforge chat "hello"` 能返回模型回复（终端流式输出）
- `agentforge run sysinfo` 能执行白名单命令并显示输出

### GUI 模式验证（Windows）
- 能通过 `wails dev` 启动应用并打开窗口
- Settings 填入 base_url/api_key/model 后，Chat 能返回模型回复
- 执行一个白名单 PowerShell 命令（如获取系统信息）并实时显示输出
- api_key 不出现在任何日志/控制台输出

### GUI 模式验证（macOS/Linux）
- 能启动应用
- 执行一个白名单 bash 命令并实时显示输出

### 安全验收（最低要求）
- 前端无法直接执行系统命令，所有操作必须通过 Go binding（GUI 模式）
- npm 包 postinstall 下载的二进制通过 SHA256 校验
- api_key 不出现在进程参数、日志、网络 query string 中

### npm 分发验证
- `npm install -g agentforge` 能在 Windows/macOS/Linux 正确安装
- 安装后 `agentforge --version` 能正确输出版本号
- 安装后 `agentforge chat "hello"` 能正常工作
