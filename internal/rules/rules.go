// Package rules 提供项目规则（AGENTFORGE.md）管理：全局（~/.agentforge）+ 项目
// （当前工作目录根）两层单文件，另支持兼容导入 CLAUDE.md / AGENTS.md（由导入开关控制）。
// RulesContext() 把生效规则拼成文本供 agent 注入上下文，整体照搬 memory 的 Provider 模式。
package rules

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/agent-rust/core/internal/store"
)

// Scope 规则来源。
type Scope string

const (
	ScopeGlobal  Scope = "global"  // ~/.agentforge/AGENTFORGE.md
	ScopeProject Scope = "project" // <workdir>/AGENTFORGE.md
)

const (
	GlobalDirName = ".agentforge"    // 全局根目录名（与 skills 全局根一致）
	RulesFile     = "AGENTFORGE.md"  // 核心规则文件
	ClaudeFile    = "CLAUDE.md"      // 兼容导入：Claude Code 规则
	AgentsFile    = "AGENTS.md"      // 兼容导入：通用 agent 规则
	MaxRuleBytes  = 64 << 10         // 单文件上限 64KB
	// 导入开关的 settings key（值为 "1" 视为启用）。
	SettingImportClaude = "rules.import.claude"
	SettingImportAgents = "rules.import.agents"
)

// ErrNotFound 规则文件不存在（Get/Clear 用）。
var ErrNotFound = errors.New("rules file not found")

// Provider 给 agent 注入规则所需的最小依赖面：仅返回生效规则的拼接文本。
type Provider interface {
	RulesContext() string
}

// Store 完整读写能力（server/HTTP 用）。
type Store interface {
	Provider
	Get(scope Scope) (string, error)
	Save(scope Scope, body string) error
	Clear(scope Scope) error
}

// Options 构造 RulesStore 的参数。WorkDir 为空时项目 scope 不可用；
// HomeDir 为空时构造时用 os.UserHomeDir() 兜底；DB 为 nil 时导入开关视为全关。
type Options struct {
	WorkDir func() string
	HomeDir string
	DB      *store.DB
}

// RulesStore 基于全局根 + workdir 的单文件实现。
type RulesStore struct {
	workdir func() string
	homeDir string
	db      *store.DB
}

// New 构造 RulesStore：兜底 homeDir、惰性创建全局根目录 ~/.agentforge。
func New(opts Options) *RulesStore {
	if opts.HomeDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			opts.HomeDir = home
		}
	}
	if opts.HomeDir != "" {
		_ = os.MkdirAll(filepath.Join(opts.HomeDir, GlobalDirName), 0o755)
	}
	return &RulesStore{workdir: opts.WorkDir, homeDir: opts.HomeDir, db: opts.DB}
}

// ResolvePath 返回某 scope 的 AGENTFORGE.md 绝对路径。
// project scope 在 workdir 为空时返回错误。
func (s *RulesStore) ResolvePath(scope Scope) (string, error) {
	switch scope {
	case ScopeGlobal:
		if s.homeDir == "" {
			return "", errors.New("全局规则目录不可用（无法解析家目录）")
		}
		return filepath.Join(s.homeDir, GlobalDirName, RulesFile), nil
	case ScopeProject:
		wd := s.workdir()
		if wd == "" {
			return "", errors.New("workdir 未设置，项目规则不可用")
		}
		return filepath.Join(wd, RulesFile), nil
	}
	return "", errors.New("非法 scope")
}

// importEnabled 读取某导入开关是否启用。db 为 nil 时视为关闭。
func (s *RulesStore) importEnabled(key string) bool {
	if s.db == nil {
		return false
	}
	v, _ := s.db.GetSetting(key)
	return v == "1"
}
