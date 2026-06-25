package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/tools"
)

// stubSkills 是 SkillContentProvider 的测试替身：按 id 返回预设全文，未命中报错。
type stubSkills map[string]string

func (s stubSkills) GetSkillContent(id string) (string, error) {
	if v, ok := s[id]; ok {
		return v, nil
	}
	return "", errors.New("skill not found: " + id)
}

func TestReadSkillLoadsContent(t *testing.T) {
	rs := ReadSkill{Skills: stubSkills{"global:docx": "# docx\ncreate word docs"}}
	r, err := rs.Run(context.Background(), `{"skill_id":"global:docx"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError {
		t.Fatalf("unexpected error: %s", r.Content)
	}
	if !strings.Contains(r.Content, "create word docs") {
		t.Errorf("content=%q", r.Content)
	}
}

func TestReadSkillUnknownIdErrors(t *testing.T) {
	rs := ReadSkill{Skills: stubSkills{}}
	r, _ := rs.Run(context.Background(), `{"skill_id":"global:nope"}`, autoAllowGate())
	if !r.IsError {
		t.Errorf("expected error for unknown id, got=%q", r.Content)
	}
}

func TestReadSkillNilProviderErrors(t *testing.T) {
	r, _ := (ReadSkill{}).Run(context.Background(), `{"skill_id":"x"}`, autoAllowGate())
	if !r.IsError {
		t.Errorf("expected error when Skills is nil, got=%q", r.Content)
	}
	_ = tools.Result{} // keep tools import referenced via package usage above
}
