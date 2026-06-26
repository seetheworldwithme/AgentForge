package rules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// Get 读取规则文件内容；不存在返回 ("", ErrNotFound)。
func (s *RulesStore) Get(scope Scope) (string, error) {
	p, err := s.ResolvePath(scope)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	if !utf8.Valid(b) {
		return "", errors.New("规则文件不是合法 UTF-8")
	}
	return string(b), nil
}

// Save 校验并写入规则文件（原子写：临时文件 + rename），照搬 memory.Save。
func (s *RulesStore) Save(scope Scope, body string) error {
	if len(body) > MaxRuleBytes {
		return fmt.Errorf("规则内容超过 %d 字节", MaxRuleBytes)
	}
	if !utf8.ValidString(body) {
		return errors.New("规则内容不是合法 UTF-8")
	}
	p, err := s.ResolvePath(scope)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建规则目录: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

// Clear 删除规则文件；不存在返回 ErrNotFound。
func (s *RulesStore) Clear(scope Scope) error {
	p, err := s.ResolvePath(scope)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
