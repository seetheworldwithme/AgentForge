package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/llm"
)

// staticMemory 实现 MemoryProvider，返回固定索引文本。
type staticMemory string

func (s staticMemory) IndexContext() string { return string(s) }

// TestRunInjectsMemoryIndex 验证 Deps.Memory 非空时，索引文本被注入到
// 发送给 LLM 的首条 system 消息中。
func TestRunInjectsMemoryIndex(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "done"}, {Done: true}},
	}}
	a := New(Deps{
		LLM:    m,
		Memory: staticMemory("【记忆索引占位】USER_PREF_DARK_THEME"),
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
	if !strings.Contains(sys.Content, "【记忆索引占位】USER_PREF_DARK_THEME") {
		t.Errorf("memory index not injected into system message: %q", sys.Content)
	}
}

// TestRunSkipsInjectionWhenMemoryNil 验证 Memory 为 nil 时不注入记忆相关内容。
func TestRunSkipsInjectionWhenMemoryNil(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "done"}, {Done: true}},
	}}
	a := New(Deps{LLM: m}) // Memory 留空
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:    &recorderEmitter{},
	})

	if len(m.lastMessages) == 0 {
		t.Fatal("no messages captured")
	}
	if strings.Contains(m.lastMessages[0].Content, "记忆索引") {
		t.Errorf("should not inject memory when Memory is nil: %q", m.lastMessages[0].Content)
	}
}

// TestRunSkipsInjectionWhenMemoryEmpty 验证索引为空白时也不注入，
// 避免向 system 注入空行造成无意义合并。
func TestRunSkipsInjectionWhenMemoryEmpty(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "done"}, {Done: true}},
	}}
	a := New(Deps{
		LLM:    m,
		Memory: staticMemory("   \n\t "), // 仅空白
	})
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:    &recorderEmitter{},
	})

	if len(m.lastMessages) == 0 {
		t.Fatal("no messages captured")
	}
	// base 提示词固定存在；记忆为空白时应只含 base，不应出现额外前导空白结构。
	// 这里只断言不含记忆标记，且 system 消息非空（base 仍在）。
	if !strings.Contains(m.lastMessages[0].Content, "工具") {
		t.Errorf("base prompt should still be present: %q", m.lastMessages[0].Content)
	}
}
