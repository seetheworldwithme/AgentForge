package rules

import (
	"os"
	"path/filepath"
	"strings"
)

// injectHeader 规则注入的外层说明。
const injectHeader = "以下是当前生效的项目规则（全局 + 项目 + 兼容导入），请在本次会话全程遵守："

// RulesContext 返回注入 agent 的规则文本（运行时拼装，不落盘）。无生效规则返回空串。
// 顺序：全局 AGENTFORGE.md → 项目 AGENTFORGE.md → CLAUDE.md（开关）→ AGENTS.md（开关）。
// AGENTFORGE.md（全局+项目）始终读取；CLAUDE.md/AGENTS.md 仅在对应导入开关启用时读取。
func (s *RulesStore) RulesContext() string {
	var sections []string

	// 1. 全局规则（始终）
	if p, err := s.ResolvePath(ScopeGlobal); err == nil {
		if c := readTrim(p); c != "" {
			sections = append(sections, section("全局规则", c))
		}
	}
	// 2. 项目规则（始终；workdir 空则跳过）
	if p, err := s.ResolvePath(ScopeProject); err == nil {
		if c := readTrim(p); c != "" {
			sections = append(sections, section("项目规则", c))
		}
	}
	// 3. 兼容导入：CLAUDE.md（开关启用且 workdir 非空）
	if s.importEnabled(SettingImportClaude) {
		if c := s.readImport(ClaudeFile); c != "" {
			sections = append(sections, section("兼容导入：CLAUDE.md", c))
		}
	}
	// 4. 兼容导入：AGENTS.md（开关启用且 workdir 非空）
	if s.importEnabled(SettingImportAgents) {
		if c := s.readImport(AgentsFile); c != "" {
			sections = append(sections, section("兼容导入：AGENTS.md", c))
		}
	}

	if len(sections) == 0 {
		return ""
	}
	return injectHeader + "\n\n" + strings.Join(sections, "\n\n")
}

// readImport 读取 workdir 根下的兼容导入文件；workdir 为空或读取失败返回空串。
func (s *RulesStore) readImport(name string) string {
	wd := s.workdir()
	if wd == "" {
		return ""
	}
	return readTrim(filepath.Join(wd, name))
}

// readTrim 读取文件并 TrimSpace；失败或空白返回空串。
func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// section 拼一个二级分区："## 标题\n\n内容"。
func section(title, content string) string {
	return "## " + title + "\n\n" + content
}
