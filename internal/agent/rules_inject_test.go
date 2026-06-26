package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/llm"
)

// staticRules 实现 RulesProvider，返回固定规则文本。
type staticRules string

func (s staticRules) RulesContext() string { return string(s) }

// TestRunInjectsRules 验证 Deps.Rules 非空时，规则文本被注入到首条 system 消息。
func TestRunInjectsRules(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "done"}, {Done: true}},
	}}
	a := New(Deps{
		LLM:   m,
		Rules: staticRules("【项目规则占位】使用中文注释"),
	})
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:    &recorderEmitter{},
	})

	if len(m.lastMessages) == 0 {
		t.Fatal("no messages captured")
	}
	sys := m.lastMessages[0]
	if sys.Role != llm.RoleSystem {
		t.Fatalf("first msg role = %v, want system", sys.Role)
	}
	if !strings.Contains(sys.Content, "【项目规则占位】使用中文注释") {
		t.Errorf("rules not injected into system message: %q", sys.Content)
	}
}

// TestRunSkipsInjectionWhenRulesNil 验证 Rules 为 nil 时不注入规则文本。
func TestRunSkipsInjectionWhenRulesNil(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "done"}, {Done: true}},
	}}
	a := New(Deps{LLM: m}) // Rules 留空
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:    &recorderEmitter{},
	})

	if len(m.lastMessages) == 0 {
		t.Fatal("no messages captured")
	}
	if strings.Contains(m.lastMessages[0].Content, "【项目规则占位】") {
		t.Errorf("should not inject rules when Rules is nil: %q", m.lastMessages[0].Content)
	}
}
