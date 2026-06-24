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

// pathOf 返回记忆目录下某条目的 .md 路径。
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
			return []Entry{}, nil
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
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, fe.Name()))
		// 要求合法 UTF-8 且含 frontmatter：无 frontmatter 的文件视为垃圾，跳过。
		if err != nil || !utf8.Valid(raw) || !hasFrontmatter(string(raw)) {
			continue
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
	defer os.Remove(tmpName)
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
