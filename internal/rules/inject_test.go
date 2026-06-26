package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/store"
)

// newStore 构造一个指向临时 home + workdir 的 RulesStore（db 可为 nil）。
func newStore(t *testing.T, db *store.DB) (*RulesStore, string, string) {
	t.Helper()
	home := t.TempDir()
	wd := t.TempDir()
	return New(Options{WorkDir: func() string { return wd }, HomeDir: home, DB: db}), home, wd
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 无任何规则文件时返回空串（agent 不注入）。
func TestRulesContextEmpty(t *testing.T) {
	s, _, _ := newStore(t, nil)
	if got := s.RulesContext(); got != "" {
		t.Fatalf("无规则时应返回空串，got=%q", got)
	}
}

// 仅全局 AGENTFORGE.md → 含全局分区与正文。
func TestRulesContextGlobalOnly(t *testing.T) {
	s, home, _ := newStore(t, nil)
	writeFile(t, filepath.Join(home, GlobalDirName, RulesFile), "用中文回答。")
	got := s.RulesContext()
	if !strings.Contains(got, "全局规则") || !strings.Contains(got, "用中文回答。") {
		t.Fatalf("应含全局规则分区，got=%q", got)
	}
}

// 仅项目 AGENTFORGE.md → 含项目分区。
func TestRulesContextProjectOnly(t *testing.T) {
	s, _, wd := newStore(t, nil)
	writeFile(t, filepath.Join(wd, RulesFile), "本项目用 gofmt -s。")
	got := s.RulesContext()
	if !strings.Contains(got, "## 项目规则") || !strings.Contains(got, "gofmt") {
		t.Fatalf("应含## 项目规则分区，got=%q", got)
	}
}

// 全局+项目同时存在时，全局分区排在项目分区之前。
func TestRulesContextOrder(t *testing.T) {
	s, home, wd := newStore(t, nil)
	writeFile(t, filepath.Join(home, GlobalDirName, RulesFile), "GLOBAL_BODY")
	writeFile(t, filepath.Join(wd, RulesFile), "PROJECT_BODY")
	got := s.RulesContext()
	gi := strings.Index(got, "GLOBAL_BODY")
	pi := strings.Index(got, "PROJECT_BODY")
	if gi < 0 || pi < 0 || gi > pi {
		t.Fatalf("全局应在项目之前，got gi=%d pi=%d", gi, pi)
	}
}

// 开关默认关：workdir 有 CLAUDE.md 但 db=nil（开关全关）→ 不应导入。
func TestRulesContextImportOffByDefault(t *testing.T) {
	s, _, wd := newStore(t, nil)
	writeFile(t, filepath.Join(wd, ClaudeFile), "CLAUDE_BODY")
	if got := s.RulesContext(); strings.Contains(got, "CLAUDE_BODY") {
		t.Fatalf("开关关时不应导入 CLAUDE.md，got=%q", got)
	}
}

// 启用 rules.import.claude 后应导入 CLAUDE.md 分区。
func TestRulesContextImportClaudeOn(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(SettingImportClaude, "1"); err != nil {
		t.Fatal(err)
	}
	s, _, wd := newStore(t, db)
	writeFile(t, filepath.Join(wd, ClaudeFile), "CLAUDE_BODY")
	got := s.RulesContext()
	if !strings.Contains(got, "兼容导入：CLAUDE.md") || !strings.Contains(got, "CLAUDE_BODY") {
		t.Fatalf("开关开时应导入 CLAUDE.md，got=%q", got)
	}
}

// 启用 rules.import.agents 后应导入 AGENTS.md 分区。
func TestRulesContextImportAgentsOn(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(SettingImportAgents, "1"); err != nil {
		t.Fatal(err)
	}
	s, _, wd := newStore(t, db)
	writeFile(t, filepath.Join(wd, AgentsFile), "AGENTS_BODY")
	got := s.RulesContext()
	if !strings.Contains(got, "兼容导入：AGENTS.md") || !strings.Contains(got, "AGENTS_BODY") {
		t.Fatalf("开关开时应导入 AGENTS.md，got=%q", got)
	}
}

// workdir 为空时项目部分被跳过，但全局仍注入。
func TestRulesContextNoWorkDir(t *testing.T) {
	home := t.TempDir()
	s := New(Options{WorkDir: func() string { return "" }, HomeDir: home})
	writeFile(t, filepath.Join(home, GlobalDirName, RulesFile), "GLOBAL_BODY")
	got := s.RulesContext()
	if !strings.Contains(got, "全局规则") {
		t.Fatalf("workdir 空也应注入全局，got=%q", got)
	}
	if strings.Contains(got, "## 项目规则") {
		t.Fatalf("workdir 空不应出现项目分区，got=%q", got)
	}
}
