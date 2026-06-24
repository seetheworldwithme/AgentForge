package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// indexLines 生成索引行，供 MEMORY.md 落盘（withLink）与 IndexContext（纯文本）共用。
// 列表已按 mtime 倒序。
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

// Reindex 扫描所有条目，重写 MEMORY.md（按 mtime 倒序）。无条目则删除索引文件。
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
