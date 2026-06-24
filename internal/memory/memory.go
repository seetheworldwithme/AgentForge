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
