# 跨会话 Memory 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 为 AgentForge 增加跨会话 Memory——agent 在对话中自主写入「值得长期记住的事实」，每次会话开头把记忆索引导入上下文，用户可在设置页查看/编辑/删除。

**架构：** 记忆以 markdown 文件存于 `<workdir>/.agentforge/memory/`（无 workdir 时 fallback 到 `<appdata>/memory/`）。新增 `internal/memory` 包负责 CRUD + frontmatter 手写解析 + MEMORY.md 索引自动同步。agent 经 `Deps.Memory`（Provider 接口）在 Skills 后、base 前注入索引；agent 用专用工具 `memory_save/read/delete` 自动写入；前端经 `/api/memory/*` 在 SettingsModal 的 Memory tab 管理。两条写入路径（工具 / API）都落到同一批 `.md` 文件。

**技术栈：** Go（chi router、`mattn/go-sqlite3`、`tools.Tool` 接口、手写 frontmatter 零依赖）；React/TS（zustand、统一 `api.ts`、语义令牌 + `Icon.tsx`）。

**前置约定：**
- Go 构建/测试前必须 `unset GOROOT`（本机 `.g` 与 toolchain 冲突，见 memory `go-build-goroot-workaround`）。
- 注释用中文（项目约定）。
- commit message 用中文 conventional commits。

---

## 文件结构

### 新建
| 文件 | 职责 |
|---|---|
| `internal/memory/memory.go` | 类型（`Type`/`Entry`）、常量、`Provider`/`Store` 接口、`MemoryStore`、`New`、`ResolveDir`、name 校验 |
| `internal/memory/frontmatter.go` | frontmatter 手写序列化/解析（`formatEntry`/`parseEntry`），零依赖 |
| `internal/memory/index.go` | `Reindex`（生成 MEMORY.md）、`IndexContext`（注入文本）、扫描公共逻辑 |
| `internal/memory/store.go` | `List`/`Get`/`Save`/`Delete`（原子写 + reindex） |
| `internal/memory/tools.go` | `memory_save`/`memory_read`/`memory_delete`，实现 `tools.Tool`；`Tools(store)` 导出注册集合 |
| `internal/memory/*_test.go` | 分文件单测 |
| `internal/server/handler_memory.go` | `GET/PUT/DELETE /api/memory`、`GET /api/memory/{name}` |
| `frontend/src/stores/memoryStore.ts` | zustand：`entries/load/save/remove` |
| `frontend/src/components/MemoryPanel.tsx` | 列表（按 type 分组）+ 编辑区 + 新建/删除 |

### 修改
| 文件 | 改动 |
|---|---|
| `internal/agent/types.go` | `Deps` 加 `Memory memory.Provider`；新增 `memory.Provider` 接口 |
| `internal/agent/agent.go` | `Run()` 在 Skills 块后、`baseSystemPrompt` 前注入 `Memory.IndexContext()` |
| `internal/server/handler_chat.go` | `ChatHandler` 加 `Memory agent.MemoryProvider`；`agent.New(Deps{..., Memory: h.Memory})` |
| `internal/server/router.go` | `Deps` 加 `Memory *memory.MemoryStore`；注册 `MemoryHandler`；传给 `ChatHandler` |
| `main.go` | 构造 `memStore := memory.New(workDir.Get, *dataDir)`；注入 `server.Deps.Memory` 与 registry |
| `frontend/src/lib/api.ts` | `listMemory/getMemory/saveMemory/deleteMemory` |
| `frontend/src/types.ts` | `MemoryType`、`MemoryEntry` |
| `frontend/src/components/SettingsModal.tsx` | TABS 加 `memory`；渲染 `<MemoryPanel/>` |
| `frontend/src/components/Icon.tsx` | IconName 加 `brain` + PATHS |

---

## 任务 1：memory 包 — 类型、校验、frontmatter 序列化/解析

**文件：**
- 创建：`internal/memory/memory.go`
- 创建：`internal/memory/frontmatter.go`
- 测试：`internal/memory/frontmatter_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/memory/frontmatter_test.go`：

```go
package memory

import "testing"

func TestFormatAndParseRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   Entry
	}{
		{"simple", Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "正文内容"}},
		{"colon-in-desc", Entry{Name: "x", Description: "含: 冒号的描述", Type: TypeUser, Body: "b"}},
		{"quote-in-desc", Entry{Name: "y", Description: `带"引号"的`, Type: TypeFeedback, Body: "**Why:** 1\n**How to apply:** 2"}},
		{"multiline-body", Entry{Name: "z", Description: "多行", Type: TypeReference, Body: "第一行\n\n第二行\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := formatEntry(c.in)
			got, err := parseEntry(c.in.Name, raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got.Name != c.in.Name || got.Description != c.in.Description ||
				got.Type != c.in.Type || got.Body != c.in.Body {
				t.Errorf("round-trip mismatch\nwant=%+v\ngot =%+v\nraw=%q", c.in, got, raw)
			}
		})
	}
}

func TestParseSkipsFrontmatterWhenAbsent(t *testing.T) {
	// 无 frontmatter 的纯正文文件：Name 来自参数，其余为零值，body 为全文。
	got, err := parseEntry("plain", "只是一段正文\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Name != "plain" || got.Body != "只是一段正文\n" || got.Type != "" {
		t.Errorf("unexpected: %+v", got)
	}
}
```

创建 `internal/memory/memory.go`（仅类型/常量/校验，供测试编译引用）：

```go
// Package memory 提供跨会话的事实记忆：以 markdown 文件存储（frontmatter + 正文），
// 自动维护 MEMORY.md 索引，供 agent 注入上下文与用户在 UI 管理。
package memory

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

// Type 记忆分类，决定正文结构约定。
type Type string

const (
	TypeUser      Type = "user"      // 用户是谁：角色、专长、偏好
	TypeFeedback  Type = "feedback"  // 用户给的工作指导（带 Why/How to apply）
	TypeProject   Type = "project"   // 进行中的工作/目标/约束
	TypeReference Type = "reference" // 外部资源指针（URL/工单）
)

// ValidType 校验 type 取值合法。
func ValidType(t Type) bool {
	switch t {
	case TypeUser, TypeFeedback, TypeProject, TypeReference:
		return true
	}
	return false
}

// Entry 一条记忆：frontmatter + 正文。UpdatedAt 来自文件 mtime，不序列化。
type Entry struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Type        Type      `json:"type"`
	Body        string    `json:"body"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// 目录、索引与上限常量（规格 §11 默认值）。
const (
	DirName      = ".agentforge/memory"
	IndexFile    = "MEMORY.md"
	MaxBodyBytes = 8 << 10 // 8 KB
	MaxDescRunes = 200
	MaxNameLen   = 64
)

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName 校验 name：kebab-case、长度 1-64，杜绝路径穿越。
func ValidName(name string) error {
	if len(name) == 0 || len(name) > MaxNameLen {
		return fmt.Errorf("name 长度需在 1-%d 之间", MaxNameLen)
	}
	if !nameRe.MatchString(name) {
		return errors.New("name 必须是 kebab-case（仅小写字母/数字/连字符）")
	}
	return nil
}

// Provider 给 agent 的最小依赖面：只读索引注入。
type Provider interface {
	IndexContext() string
}

// Store 完整读写能力（server / 工具 / API 用）。
type Store interface {
	Provider
	List() ([]Entry, error)
	Get(name string) (Entry, error)
	Save(e Entry) error
	Delete(name string) error
}

// MemoryStore 基于 workdir/appdata 的文件实现。
type MemoryStore struct {
	workdir func() string // 返回当前 workdir；空串触发 fallback
	appdata string        // fallback 根目录
}

// New 构造 MemoryStore。workdir 为当前工作目录取值函数，appdata 为 fallback 根。
func New(workdir func() string, appdata string) *MemoryStore {
	return &MemoryStore{workdir: workdir, appdata: appdata}
}

// ResolveDir 定位记忆目录：workdir 非空 → <workdir>/.agentforge/memory；
// 否则 → <appdata>/memory。目录不存在不创建（由 Save 惰性创建）。
func (s *MemoryStore) ResolveDir() (string, error) {
	if wd := s.workdir(); wd != "" {
		return filepath.Join(wd, DirName), nil
	}
	if s.appdata == "" {
		return "", errors.New("无可用的记忆目录（workdir 与 appdata 均为空）")
	}
	return filepath.Join(s.appdata, "memory"), nil
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`unset GOROOT && go test ./internal/memory/ -run TestFormatAndParseRoundTrip -v`
预期：FAIL（`formatEntry`/`parseEntry` 未定义，编译错误）。

- [ ] **步骤 3：编写实现**

创建 `internal/memory/frontmatter.go`：

```go
package memory

import (
	"errors"
	"strings"
)

// escapeScalar 把字符串安全地包进双引号（YAML 双引号标量），内部 \ 与 " 转义。
// description 可能含冒号/特殊字符，统一引号包裹避免解析歧义。
func escapeScalar(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func unescapeScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		r := strings.NewReplacer(`\"`, `"`, `\\`, `\`)
		return r.Replace(inner)
	}
	return s
}

// splitKV 把 "key: value" 拆为 key、value；无冒号或空行返回 ok=false。
func splitKV(line string) (key, val string, ok bool) {
	i := strings.Index(line, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// formatEntry 把 Entry 序列化为完整 .md 文件内容（frontmatter + 正文）。
func formatEntry(e Entry) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + e.Name + "\n")
	sb.WriteString("description: " + escapeScalar(e.Description) + "\n")
	sb.WriteString("type: " + string(e.Type) + "\n")
	sb.WriteString("---\n\n")
	body := strings.TrimLeft(e.Body, "\n") // 去多余前导换行
	sb.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		sb.WriteString("\n")
	}
	return sb.String()
}

const fmOpen = "---\n"

// parseEntry 解析 .md 文件内容为 Entry。frontmatter 仅支持扁平 key: value；
// 无 frontmatter 时视为纯正文，Name 取参数传入。
func parseEntry(name, raw string) (Entry, error) {
	e := Entry{Name: name}
	body := raw
	if strings.HasPrefix(raw, fmOpen) {
		rest := raw[len(fmOpen):]
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			fm := rest[:end]
			// 正文从结束标记那行的换行之后开始。
			after := rest[end+len("\n---"):]
			body = strings.TrimPrefix(after, "\n")
			for _, line := range strings.Split(fm, "\n") {
				k, v, ok := splitKV(line)
				if !ok {
					continue
				}
				switch k {
				case "name":
					e.Name = v
				case "description":
					e.Description = unescapeScalar(v)
				case "type":
					e.Type = Type(v)
				}
			}
		}
	}
	e.Body = body
	return e, nil
}

// errParseSkip 占位，便于未来扩展（如严格模式返回错误）。
var errParseSkip = errors.New("parse skip")
```

- [ ] **步骤 4：运行测试验证通过**

运行：`unset GOROOT && go test ./internal/memory/ -v`
预期：PASS（`TestFormatAndParseRoundTrip`、`TestParseSkipsFrontmatterWhenAbsent` 全过）。

- [ ] **步骤 5：Commit**

```bash
git add internal/memory/memory.go internal/memory/frontmatter.go internal/memory/frontmatter_test.go
git commit -m "feat(memory): 记忆类型定义与 frontmatter 手写序列化/解析"
```

---

## 任务 2：memory 包 — Store 读写（List/Get/Save/Delete）

**文件：**
- 创建：`internal/memory/store.go`
- 测试：`internal/memory/store_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/memory/store_test.go`：

```go
package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore 在临时目录建一个 MemoryStore，workdir 指向该目录。
func newTestStore(t *testing.T) (*MemoryStore, string) {
	t.Helper()
	wd := t.TempDir()
	s := New(func() string { return wd }, "")
	return s, wd
}

func TestSaveAndList(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.Save(Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "正文"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "go-env" || got[0].Description != "Go 环境坑" ||
		got[0].Type != TypeProject || got[0].Body != "正文\n" {
		t.Fatalf("unexpected list: %+v", got)
	}
	if got[0].UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt should come from file mtime, got zero")
	}
}

func TestSaveRejectsBadName(t *testing.T) {
	s, _ := newTestStore(t)
	for _, bad := range []string{"", "UPPER", "has space", "../etc", "a/b"} {
		if err := s.Save(Entry{Name: bad, Description: "d", Type: TypeUser}); err == nil {
			t.Errorf("expected error for name %q", bad)
		}
	}
}

func TestSaveRejectsBadTypeAndOversize(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Save(Entry{Name: "ok", Description: "d", Type: "bogus"}); err == nil {
		t.Errorf("expected error for bad type")
	}
	longBody := make([]byte, MaxBodyBytes+1)
	for i := range longBody {
		longBody[i] = 'x'
	}
	if err := s.Save(Entry{Name: "ok", Description: "d", Type: TypeUser, Body: string(longBody)}); err == nil {
		t.Errorf("expected error for oversize body")
	}
}

func TestSaveUpdatesExisting(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "x", Description: "旧", Type: TypeUser, Body: "a"})
	// 让 mtime 推进，避免某些文件系统同毫秒覆盖
	time.Sleep(15 * time.Millisecond)
	_ = s.Save(Entry{Name: "x", Description: "新", Type: TypeUser, Body: "b"})
	got, _ := s.List()
	if len(got) != 1 || got[0].Description != "新" || got[0].Body != "b\n" {
		t.Fatalf("update failed: %+v", got)
	}
}

func TestGetAndDelete(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "x", Description: "d", Type: TypeUser, Body: "正文"})
	e, err := s.Get("x")
	if err != nil || e.Body != "正文\n" {
		t.Fatalf("get: %v %+v", err, e)
	}
	if _, err := s.Get("missing"); err == nil {
		t.Errorf("expected error for missing get")
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := s.List()
	if len(got) != 0 {
		t.Fatalf("expected empty after delete, got %+v", got)
	}
}

func TestListSkipsIndexFile(t *testing.T) {
	s, wd := newTestStore(t)
	_ = s.Save(Entry{Name: "x", Description: "d", Type: TypeUser, Body: "b"})
	// 列表不应包含 MEMORY.md
	_ = os.WriteFile(filepath.Join(wd, DirName, IndexFile), []byte("# Memory Index"), 0o644)
	got, _ := s.List()
	for _, e := range got {
		if e.Name == "MEMORY" {
			t.Fatalf("index file leaked into list: %+v", got)
		}
	}
}

func TestListSkipsUnparseable(t *testing.T) {
	s, wd := newTestStore(t)
	_ = s.Save(Entry{Name: "good", Description: "d", Type: TypeUser, Body: "b"})
	// 一个 name 合法但内容非 .md 的文件应被跳过而非中断
	_ = os.WriteFile(filepath.Join(wd, DirName, "bad.md"), []byte("\x00\x01garbage"), 0o644)
	got, _ := s.List()
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("expected only good entry, got %+v", got)
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`unset GOROOT && go test ./internal/memory/ -run 'TestSave|TestGet|TestList' -v`
预期：FAIL（`Save`/`List`/`Get`/`Delete` 未定义）。

- [ ] **步骤 3：编写实现**

创建 `internal/memory/store.go`：

```go
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// ErrNotFound 记忆条目不存在。
var ErrNotFound = errors.New("memory entry not found")

// pathOf 返回记忆目录下某条目的 .md 路径；name 必须已通过 ValidName。
func (s *MemoryStore) pathOf(name string) (string, error) {
	dir, err := s.ResolveDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".md"), nil
}

// List 扫描记忆目录所有 *.md（排除 MEMORY.md），解析为条目，按 mtime 倒序。
func (s *MemoryStore) List() ([]Entry, error) {
	dir, err := s.ResolveDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil // 目录尚未创建视为空
		}
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, fe := range entries {
		if fe.IsDir() || fe.Name() == IndexFile {
			continue
		}
		name := stringsTrimSuffix(fe.Name(), ".md")
		if name == fe.Name() || !isValidNameQuiet(name) {
			continue // 非 .md 或 name 非法：跳过
		}
		raw, err := os.ReadFile(filepath.Join(dir, fe.Name()))
		if err != nil || !utf8.Valid(raw) {
			continue // 读取失败或非 UTF-8：跳过该条，不中断
		}
		e, _ := parseEntry(name, string(raw))
		fi, err := fe.Info()
		if err == nil {
			e.UpdatedAt = fi.ModTime()
		}
		out = append(out, e)
	}
	sortByMtimeDesc(out)
	return out, nil
}

// Get 读取单条；不存在返回 ErrNotFound。
func (s *MemoryStore) Get(name string) (Entry, error) {
	if err := ValidName(name); err != nil {
		return Entry{}, err
	}
	p, err := s.pathOf(name)
	if err != nil {
		return Entry{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	e, _ := parseEntry(name, string(raw))
	fi, err := os.Stat(p)
	if err == nil {
		e.UpdatedAt = fi.ModTime()
	}
	return e, nil
}

// Save 校验并写入条目（原子写：临时文件 + rename），成功后触发 reindex。
func (s *MemoryStore) Save(e Entry) error {
	if err := ValidName(e.Name); err != nil {
		return err
	}
	if !ValidType(e.Type) {
		return fmt.Errorf("非法 type：%q", e.Type)
	}
	if len([]rune(e.Description)) > MaxDescRunes {
		return fmt.Errorf("description 超过 %d 字", MaxDescRunes)
	}
	if len(e.Body) > MaxBodyBytes {
		return fmt.Errorf("正文超过 %d 字节", MaxBodyBytes)
	}
	dir, err := s.ResolveDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建记忆目录: %w", err)
	}
	final := filepath.Join(dir, e.Name+".md")
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 若 rename 成功则 Remove 无效；失败则清理
	if _, err := tmp.WriteString(formatEntry(e)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return err
	}
	return s.Reindex()
}

// Delete 删除条目并 reindex；不存在返回 ErrNotFound。
func (s *MemoryStore) Delete(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	p, err := s.pathOf(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return s.Reindex()
}
```

同时创建 `internal/memory/util.go`（store.go 引用的两个小工具）：

```go
package memory

import (
	"sort"
	"strings"
)

func stringsTrimSuffix(s, suffix string) string { return strings.TrimSuffix(s, suffix) }

// isValidNameQuiet 不返回错误，仅布尔，用于扫描过滤。
func isValidNameQuiet(name string) bool { return ValidName(name) == nil }

// sortByMtimeDesc 原地按 UpdatedAt 倒序（最近更新在前）。
func sortByMtimeDesc(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool { return es[i].UpdatedAt.After(es[j].UpdatedAt) })
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`unset GOROOT && go test ./internal/memory/ -v`
预期：全部 PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/memory/store.go internal/memory/util.go internal/memory/store_test.go
git commit -m "feat(memory): Store 读写(List/Get/Save/Delete) 与原子写"
```

---

## 任务 3：memory 包 — Reindex + IndexContext

**文件：**
- 创建：`internal/memory/index.go`
- 测试：`internal/memory/index_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/memory/index_test.go`：

```go
package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReindexWritesMemoryMd(t *testing.T) {
	s, wd := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "b"})
	_ = s.Save(Entry{Name: "frontend", Description: "前端设计", Type: TypeFeedback, Body: "b"})

	err := s.Reindex()
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(wd, DirName, IndexFile))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	txt := string(idx)
	if !strings.Contains(txt, "Go 环境坑") || !strings.Contains(txt, "前端设计") {
		t.Errorf("index missing entries: %s", txt)
	}
	if !strings.Contains(txt, "(go-env.md)") || !strings.Contains(txt, "(frontend.md)") {
		t.Errorf("index missing links: %s", txt)
	}
	if !strings.Contains(txt, "· project") || !strings.Contains(txt, "· feedback") {
		t.Errorf("index missing type tags: %s", txt)
	}
}

func TestIndexContextInjectableText(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "b"})
	ctx := s.IndexContext()
	if !strings.Contains(ctx, "Go 环境坑") {
		t.Errorf("context missing entry: %s", ctx)
	}
	if !strings.Contains(ctx, "memory_read") || !strings.Contains(ctx, "memory_save") {
		t.Errorf("context missing tool hints: %s", ctx)
	}
}

func TestIndexContextEmptyWhenNoMemory(t *testing.T) {
	s, _ := newTestStore(t)
	if ctx := s.IndexContext(); ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`unset GOROOT && go test ./internal/memory/ -run 'TestReindex|TestIndexContext' -v`
预期：FAIL（`Reindex`/`IndexContext` 未定义）。

- [ ] **步骤 3：编写实现**

创建 `internal/memory/index.go`：

```go
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// indexLines 生成索引行（不含标题），供 MEMORY.md 落盘与 IndexContext 共用。
// 每行：`- {description} · {type}（{name}.md）`；列表已按 mtime 倒序。
func indexLines(es []Entry, withLink bool) []string {
	lines := make([]string, 0, len(es))
	for _, e := range es {
		if withLink {
			lines = append(lines, fmt.Sprintf("- [%s](%s.md) · %s", e.Description, e.Name, e.Type))
		} else {
			lines = append(lines, fmt.Sprintf("- %s · %s（%s.md）", e.Description, e.Type, e.Name))
		}
	}
	return lines
}

// Reindex 扫描所有条目，重写 MEMORY.md（按 mtime 倒序）。无条目则不写文件。
func (s *MemoryStore) Reindex() error {
	es, err := s.List()
	if err != nil {
		return err
	}
	dir, err := s.ResolveDir()
	if err != nil {
		return err
	}
	if len(es) == 0 {
		// 无条目时若索引文件存在则删除，保持一致
		_ = os.Remove(filepath.Join(dir, IndexFile))
		return nil
	}
	var sb strings.Builder
	sb.WriteString("# Memory Index\n\n")
	for _, l := range indexLines(es, true) {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(dir, IndexFile), []byte(sb.String()), 0o644)
}

// indexInjectHeader 注入给 agent 的引导提示。
const indexInjectHeader = `你拥有一份跨会话的记忆库。以下是当前所有记忆的索引（按最近更新排序）：`

const indexInjectFooter = `
若某条与当前任务相关，调用 memory_read(name) 读取完整内容后再使用。
当用户给出值得长期记住的事实（偏好/约定/环境坑/外部资源），调用 memory_save 记录；重复事实用同名更新，不要新建重复条目。`

// IndexContext 返回注入 agent 的索引文本（运行时生成，不落盘）。无记忆返回空串。
func (s *MemoryStore) IndexContext() string {
	es, err := s.List()
	if err != nil || len(es) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(indexInjectHeader)
	sb.WriteString("\n\n")
	for _, l := range indexLines(es, false) {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	sb.WriteString(indexInjectFooter)
	return sb.String()
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`unset GOROOT && go test ./internal/memory/ -v`
预期：全部 PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/memory/index.go internal/memory/index_test.go
git commit -m "feat(memory): Reindex 生成 MEMORY.md 与 IndexContext 注入文本"
```

---

## 任务 4：memory 工具（memory_save/read/delete）

**文件：**
- 创建：`internal/memory/tools.go`
- 测试：`internal/memory/tools_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/memory/tools_test.go`：

```go
package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/tools"
)

func TestMemorySaveTool(t *testing.T) {
	s, _ := newTestStore(t)
	tt := &saveTool{store: s}
	spec := tt.Spec()
	if spec.Name != "memory_save" {
		t.Fatalf("spec name: %s", spec.Name)
	}
	args := `{"name":"go-env","description":"Go 环境坑","type":"project","body":"正文"}`
	res, err := tt.Run(context.Background(), args, tools.NewAutoAllowGate())
	if err != nil || res.IsError {
		t.Fatalf("run: %v %+v", err, res)
	}
	got, _ := s.List()
	if len(got) != 1 || got[0].Name != "go-env" || got[0].Body != "正文\n" {
		t.Fatalf("not saved: %+v", got)
	}
}

func TestMemoryReadTool(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "d", Type: TypeProject, Body: "正文"})
	rt := &readTool{store: s}
	res, _ := rt.Run(context.Background(), `{"name":"go-env"}`, tools.NewAutoAllowGate())
	if res.IsError || !strings.Contains(res.Content, "正文") {
		t.Fatalf("read result: %+v", res)
	}
}

func TestMemoryDeleteTool(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "d", Type: TypeProject, Body: "b"})
	dt := &deleteTool{store: s}
	res, _ := dt.Run(context.Background(), `{"name":"go-env"}`, tools.NewAutoAllowGate())
	if res.IsError {
		t.Fatalf("delete failed: %+v", res)
	}
	got, _ := s.List()
	if len(got) != 0 {
		t.Fatalf("not deleted: %+v", got)
	}
}

func TestToolsRegistry(t *testing.T) {
	s, _ := newTestStore(t)
	ts := Tools(s)
	names := map[string]bool{}
	for _, tk := range ts {
		names[tk.Spec().Name] = true
	}
	for _, want := range []string{"memory_save", "memory_read", "memory_delete"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`unset GOROOT && go test ./internal/memory/ -run 'TestMemory|TestTools' -v`
预期：FAIL（`saveTool`/`readTool`/`deleteTool`/`Tools` 未定义）。

- [ ] **步骤 3：编写实现**

创建 `internal/memory/tools.go`：

```go
package memory

import (
	"context"
	"encoding/json"

	"github.com/agent-rust/core/internal/tools"
)

// saveTool 实现 memory_save：校验并写入记忆条目，自动维护索引。
type saveTool struct{ store *MemoryStore }

func (t *saveTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "memory_save",
		Description: "记录一条跨会话记忆（仅记「长期有用、代码/git 查不到」的事实：用户偏好、" +
			"工作约定、环境坑、外部资源）。同名 name 会覆盖更新，不要为重复事实新建。"),
		Parameters: `{"type":"object","properties":{
			"name":{"type":"string","description":"kebab-case 唯一标识，如 go-env"},
			"description":{"type":"string","description":"一行摘要，召回时判断相关性用"},
			"type":{"type":"string","enum":["user","feedback","project","reference"],
				"description":"user=用户偏好; feedback=工作指导; project=进行中的工作/约束; reference=外部资源"},
			"body":{"type":"string","description":"markdown 正文；feedback/project 类末尾带 **Why:** 与 **How to apply:**"}
		},"required":["name","description","type","body"]}`,
	}
}

func (t *saveTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Name, Description, Type, Body string
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if err := t.store.Save(Entry{
		Name: p.Name, Description: p.Description, Type: Type(p.Type), Body: p.Body,
	}); err != nil {
		return tools.Result{Content: "memory_save 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "已记忆 " + p.Name}, nil
}

// readTool 实现 memory_read：读取单条记忆全文。
type readTool struct{ store *MemoryStore }

func (t *readTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "memory_read",
		Description: "读取一条记忆的完整内容（frontmatter + 正文）。",
		Parameters:  `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
	}
}

func (t *readTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct{ Name string }
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	e, err := t.store.Get(p.Name)
	if err != nil {
		return tools.Result{Content: "memory_read 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: formatEntry(e)}, nil
}

// deleteTool 实现 memory_delete：删除一条记忆。
type deleteTool struct{ store *MemoryStore }

func (t *deleteTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "memory_delete",
		Description: "删除一条记忆（仅在确认其已过时/有误时使用）。",
		Parameters:  `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
	}
}

func (t *deleteTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct{ Name string }
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if err := t.store.Delete(p.Name); err != nil {
		return tools.Result{Content: "memory_delete 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "已删除记忆 " + p.Name}, nil
}

// Tools 返回全部 memory 工具，供 main.go 注册进 tools.Registry。
func Tools(s *MemoryStore) []tools.Tool {
	return []tools.Tool{&saveTool{store: s}, &readTool{store: s}, &deleteTool{store: s}}
}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`unset GOROOT && go test ./internal/memory/ -v`
预期：全部 PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/memory/tools.go internal/memory/tools_test.go
git commit -m "feat(memory): memory_save/read/delete 工具"
```

---

## 任务 5：agent 集成（Deps.Memory + Run 注入）

**文件：**
- 修改：`internal/agent/types.go`
- 修改：`internal/agent/agent.go`
- 测试：`internal/agent/memory_inject_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/agent/memory_inject_test.go`：

```go
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/llm"
)

// fakeMemory 实现 MemoryProvider，返回固定索引文本。
type fakeMemory struct{ ctx string }

func (f fakeMemory) IndexContext() string { return f.ctx }

// fakeStreamLLM 捕获收到的 history 用于断言，返回一次空文本即结束。
type fakeStreamLLM struct{ got []llm.Message }

func (f *fakeStreamLLM) ChatStream(ctx context.Context, msgs []llm.Message, tools []llm.ToolSpec) (<-chan llm.Chunk, error) {
	f.got = append([]llm.Message(nil), msgs...)
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Text: "done", Done: true}
	close(ch)
	return ch, nil
}

// Chat / Embed 满足 llm.LLMClient 接口（本测试不用，仅占位）。
func (f *fakeStreamLLM) Chat(ctx context.Context, msgs []llm.Message) (string, error) {
	return "done", nil
}
func (f *fakeStreamLLM) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	return nil, nil
}

func TestRunInjectsMemoryIndex(t *testing.T) {
	mem := fakeMemory{ctx: "【记忆索引占位】USER_PREF_DARK_THEME"}
	ll := &fakeStreamLLM{}
	emit := &collectEmit{}
	a := New(Deps{
		LLM:    ll,
		Memory: mem,
	})
	a.Run(context.Background(), RunInput{
		History:      []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:         emit,
	})
	// history[0] 是合并后的 system 消息，应含记忆索引
	if len(ll.got) == 0 {
		t.Fatal("no history captured")
	}
	sys := ll.got[0]
	if sys.Role != llm.RoleSystem {
		t.Fatalf("first msg role = %v, want system", sys.Role)
	}
	if !strings.Contains(sys.Content, "【记忆索引占位】USER_PREF_DARK_THEME") {
		t.Errorf("memory index not injected: %q", sys.Content)
	}
}

func TestRunSkipsInjectionWhenMemoryNil(t *testing.T) {
	ll := &fakeStreamLLM{}
	emit := &collectEmit{}
	a := New(Deps{LLM: ll})
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:    emit,
	})
	if len(ll.got) == 0 {
		t.Fatal("no history captured")
	}
	// Memory 为 nil 时，首条应是 baseSystemPrompt（无记忆）
	if strings.Contains(ll.got[0].Content, "记忆索引") {
		t.Errorf("should not inject memory when nil")
	}
}

// collectEmit 吞掉所有事件（agent.Run 需要 EventEmitter）。
type collectEmit struct{}

func (c *collectEmit) Emit(event string, data any) {}
```

> 说明：`llm.Chunk`（`Text`/`Done`）、`llm.Message`（`Role`/`Content`）、`LLMClient` 接口（`ChatStream`/`Chat`/`Embed`）字段与签名已对照 `internal/llm/client.go:14-65` 确认，`fakeStreamLLM` 已补全 `Chat`/`Embed` 以满足接口。

- [ ] **步骤 2：运行测试验证失败**

运行：`unset GOROOT && go test ./internal/agent/ -run TestRunInjectsMemoryIndex -v`
预期：FAIL（`Deps.Memory` 字段 / `MemoryProvider` 接口未定义，编译错误）。

- [ ] **步骤 3：编写实现**

修改 `internal/agent/types.go`：在 `Deps` 结构体中、`Skills` 字段后增加 `Memory` 字段。

```go
// 在 types.go 的 Deps 结构体中追加（位于 Skills SkillProvider 之后）：
	Memory MemoryProvider // may be nil when memory is off
```

并在 types.go 中（`SkillProvider` 接口附近）新增接口：

```go
// MemoryProvider 是 agent 注入记忆索引所需的最小接口。
// 实现者（internal/memory.MemoryStore）返回当前所有记忆的索引文本；无记忆返回空串。
type MemoryProvider interface {
	IndexContext() string
}
```

修改 `internal/agent/agent.go` 的 `Run()`：在 Skills 注入块**之后**（约 `agent.go:119` 之后）、`history = prependSystemContext(history, baseSystemPrompt)`（`agent.go:124`）**之前**插入：

```go
	// 注入记忆索引：跨会话事实背景。放在 skills 之后、base 之前，
	// 使其在最终 system 内容中位于 base（工具路由策略）与 skills 之间。
	if a.deps.Memory != nil {
		if idx := a.deps.Memory.IndexContext(); strings.TrimSpace(idx) != "" {
			history = prependSystemContext(history, idx)
		}
	}
```

- [ ] **步骤 4：运行测试验证通过**

运行：`unset GOROOT && go test ./internal/agent/ -run 'TestRunInjectsMemory|TestRunSkipsInjection' -v`
预期：PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/agent/types.go internal/agent/agent.go internal/agent/memory_inject_test.go
git commit -m "feat(agent): Deps 注入 MemoryProvider 并在 Run 中注入记忆索引"
```

---

## 任务 6：HTTP API（handler_memory + router 注册）

**文件：**
- 创建：`internal/server/handler_memory.go`
- 修改：`internal/server/router.go`
- 测试：`internal/server/handler_memory_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `internal/server/handler_memory_test.go`：

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/memory"
	"github.com/go-chi/chi/v5"
)

func newMemoryTestHandler(t *testing.T) (*MemoryHandler, *memory.MemoryStore) {
	t.Helper()
	wd := t.TempDir()
	s := memory.New(func() string { return wd }, "")
	return &MemoryHandler{Store: s}, s
}

func TestMemoryListEmpty(t *testing.T) {
	h, _ := newMemoryTestHandler(t)
	r := chi.NewRouter()
	r.Get("/api/memory", h.List)
	req := httptest.NewRequest("GET", "/api/memory", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Entries []memory.Entry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 0 {
		t.Fatalf("expected empty, got %+v", body.Entries)
	}
}

func TestMemoryPutAndDelete(t *testing.T) {
	h, _ := newMemoryTestHandler(t)
	r := chi.NewRouter()
	r.Put("/api/memory/{name}", h.Put)
	r.Delete("/api/memory/{name}", h.Delete)

	body := `{"description":"Go 环境坑","type":"project","body":"正文"}`
	req := httptest.NewRequest("PUT", "/api/memory/go-env", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("DELETE", "/api/memory/go-env", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMemoryPutRejectsBadName(t *testing.T) {
	h, _ := newMemoryTestHandler(t)
	r := chi.NewRouter()
	r.Put("/api/memory/{name}", h.Put)
	req := httptest.NewRequest("PUT", "/api/memory/..", strings.NewReader(`{"description":"d","type":"user","body":"b"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`unset GOROOT && go test ./internal/server/ -run TestMemory -v`
预期：FAIL（`MemoryHandler`/`List`/`Put`/`Delete` 未定义）。

- [ ] **步骤 3：编写实现**

创建 `internal/server/handler_memory.go`：

```go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/agent-rust/core/internal/memory"
	"github.com/go-chi/chi/v5"
)

// MemoryHandler 暴露记忆库的 CRUD 给前端 UI（agent 写记忆走工具，不经此处）。
type MemoryHandler struct {
	Store *memory.MemoryStore
}

func (h *MemoryHandler) Routes(r chi.Router) {
	r.Get("/memory", h.List)
	r.Get("/memory/{name}", h.Get)
	r.Put("/memory/{name}", h.Put)
	r.Delete("/memory/{name}", h.Delete)
}

func (h *MemoryHandler) List(w http.ResponseWriter, r *http.Request) {
	es, err := h.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": es})
}

func (h *MemoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	e, err := h.Store.Get(name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

type memoryPutBody struct {
	Description string         `json:"description"`
	Type        memory.Type    `json:"type"`
	Body        string         `json:"body"`
}

func (h *MemoryHandler) Put(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body memoryPutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body: "+err.Error())
		return
	}
	e := memory.Entry{Name: name, Description: body.Description, Type: body.Type, Body: body.Body}
	if err := h.Store.Save(e); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name})
}

func (h *MemoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.Store.Delete(name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name})
}
```

修改 `internal/server/router.go`：

1. `Deps` 结构体增加字段（紧跟 `WorkDir *tools.WorkDir` 之后）：
```go
	Memory *memory.MemoryStore // optional; nil disables memory feature
```
2. 在 import 块增加 `"github.com/agent-rust/core/internal/memory"`。
3. 在 `r.Route("/api", ...)` 内追加注册（`MCPHandler` 之后）：
```go
		if d.Memory != nil {
			(&MemoryHandler{Store: d.Memory}).Routes(r)
		}
```
4. `ChatHandler` 的构造调用（`router.go:56`）增加 `Memory` 传参——但 `ChatHandler.Memory` 字段在任务 7 的 handler_chat.go 修改中添加；此处先把 server.Deps.Memory 经 router 内部传给 ChatHandler。需在 `NewRouter` 内构造 ChatHandler 时加 `Memory: <provider>`。由于 `*memory.MemoryStore` 同时实现 `agent.MemoryProvider`，直接传 `d.Memory`。

修改 `router.go:56` 的 ChatHandler 构造为：
```go
		(&ChatHandler{DB: d.DB, Gate: d.Gate, Engine: d.Engine, MCP: d.MCP, RAG: d.RAG, Skills: skillProvider, MCPConfigPath: mcpConfigPath, WorkDir: d.WorkDir, Memory: d.Memory}).Routes(r)
```

> 注意：`writeErr`/`writeJSON` 已是 server 包既有 helper（见 router.go:50、handler_chat.go:80），无需新增。

- [ ] **步骤 4：运行测试验证通过**

运行：`unset GOROOT && go test ./internal/server/ -run TestMemory -v`
预期：PASS。

- [ ] **步骤 5：Commit**

```bash
git add internal/server/handler_memory.go internal/server/handler_memory_test.go internal/server/router.go
git commit -m "feat(server): /api/memory CRUD 接口与路由注册"
```

---

## 任务 7：ChatHandler 接 Memory + main.go 装配

**文件：**
- 修改：`internal/server/handler_chat.go`
- 修改：`main.go`

- [ ] **步骤 1：修改 ChatHandler 结构与 agent 构造**

在 `internal/server/handler_chat.go` 的 `ChatHandler` 结构体（`handler_chat.go:30-39`）中，`WorkDir` 字段后增加：

```go
	Memory agent.MemoryProvider // optional; nil disables memory injection
```

在 `handler_chat.go:164-167` 的 `agent.New(agent.Deps{...})` 调用中增加 `Memory` 字段：

```go
	a := agent.New(agent.Deps{
		LLM: llmClient, Tools: chatEngine, RAG: h.RAG, Skills: h.Skills,
		Memory:      h.Memory,
		MaxToolCalls: toolLimitSetting(h.DB),
	})
```

> 说明：本任务无独立单测——Memory 注入逻辑已在任务 5 覆盖，此处仅是装配连线。验证靠 `go build` + 任务 10 端到端。

- [ ] **步骤 2：修改 main.go 装配**

在 `main.go` 增加 memory 包 import：
```go
	"github.com/agent-rust/core/internal/memory"
```

在 `main.go:53`（`workDir := tools.NewWorkDir()`）之后构造 memory store：
```go
	memStore := memory.New(workDir.Get, *dataDir)
```

将 memory 工具加入 registry。修改 `main.go:54-57` 的 `NewRegistry` 调用，在 builtin 工具后追加 memory 工具：
```go
	allTools := []tools.Tool{
		builtin.FileRead{}, builtin.FileWrite{}, builtin.FileEdit{},
		builtin.Grep{}, builtin.Bash{WorkDir: workDir},
	}
	allTools = append(allTools, memory.Tools(memStore)...)
	registry := tools.NewRegistry(allTools...)
```

在 `server.NewRouter(server.Deps{...})`（`main.go:75-79`）增加 `Memory: memStore`：
```go
	router := server.NewRouter(server.Deps{
		DB: db, Gate: gate, Engine: baseEngine,
		EmbedClient: embedClient, RAG: ragRetriever, Skills: skillsManager, MCP: mcpManager, WorkDir: workDir,
		Memory:    memStore,
		UploadDir: filepath.Join(*dataDir, "uploads"),
	})
```

- [ ] **步骤 3：编译验证**

运行：`unset GOROOT && go build ./...`
预期：成功，无报错。

- [ ] **步骤 4：运行全部 Go 测试**

运行：`unset GOROOT && go test ./...`
预期：全部 PASS（含新 memory/agent/server 测试，原有测试不回归）。

- [ ] **步骤 5：Commit**

```bash
git add internal/server/handler_chat.go main.go
git commit -m "feat: ChatHandler 接入 MemoryProvider 与 main 装配 memory store/工具"
```

---

## 任务 8：前端 — types + api + memoryStore

**文件：**
- 修改：`frontend/src/types.ts`
- 修改：`frontend/src/lib/api.ts`
- 创建：`frontend/src/stores/memoryStore.ts`

- [ ] **步骤 1：新增类型**

在 `frontend/src/types.ts` 末尾追加：

```ts
// 跨会话记忆条目（镜像 internal/memory Entry 的 JSON）。
export type MemoryType = 'user' | 'feedback' | 'project' | 'reference';

export interface MemoryEntry {
  name: string;
  description: string;
  type: MemoryType;
  body: string;
  updated_at: string;
}
```

- [ ] **步骤 2：新增 api 端点**

在 `frontend/src/lib/api.ts` 顶部的 `import type { ... } from '../types'` 中加入 `MemoryEntry`：

```ts
import type {
  Provider,
  Session,
  Message,
  KnowledgeBase,
  Document,
  ChunkPreview,
  RetrieveHit,
  Skill,
  MCPServer,
  TreeItem,
  MemoryEntry,
} from '../types';
```

在 `api` 对象中（`listTree` 之后）追加：

```ts
  // --- memory（跨会话记忆） ---
  listMemory: () => jget<{ entries: MemoryEntry[] }>('/api/memory'),
  getMemory: (name: string) => jget<MemoryEntry>(`/api/memory/${encodeURIComponent(name)}`),
  saveMemory: (name: string, body: { description: string; type: string; body: string }) =>
    jput<{ name: string }>(`/api/memory/${encodeURIComponent(name)}`, body),
  deleteMemory: (name: string) => jdel(`/api/memory/${encodeURIComponent(name)}`),
```

- [ ] **步骤 3：创建 memoryStore**

创建 `frontend/src/stores/memoryStore.ts`：

```ts
import { create } from 'zustand';
import { api } from '../lib/api';
import type { MemoryEntry, MemoryType } from '../types';

interface MemoryState {
  entries: MemoryEntry[];
  loaded: boolean;
  load: () => Promise<void>;
  save: (
    name: string,
    body: { description: string; type: MemoryType; body: string },
  ) => Promise<void>;
  remove: (name: string) => Promise<void>;
}

export const useMemoryStore = create<MemoryState>((set, get) => ({
  entries: [],
  loaded: false,
  load: async () => {
    const { entries } = await api.listMemory();
    set({ entries, loaded: true });
  },
  save: async (name, body) => {
    await api.saveMemory(name, body);
    await get().load();
  },
  remove: async (name) => {
    await api.deleteMemory(name);
    await get().load();
  },
}));
```

- [ ] **步骤 4：类型检查**

运行：`cd frontend && npx tsc --noEmit`
预期：无类型错误。

- [ ] **步骤 5：Commit**

```bash
git add frontend/src/types.ts frontend/src/lib/api.ts frontend/src/stores/memoryStore.ts
git commit -m "feat(frontend): memory 类型、api 端点与 memoryStore"
```

---

## 任务 9：前端 — Icon + MemoryPanel + SettingsModal tab

**文件：**
- 修改：`frontend/src/components/Icon.tsx`
- 创建：`frontend/src/components/MemoryPanel.tsx`
- 修改：`frontend/src/components/SettingsModal.tsx`

- [ ] **步骤 1：新增 brain 图标**

在 `frontend/src/components/Icon.tsx` 的 `IconName` union（`'square';` 之前）追加：

```ts
  | 'brain'
```

在 `PATHS` 对象中（`square: ...` 之前）追加（Lucide brain 简化路径）：

```ts
  brain: (
    <path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z" />
  ),
```

并在 IconName 列表实际位置加入（保持与 PATHS 一致）。

- [ ] **步骤 2：创建 MemoryPanel**

创建 `frontend/src/components/MemoryPanel.tsx`：

```tsx
import { useEffect, useMemo, useState } from 'react';
import { Icon } from './Icon';
import { useMemoryStore } from '../stores/memoryStore';
import type { MemoryEntry, MemoryType } from '../types';

const TYPE_LABEL: Record<MemoryType, string> = {
  user: '用户偏好',
  feedback: '工作指导',
  project: '项目约束',
  reference: '外部资源',
};

const TYPE_ORDER: MemoryType[] = ['user', 'feedback', 'project', 'reference'];

type Draft = { name: string; description: string; type: MemoryType; body: string };

const emptyDraft = (): Draft => ({ name: '', description: '', type: 'user', body: '' });

function toDraft(e: MemoryEntry): Draft {
  return { name: e.name, description: e.description, type: e.type, body: e.body };
}

export function MemoryPanel() {
  const { entries, loaded, load, save, remove } = useMemoryStore();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    load();
  }, [load]);

  const grouped = useMemo(() => {
    const m: Record<MemoryType, MemoryEntry[]> = { user: [], feedback: [], project: [], reference: [] };
    for (const e of entries) if (m[e.type]) m[e.type].push(e);
    return m;
  }, [entries]);

  const select = (e: MemoryEntry) => {
    setDraft(toDraft(e));
    setIsNew(false);
    setError('');
  };

  const startNew = () => {
    setDraft(emptyDraft());
    setIsNew(true);
    setError('');
  };

  const submit = async () => {
    if (!draft) return;
    setSaving(true);
    setError('');
    try {
      await save(draft.name, { description: draft.description, type: draft.type, body: draft.body });
      setDraft(null);
      setIsNew(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const del = async (name: string) => {
    if (!confirm(`删除记忆「${name}」？`)) return;
    await remove(name);
    if (draft?.name === name) setDraft(null);
  };

  if (!loaded) {
    return <div className="p-4 text-sm text-muted-foreground">加载中…</div>;
  }

  return (
    <div className="flex h-full min-h-0">
      {/* 左：按 type 分组的列表 */}
      <div className="flex w-56 shrink-0 flex-col overflow-y-auto border-r border-border pr-1">
        <button
          className="btn btn-primary mb-2 flex items-center justify-center gap-1.5 py-1.5 text-sm"
          onClick={startNew}
        >
          <Icon name="plus" size={14} /> 新建记忆
        </button>
        {entries.length === 0 && (
          <p className="px-1 py-2 text-xs text-muted-foreground">还没有记忆。Agent 会在对话中自动记录。</p>
        )}
        {TYPE_ORDER.map((t) =>
          grouped[t].length === 0 ? null : (
            <div key={t} className="mb-2">
              <div className="px-1 py-1 text-xs font-medium text-muted-foreground">{TYPE_LABEL[t]}</div>
              {grouped[t].map((e) => (
                <button
                  key={e.name}
                  className={
                    'group flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors ' +
                    (draft?.name === e.name && !isNew
                      ? 'bg-card font-medium text-foreground shadow-sm'
                      : 'text-foreground/80 hover:bg-muted')
                  }
                  onClick={() => select(e)}
                >
                  <span className="flex-1 truncate">{e.description || e.name}</span>
                  <span
                    className="opacity-0 transition-opacity group-hover:opacity-100"
                    onClick={(ev) => {
                      ev.stopPropagation();
                      del(e.name);
                    }}
                  >
                    <Icon name="trash" size={14} className="text-muted-foreground hover:text-destructive" />
                  </span>
                </button>
              ))}
            </div>
          ),
        )}
      </div>

      {/* 右：编辑区 */}
      <div className="flex min-w-0 flex-1 flex-col overflow-y-auto pl-4">
        {!draft ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            选择左侧条目编辑，或点「新建记忆」。
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {error && (
              <div className="flex items-center gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                <Icon name="alert-circle" size={14} /> {error}
              </div>
            )}
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">名称（kebab-case，唯一）</span>
              <input
                className="field"
                value={draft.name}
                disabled={!isNew}
                placeholder="如 go-env"
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">摘要（一句话，召回相关性依据）</span>
              <input
                className="field"
                value={draft.description}
                onChange={(e) => setDraft({ ...draft, description: e.target.value })}
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">类型</span>
              <select
                className="field"
                value={draft.type}
                onChange={(e) => setDraft({ ...draft, type: e.target.value as MemoryType })}
              >
                {TYPE_ORDER.map((t) => (
                  <option key={t} value={t}>
                    {TYPE_LABEL[t]}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">
                正文（markdown；feedback/project 类末尾带 **Why:** / **How to apply:**）
              </span>
              <textarea
                className="field min-h-[160px] resize-y font-mono text-xs"
                value={draft.body}
                onChange={(e) => setDraft({ ...draft, body: e.target.value })}
              />
            </label>
            <div className="flex gap-2">
              <button className="btn btn-primary py-1.5 text-sm" onClick={submit} disabled={saving}>
                {saving ? '保存中…' : '保存'}
              </button>
              <button className="btn btn-ghost py-1.5 text-sm" onClick={() => setDraft(null)}>
                取消
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **步骤 3：SettingsModal 加 Memory tab**

修改 `frontend/src/components/SettingsModal.tsx`：

1. 顶部 import 增加：
```ts
import { MemoryPanel } from './MemoryPanel';
```

2. `TabKey` 类型改为：
```ts
type TabKey = 'model' | 'mcp' | 'skills' | 'memory';
```

3. `TABS` 数组增加一项（`skills` 之后）：
```ts
  { key: 'memory', label: '记忆', desc: '跨会话的事实记忆', icon: 'brain' },
```

4. 右侧内容区条件渲染（`{tab === 'skills' && <SkillsSettings />}` 之后）增加：
```tsx
            {tab === 'memory' && <MemoryPanel />}
```

- [ ] **步骤 4：类型检查 + 构建**

运行：`cd frontend && npx tsc --noEmit && npm run build`
预期：无类型错误，构建成功。

- [ ] **步骤 5：Commit**

```bash
git add frontend/src/components/Icon.tsx frontend/src/components/MemoryPanel.tsx frontend/src/components/SettingsModal.tsx
git commit -m "feat(frontend): Memory 管理面板与 SettingsModal 记忆 tab"
```

---

## 任务 10：端到端验证

**文件：** 无（手动 + 构建验证）

- [ ] **步骤 1：完整后端构建与测试**

```bash
unset GOROOT && go build ./... && go test ./...
```
预期：构建成功，全部测试 PASS。

- [ ] **步骤 2：启动应用**

```bash
make dev
```
预期：wails dev 正常启动，前端编译无错，应用窗口打开。

- [ ] **步骤 3：手动验证 UI 管理**

在应用中：打开「设置 → 记忆」tab → 点「新建记忆」→ 填 `name=test-ui`、`description=UI 测试`、`type=user`、`body=测试正文` → 保存。
- 验证：列表出现该条；文件系统 `<workdir>/.agentforge/memory/test-ui.md` 存在且 frontmatter 正确；`MEMORY.md` 含该条索引。
- 编辑该条 description → 保存 → 列表刷新、文件更新。
- 删除该条 → 列表移除、文件删除、`MEMORY.md` 同步。

- [ ] **步骤 4：手动验证 agent 注入与自动写入**

新开一个对话，确认已配置 provider 与 workdir。提问一个与既有记忆相关的简单问题（例如先手动建一条 `user` 类偏好记忆，再问相关内容）。
- 验证日志：后端日志应显示 agent 注入了记忆索引（可在 `agent.go` 临时加 log 或观察行为）。
- 让 agent「记住我喜欢简洁的回复」→ 观察 agent 调用 `memory_save` → 文件系统出现新 `.md` → 刷新「设置 → 记忆」可见。
- 让 agent「读一下 test 记忆」→ agent 调用 `memory_read` 返回内容。

- [ ] **步骤 5：Commit 验证记录（可选）**

若验证中发现并修复了小问题，按修复内容 commit。无问题则本任务无需 commit。

---

## 自检（writing-plans 规定）

**1. 规格覆盖度**：对照规格 §1–§13 逐条核对——
- §4 文件布局/数据模型 → 任务 1（类型/frontmatter）、任务 2（Save/List）、任务 3（Reindex/MEMORY.md）✅
- §5 三条数据流 → 读取注入（任务 5）、写入工具（任务 4）、UI 管理（任务 6/8/9）✅
- §6 后端 → memory 包（1-4）、agent 集成（5）、API（6）、装配（7）✅
- §7 前端 → 任务 8/9 ✅
- §8 注入格式 → 任务 3（IndexContext 文本含 memory_read/memory_save 提示）✅
- §9 错误处理 → 任务 2（name/type/超限校验）、任务 4（工具 IsError）、任务 6（API 400）✅
- §10 安全 → 任务 2（ValidName 防穿越、ResolveDir）✅
- §11 默认值 → 任务 1 常量（MaxBodyBytes=8KB 等）✅
- §12 测试 → 每任务 TDD + 任务 10 端到端 ✅
- §13 风险 → name 去重（任务 4 工具说明「同名覆盖」）✅

**2. 占位符扫描**：无 TODO/待定；每步含完整代码或精确命令。任务 5 测试的 `llm.Chunk{Text, Done}`、`llm.Message{Role, Content}`、`fakeStreamLLM`（已补全 `Chat`/`Embed` 满足 `llm.LLMClient` 接口）字段/签名已对照 `internal/llm/client.go` 确认一致；任务 2 `sortByMtimeDesc` 与 store.go 调用同名。

**3. 类型一致性**：`Entry`（Name/Description/Type/Body/UpdatedAt）贯穿任务 1-9 一致；`MemoryStore` 方法名（List/Get/Save/Delete/Reindex/IndexContext）跨任务一致；`memory.Provider.IndexContext()` 在 agent（任务 5）与 memory（任务 3）一致；前端 `MemoryEntry`/`MemoryType` 在 types/api/store/panel 一致。
