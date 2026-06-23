package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

func (s staticSkills) InstructionsFor(ids []string) (string, error) { return string(s), nil }

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
	data   []map[string]any // parallel to events; nil when payload isn't a map
}

func (r *recorderEmitter) Emit(event string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	if d, ok := data.(map[string]any); ok {
		r.data = append(r.data, d)
	} else {
		r.data = append(r.data, nil)
	}
}

// dataFor returns the payload of the i-th occurrence of event, or nil.
func (r *recorderEmitter) dataFor(event string, occurrence int) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := 0
	for i, e := range r.events {
		if e != event {
			continue
		}
		seen++
		if seen == occurrence {
			return r.data[i]
		}
	}
	return nil
}

// hasStatusKind reports whether any "status" event carries the given kind.
func (r *recorderEmitter) hasStatusKind(kind string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.events {
		if e == "status" && r.data[i] != nil && r.data[i]["kind"] == kind {
			return true
		}
	}
	return false
}

type cancelAfterToolResultsEmitter struct {
	rec    recorderEmitter
	limit  int
	cancel context.CancelFunc
	count  int
}

func (r *cancelAfterToolResultsEmitter) Emit(event string, data any) {
	r.rec.Emit(event, data)
	if event != "tool_result" {
		return
	}
	r.count++
	if r.count >= r.limit {
		r.cancel()
	}
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

func TestRunContinuesToolCallsPastMaxIterCheckpoint(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: `{"command":"excelcli query step 1"}`}}},
		{{ToolCall: &llm.ToolCall{ID: "c2", Name: "bash", Args: `{"command":"excelcli query step 2"}`}}},
		{{Text: "final answer after both queries"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 1})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "analyze transactions"}},
		Emit:    rec,
	})

	if m.calls != 3 {
		t.Fatalf("LLM calls = %d, want 3 so tool use can continue past the checkpoint; events=%v", m.calls, rec.events)
	}
	toolCalls := 0
	for _, e := range rec.events {
		if e == "tool_call" {
			toolCalls++
		}
	}
	if toolCalls != 2 {
		t.Fatalf("tool_call count=%d, want 2; events=%v", toolCalls, rec.events)
	}
	joined := join(rec.events)
	if !contains(joined, "status") || !contains(joined, "delta") || !contains(joined, "done") {
		t.Fatalf("expected checkpoint status, final answer delta, and done; got %v", rec.events)
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
	// NOTE: 瞬态错误（429/5xx）的重试已下沉到 llm.Retry（见 internal/llm/retry.go，
	// 由 retry_test.go 覆盖）。agent 层不再做二次重试，故此处不再验证 agent 侧重试。
	t.Skip("agent-level LLM retry removed; covered by internal/llm/retry_test.go")
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

func TestRunContinuesPastMaxIterUntilContextCanceled(t *testing.T) {
	// LLM always emits a tool call. MaxIter is only a checkpoint; context
	// cancellation is the safety stop for a task that never reaches final text.
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c", Name: "bash", Args: "{}"}}},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	rec := &cancelAfterToolResultsEmitter{limit: 5, cancel: cancel}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 3})
	done := make(chan struct{})
	go func() {
		a.Run(ctx, RunInput{
			History: []llm.Message{{Role: llm.RoleUser, Content: "x"}}, Emit: rec,
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not terminate after context cancellation")
	}
	count := 0
	for _, e := range rec.rec.events {
		if e == "tool_call" {
			count++
		}
	}
	if count != 5 {
		t.Errorf("tool_call count=%d want 5; events=%v", count, rec.rec.events)
	}
	if !contains(join(rec.rec.events), "status") {
		t.Errorf("expected checkpoint status event, got %v", rec.rec.events)
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

// --- 🟡-6 新增测试 ---

// fakeRAG 返回固定 chunks 并统计 Retrieve 调用次数，用于验证 RAG 只注入一次。
type fakeRAG struct {
	calls  int
	chunks []RetrievedChunk
}

func (r *fakeRAG) Retrieve(ctx context.Context, kbID, query string, k int) ([]RetrievedChunk, error) {
	r.calls++
	return r.chunks, nil
}

// countingToolEngine 统计执行次数，用于验证工具上限只放行配额内的调用。
type countingToolEngine struct {
	calls int
}

func (c *countingToolEngine) List() []tools.Spec { return nil }
func (c *countingToolEngine) Execute(ctx context.Context, name, args string) (tools.Result, error) {
	c.calls++
	return tools.Result{Content: fmt.Sprintf("ran %s", name)}, nil
}

// errorToolEngine 工具执行恒返回 error，用于验证错误结果反馈给模型。
type errorToolEngine struct{}

func (errorToolEngine) List() []tools.Spec { return nil }
func (errorToolEngine) Execute(ctx context.Context, name, args string) (tools.Result, error) {
	return tools.Result{}, errors.New("boom")
}

func wrapCountingEngine(c *countingToolEngine) *tools.Engine {
	return tools.NewEngineFromFunc(c.List, c.Execute)
}

func wrapErrorEngine(e errorToolEngine) *tools.Engine {
	return tools.NewEngineFromFunc(e.List, e.Execute)
}

// TestRunInjectsRAGOnceAcrossIterations 防止 🔴-1 回归：RAG 上下文必须在
// 整个工具迭代过程中只检索、注入一次，而非每轮重复注入导致 system 膨胀。
func TestRunInjectsRAGOnceAcrossIterations(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: "{}"}}},
		{{ToolCall: &llm.ToolCall{ID: "c2", Name: "bash", Args: "{}"}}},
		{{Text: "done"}, {Done: true}},
	}}
	rag := &fakeRAG{chunks: []RetrievedChunk{
		{ID: "k1", DocID: "d1", Filename: "doc.md", Text: "知识片段内容", Similarity: 0.8},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), RAG: rag, MaxIter: 5})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "ask"}},
		Emit:    rec, UseRAG: true, KBID: "kb1",
	})

	if rag.calls != 1 {
		t.Fatalf("RAG Retrieve calls = %d, want 1 (must inject once, not per-iteration)", rag.calls)
	}
	if len(m.lastMessages) == 0 || m.lastMessages[0].Role != llm.RoleSystem {
		t.Fatalf("expected a system message, got %+v", m.lastMessages)
	}
	// "excerpt 1" 每份注入出现一次；多轮迭代后仍应只有 1 份。
	if got := strings.Count(m.lastMessages[0].Content, "excerpt 1"); got != 1 {
		t.Errorf("RAG excerpt injected %d times in system prompt, want 1; system=%q", got, m.lastMessages[0].Content)
	}
}

// TestRunStopsToolCallsAtLimit 验证 🟡-2：达到 MaxToolCalls 后不再执行新工具，
// 发出 tool_limit_reached 警告，模型随后给出最终文本。
func TestRunStopsToolCallsAtLimit(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: "{}"}}},
		{{ToolCall: &llm.ToolCall{ID: "c2", Name: "bash", Args: "{}"}}},
		{{Text: "final answer"}, {Done: true}},
	}}
	engine := &countingToolEngine{}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapCountingEngine(engine), MaxIter: 5, MaxToolCalls: 1})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "do it"}},
		Emit:    rec,
	})

	if engine.calls != 1 {
		t.Fatalf("executed tools = %d, want 1 (second call must be blocked by limit)", engine.calls)
	}
	joined := join(rec.events)
	if !rec.hasStatusKind("tool_limit_reached") || !contains(joined, "done") {
		t.Fatalf("expected tool_limit_reached status and done; got %v", rec.events)
	}
	// 第二个 tool_result 应为"未执行"（is_error=true）。
	res := rec.dataFor("tool_result", 2)
	if res == nil {
		t.Fatalf("missing second tool_result; events=%v", rec.events)
	}
	if res["is_error"] != true {
		t.Errorf("second tool_result is_error=%v, want true (skipped)", res["is_error"])
	}
	if !contains(fmt.Sprint(res["content"]), "未执行") {
		t.Errorf("second tool_result content=%v, want 未执行 hint", res["content"])
	}
}

// TestRunEmitsErrorOnNonTransientLLMError 验证 🟡-3/🟡-4：非瞬态错误不再重试，
// 直接发出 error 并终止。
func TestRunEmitsErrorOnNonTransientLLMError(t *testing.T) {
	m := &fakeLLM{
		scripts: [][]llm.Chunk{{{Text: "x"}, {Done: true}}},
		errors:  []error{errors.New("permanent failure: 401 unauthorized")},
	}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 5})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, Emit: rec,
	})

	if m.calls != 1 {
		t.Fatalf("LLM calls = %d, want 1 (no retry on non-transient error)", m.calls)
	}
	if !contains(join(rec.events), "error") {
		t.Fatalf("expected error event; got %v", rec.events)
	}
}

// TestRunToolExecErrorFedBack 验证工具执行 error 被标记为 is_error 反馈给模型。
func TestRunToolExecErrorFedBack(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: "{}"}}},
		{{Text: "recovered"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: wrapErrorEngine(errorToolEngine{}), MaxIter: 5})

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "go"}}, Emit: rec,
	})

	res := rec.dataFor("tool_result", 1)
	if res == nil {
		t.Fatalf("missing tool_result; events=%v", rec.events)
	}
	if res["is_error"] != true {
		t.Errorf("tool_result is_error=%v, want true on exec error", res["is_error"])
	}
	if !contains(join(rec.events), "done") {
		t.Errorf("expected done after feeding error back; got %v", rec.events)
	}
}

// TestRunNoToolsReturnsNotAvailable 验证 Tools=nil 时返回 "no tools available"。
func TestRunNoToolsReturnsNotAvailable(t *testing.T) {
	m := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: "{}"}}},
		{{Text: "ok"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, MaxIter: 5}) // Tools 故意留空

	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "go"}}, Emit: rec,
	})

	res := rec.dataFor("tool_result", 1)
	if res == nil {
		t.Fatalf("missing tool_result; events=%v", rec.events)
	}
	if !contains(fmt.Sprint(res["content"]), "no tools available") {
		t.Errorf("tool_result content=%v, want 'no tools available'", res["content"])
	}
}
