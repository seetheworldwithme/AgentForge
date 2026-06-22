package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/tools"
)

// fakeLLM scripts a sequence of streamed responses (one per turn).
type fakeLLM struct {
	scripts      [][]llm.Chunk
	errors       []error
	calls        int
	scriptCalls  int
	lastMessages []llm.Message
}

func (f *fakeLLM) Chat(ctx context.Context, msgs []llm.Message) (string, error) {
	return "", nil
}

func (f *fakeLLM) ChatStream(ctx context.Context, msgs []llm.Message, ts []llm.ToolSpec) (<-chan llm.Chunk, error) {
	f.lastMessages = append([]llm.Message(nil), msgs...)
	if f.calls < len(f.errors) && f.errors[f.calls] != nil {
		err := f.errors[f.calls]
		f.calls++
		return nil, err
	}
	script := f.scripts[f.scriptCalls%len(f.scripts)]
	f.scriptCalls++
	f.calls++
	ch := make(chan llm.Chunk, len(script))
	for _, c := range script {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (f *fakeLLM) Embed(ctx context.Context, in []string) ([][]float32, error) {
	return nil, nil
}

type staticSkills string

func (s staticSkills) EnabledInstructions() (string, error) { return string(s), nil }

// fakeToolEngine just echoes the tool name as the result.
type fakeToolEngine struct{}

func (fakeToolEngine) List() []tools.Spec { return nil }
func (fakeToolEngine) Execute(ctx context.Context, name, args string) (tools.Result, error) {
	return tools.Result{Content: fmt.Sprintf("ran %s with %s", name, args)}, nil
}

// recorderEmitter captures events for assertions.
type recorderEmitter struct {
	mu     sync.Mutex
	events []string
}

func (r *recorderEmitter) Emit(event string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func TestRunPlainTextThenDone(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "hello"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, MaxIter: 5})
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:    rec,
	})
	if len(rec.events) == 0 || rec.events[len(rec.events)-1] != "done" {
		t.Errorf("expected done event, got %v", rec.events)
	}
}

func TestRunInjectsEnabledSkillsIntoSystemPrompt(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "ok"}, {Done: true}},
	}}
	a := New(Deps{
		LLM:     m,
		Skills:  staticSkills("Use the frontend-design skill before changing UI."),
		MaxIter: 5,
	})

	a.Run(context.Background(), RunInput{
		History:     []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:        &recorderEmitter{},
		UserMessage: "change settings",
	})

	if len(m.lastMessages) == 0 {
		t.Fatal("no messages sent to LLM")
	}
	if m.lastMessages[0].Role != llm.RoleSystem {
		t.Fatalf("first message role = %q", m.lastMessages[0].Role)
	}
	if !contains(m.lastMessages[0].Content, "frontend-design") {
		t.Fatalf("system prompt missing skill instructions: %q", m.lastMessages[0].Content)
	}
}

func TestRunToolCallThenAnswer(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		// first turn: emit a tool call
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: `{"command":"ls"}`}}},
		// second turn: answer text
		{{Text: "done"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 5})
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "ls"}},
		Emit:    rec,
	})
	joined := join(rec.events)
	if !contains(joined, "tool_call") || !contains(joined, "tool_result") || !contains(joined, "done") {
		t.Errorf("event sequence unexpected: %v", rec.events)
	}
}

func TestRunFinalizesAfterToolResultAtMaxIter(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: `{"command":"excelcli query"}`}}},
		{{Text: "final answer from query result"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 1})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "analyze transactions"}},
		Emit:    rec,
	})

	if m.calls != 2 {
		t.Fatalf("LLM calls = %d, want 2 so tool results are returned to the model; events=%v", m.calls, rec.events)
	}
	joined := join(rec.events)
	if !contains(joined, "tool_call") || !contains(joined, "tool_result") || !contains(joined, "delta") || !contains(joined, "done") {
		t.Fatalf("expected tool call, tool result, final answer delta, and done; got %v", rec.events)
	}
}

func TestRunDoesNotStopOnBlankAnswerAfterToolResult(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: `{"command":"excelcli import"}`}}},
		{{Text: "\n\n"}, {Done: true}},
		{{Text: "analysis complete"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 5})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "analyze anti-money case"}},
		Emit:    rec,
	})

	if m.calls != 3 {
		t.Fatalf("LLM calls = %d, want 3; events=%v", m.calls, rec.events)
	}
	joined := join(rec.events)
	if !contains(joined, "tool_call") || !contains(joined, "tool_result") || !contains(joined, "done") {
		t.Errorf("event sequence unexpected: %v", rec.events)
	}
}

func TestRunRetriesRecoverableLLMErrorAfterToolResult(t *testing.T) {
	m := &fakeLLM{
		scripts: [][]llm.Chunk{
			{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: `{"command":"ls"}`}}},
			{{Text: "analysis complete"}, {Done: true}},
		},
		errors: []error{
			nil,
			errors.New(`after 3 retries: llm http 429: {"message":"TPM limit reached"}`),
		},
	}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 5, LLMRetryWait: time.Millisecond})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "analyze anti-money case"}},
		Emit:    rec,
	})

	if m.calls != 3 {
		t.Fatalf("LLM calls = %d, want 3; events=%v", m.calls, rec.events)
	}
	joined := join(rec.events)
	if !contains(joined, "status") || !contains(joined, "done") {
		t.Fatalf("expected status retry and final done, got %v", rec.events)
	}
}

func TestRunDoesNotTreatIncompleteTextStreamAsFinalAnswer(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "现在我将基于收集到的数据撰写反洗钱分析报告。"}},
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: `{"command":"write report"}`}}},
		{{Text: "report complete"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 5})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "write the final markdown report"}},
		Emit:    rec,
	})

	if m.calls != 3 {
		t.Fatalf("LLM calls = %d, want 3; events=%v", m.calls, rec.events)
	}
	joined := join(rec.events)
	if !contains(joined, "status") || !contains(joined, "tool_call") || !contains(joined, "done") {
		t.Fatalf("expected incomplete-stream status, tool call, and final done; got %v", rec.events)
	}
}

func TestRunRespectsMaxIter(t *testing.T) {
	// LLM always emits a tool call -> infinite loop unless capped
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c", Name: "bash", Args: "{}"}}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 3})
	done := make(chan struct{})
	go func() {
		a.Run(context.Background(), RunInput{
			History: []llm.Message{{Role: llm.RoleUser, Content: "x"}}, Emit: rec,
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not terminate within 2s (maxIter not respected)")
	}
	count := 0
	for _, e := range rec.events {
		if e == "tool_call" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("tool_call count=%d want 3", count)
	}
}

// --- helpers ---

func wrapEngine(e *fakeToolEngine) *tools.Engine {
	return tools.NewEngineFromFunc(e.List, e.Execute)
}

func join(xs []string) string {
	out := ""
	for _, x := range xs {
		out += x + ","
	}
	return out
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
