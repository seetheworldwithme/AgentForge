package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/tools"
)

// recordedCall 记录一次 ChatStream 调用收到的消息与工具规格，用于断言子 agent 的
// prompt 注入 / 工具白名单 / 上下文隔离 等行为。
type recordedCall struct {
	msgs  []llm.Message
	tools []llm.ToolSpec
}

// recordingLLM 是脚本驱动的 LLM mock：按调用顺序依次消费 scripts 中的一段流式响应，
// 并把每次调用收到的 (msgs, tools) 记录到 calls。delay 用于超时测试。
type recordingLLM struct {
	scripts [][]llm.Chunk
	calls   []recordedCall
	delay   time.Duration
}

func (r *recordingLLM) ChatStream(ctx context.Context, msgs []llm.Message, ts []llm.ToolSpec) (<-chan llm.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	idx := len(r.calls)
	r.calls = append(r.calls, recordedCall{
		msgs:  append([]llm.Message(nil), msgs...),
		tools: append([]llm.ToolSpec(nil), ts...),
	})
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan llm.Chunk, 4)
	if idx < len(r.scripts) {
		for _, c := range r.scripts[idx] {
			ch <- c
		}
	} else {
		ch <- llm.Chunk{Text: "(no more script)", Done: true} // 超出脚本：终止，避免空转
	}
	close(ch)
	return ch, nil
}

func (r *recordingLLM) Chat(ctx context.Context, msgs []llm.Message) (string, error) {
	return "", nil
}

func (r *recordingLLM) Embed(ctx context.Context, in []string) ([][]float32, error) {
	return nil, nil
}

// subTestEngine 构造含读写工具的测试引擎，用于验证子 agent 的工具白名单过滤。
func subTestEngine() *tools.Engine {
	specs := []tools.Spec{
		{Name: "file_read", Description: "read", Parameters: "{}"},
		{Name: "file_write", Description: "write", Parameters: "{}"},
		{Name: "file_edit", Description: "edit", Parameters: "{}"},
		{Name: "bash", Description: "bash", Parameters: "{}"},
		{Name: "grep", Description: "grep", Parameters: "{}"},
	}
	return tools.NewEngineFromFunc(
		func() []tools.Spec { return specs },
		func(ctx context.Context, name, args string) (tools.Result, error) {
			return tools.Result{Content: "ran " + name}, nil
		},
	)
}

func toolNames(specs []llm.ToolSpec) map[string]bool {
	m := make(map[string]bool, len(specs))
	for _, s := range specs {
		m[s.Name] = true
	}
	return m
}

// TestClampSubToolCalls 验证子 agent 工具上限收敛：父的 1/3，clamp 到 [5,30]；父无上限给 10。
func TestClampSubToolCalls(t *testing.T) {
	cases := []struct{ parent, want int }{
		{60, 20}, {30, 10}, {90, 30}, {9, 5}, {3, 5}, {100, 30}, {0, 10}, {-1, 10},
	}
	for _, c := range cases {
		if got := clampSubToolCalls(c.parent); got != c.want {
			t.Errorf("clampSubToolCalls(%d) = %d, want %d", c.parent, got, c.want)
		}
	}
}

// TestDispatchSpecListsSubagents 验证 dispatch 工具的 description 列出三个内置子 agent，
// 让主 agent 知道有哪些 subagent_type 可选。
func TestDispatchSpecListsSubagents(t *testing.T) {
	desc := dispatchToolSpec().Description
	for _, key := range []string{"explorer", "reviewer", "planner"} {
		if !strings.Contains(desc, key) {
			t.Errorf("dispatch spec description 应列出 %q, got: %q", key, desc)
		}
	}
}

// TestRunSubAgent_RoutingAndPrompt 验证三个子 agent 各自的 persona 正确注入（user 消息
// 含对应 prompt 特征词），且用独立上下文（首轮只有 system+user 两条）。
func TestRunSubAgent_RoutingAndPrompt(t *testing.T) {
	cases := []struct{ typeKey, wantFrag string }{
		{"explorer", "代码探索"},
		{"reviewer", "代码审查"},
		{"planner", "实施计划"},
	}
	for _, c := range cases {
		m := &recordingLLM{scripts: [][]llm.Chunk{{{Text: "ok"}, {Done: true}}}}
		a := New(Deps{LLM: m, Tools: subTestEngine(), MaxIter: 5})
		def := builtinSubagents[c.typeKey]
		res := a.runSubAgent(context.Background(), "任务X", def, false)

		if res.IsError {
			t.Errorf("[%s] unexpected error: %v", c.typeKey, res.Content)
			continue
		}
		if len(m.calls) != 1 {
			t.Errorf("[%s] 子 agent LLM 调用次数=%d, want 1", c.typeKey, len(m.calls))
			continue
		}
		msgs := m.calls[0].msgs
		if len(msgs) != 2 {
			t.Errorf("[%s] 子首轮消息数=%d, want 2 (system+user, 独立上下文无父历史)", c.typeKey, len(msgs))
			continue
		}
		if !strings.Contains(msgs[1].Content, c.wantFrag) {
			t.Errorf("[%s] 子 user 消息应含 prompt 特征 %q, got: %q", c.typeKey, c.wantFrag, msgs[1].Content)
		}
		if !strings.Contains(res.Content, def.Title) {
			t.Errorf("[%s] 结果前缀应含 Title %q, got: %q", c.typeKey, def.Title, res.Content)
		}
	}
}

// TestSubagentToolWhitelist 验证每个子 agent 的工具白名单：只暴露允许的只读工具，
// 屏蔽写工具与 dispatch_agent（防递归）。这是"只读"的硬保证。
func TestSubagentToolWhitelist(t *testing.T) {
	cases := []struct {
		typeKey  string
		mustHave []string
		mustNot  []string
	}{
		{"explorer", []string{"file_read", "grep", "bash"}, []string{"file_write", "file_edit"}},
		{"reviewer", []string{"file_read", "grep"}, []string{"bash", "file_write", "file_edit"}},
		{"planner", []string{"file_read", "grep", "bash"}, []string{"file_write", "file_edit"}},
	}
	for _, c := range cases {
		m := &recordingLLM{scripts: [][]llm.Chunk{{{Text: "ok"}, {Done: true}}}}
		a := New(Deps{LLM: m, Tools: subTestEngine(), MaxIter: 5})
		_ = a.runSubAgent(context.Background(), "task", builtinSubagents[c.typeKey], false)
		if len(m.calls) == 0 {
			t.Fatalf("[%s] 子 agent 未发起 LLM 调用", c.typeKey)
		}
		names := toolNames(m.calls[0].tools)
		for _, n := range c.mustHave {
			if !names[n] {
				t.Errorf("[%s] 应暴露 %s, tools=%v", c.typeKey, n, names)
			}
		}
		for _, n := range c.mustNot {
			if names[n] {
				t.Errorf("[%s] 不应暴露 %s（只读保证）, tools=%v", c.typeKey, n, names)
			}
		}
		if names[dispatchToolName] {
			t.Errorf("[%s] 不应暴露 dispatch_agent（防递归）", c.typeKey)
		}
	}
}

// TestRunSubAgent_EventIsolation 验证命门：子 agent 的内部 delta 不泄漏到父事件流，
// 但 dispatch 的 tool_result 携带子摘要（带子 agent 类型前缀）。
func TestRunSubAgent_EventIsolation(t *testing.T) {
	subDigest := "SUBAGENT_UNIQUE_DIGEST_xyz789"
	m := &recordingLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "d1", Name: "dispatch_agent", Args: `{"subagent_type":"explorer","task":"调研X"}`}}},
		{{Text: subDigest}, {Done: true}},       // 子 1 轮：摘要
		{{Text: "主agent最终回答"}, {Done: true}}, // 父第2轮：收尾
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: m, Tools: subTestEngine(), MaxIter: 5})
	a.Run(context.Background(), RunInput{
		History:      []llm.Message{{Role: llm.RoleUser, Content: "帮我调研X"}},
		Emit:         rec,
		ToolsEnabled: true,
	})

	var parentDelta strings.Builder
	for i, e := range rec.events {
		if e != "delta" || i >= len(rec.data) || rec.data[i] == nil {
			continue
		}
		if t, ok := rec.data[i]["text"].(string); ok {
			parentDelta.WriteString(t)
		}
	}
	if strings.Contains(parentDelta.String(), subDigest) {
		t.Errorf("子 agent 的 delta 泄漏到父事件流（事件隔离失败），parentDelta=%q", parentDelta.String())
	}
	if !strings.Contains(parentDelta.String(), "主agent最终回答") {
		t.Errorf("父最终回答缺失，parentDelta=%q", parentDelta.String())
	}

	tr := rec.dataFor("tool_result", 1)
	if tr == nil {
		t.Fatal("缺少 dispatch 的 tool_result")
	}
	if content, _ := tr["content"].(string); !strings.Contains(content, subDigest) || !strings.Contains(content, "探索") {
		t.Errorf("dispatch tool_result 应含子摘要与「探索」前缀，got: %v", tr["content"])
	}
}

// TestPrimaryAgentExposesDispatch 验证主 agent（New 构造）会向模型暴露 dispatch_agent。
func TestPrimaryAgentExposesDispatch(t *testing.T) {
	m := &recordingLLM{scripts: [][]llm.Chunk{{{Text: "ok"}, {Done: true}}}}
	a := New(Deps{LLM: m, Tools: subTestEngine(), MaxIter: 5})
	a.Run(context.Background(), RunInput{
		History:      []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:         &recorderEmitter{},
		ToolsEnabled: true,
	})
	if len(m.calls) == 0 {
		t.Fatal("主 agent 未发起 LLM 调用")
	}
	if names := toolNames(m.calls[0].tools); !names[dispatchToolName] {
		t.Errorf("主 agent 应暴露 dispatch_agent，tools=%v", names)
	}
}

// TestHandleDispatch_Routing 验证 subagent_type 路由与校验：合法 type 成功；空/未知/坏JSON/空task 报错。
func TestHandleDispatch_Routing(t *testing.T) {
	a := New(Deps{LLM: &recordingLLM{scripts: [][]llm.Chunk{{{Text: "ok"}, {Done: true}}}}, Tools: subTestEngine(), MaxIter: 5})

	if res := a.handleDispatch(context.Background(), `{"subagent_type":"explorer","task":"找X"}`, false); res.IsError {
		t.Errorf("explorer 路由应成功： %v", res.Content)
	}
	if res := a.handleDispatch(context.Background(), `{"task":"x"}`, false); !res.IsError || !strings.Contains(res.Content, "subagent_type") {
		t.Errorf("空 subagent_type 应报错： %+v", res)
	}
	if res := a.handleDispatch(context.Background(), `{"subagent_type":"hacker","task":"x"}`, false); !res.IsError || !strings.Contains(res.Content, "未知") {
		t.Errorf("未知 subagent_type 应报错： %+v", res)
	}
	if res := a.handleDispatch(context.Background(), "not json", false); !res.IsError {
		t.Errorf("坏 JSON 应报错： %+v", res)
	}
	if res := a.handleDispatch(context.Background(), `{"subagent_type":"explorer","task":""}`, false); !res.IsError {
		t.Errorf("空 task 应报错： %+v", res)
	}
}

// TestRunSubAgent_Timeout 验证子 agent 在父 ctx 超时后正确中止并返回占位摘要（非空）。
func TestRunSubAgent_Timeout(t *testing.T) {
	m := &recordingLLM{
		scripts: [][]llm.Chunk{{{Text: "结果"}, {Done: true}}},
		delay:   400 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	a := New(Deps{LLM: m, Tools: subTestEngine(), MaxIter: 5})

	done := make(chan tools.Result, 1)
	go func() { done <- a.runSubAgent(ctx, "task", builtinSubagents["explorer"], false) }()
	select {
	case res := <-done:
		if !strings.Contains(res.Content, "未产生输出") {
			t.Errorf("超时后应返回占位摘要（未产生输出），got: %q", res.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runSubAgent 超时未返回（未正确响应 ctx 取消）")
	}
}
