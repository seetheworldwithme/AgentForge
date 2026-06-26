package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/tools"
)

// fakeQuestionAsker 记录收到的问题，并返回预设答案，便于断言 handleAskUser 的格式化。
type fakeQuestionAsker struct {
	answer tools.Answer
	got    tools.Question
}

func (f *fakeQuestionAsker) Ask(_ context.Context, q tools.Question) tools.Answer {
	f.got = q
	return f.answer
}

func newAskAgent(ans tools.Answer) (*Agent, *fakeQuestionAsker) {
	f := &fakeQuestionAsker{answer: ans}
	return New(Deps{Asker: f}), f
}

func TestHandleAskUserSelection(t *testing.T) {
	a, f := newAskAgent(tools.Answer{Selection: "Redis"})
	res := a.handleAskUser(context.Background(), "call_1",
		`{"question":"用哪个缓存?","options":[{"label":"Redis","description":"内存"},{"label":"本地文件"}]}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "用户选择：Redis") {
		t.Fatalf("want selection echoed, got %q", res.Content)
	}
	if f.got.ID != "call_1" || f.got.Text != "用哪个缓存?" || len(f.got.Options) != 2 {
		t.Fatalf("question passed to Asker wrong: %+v", f.got)
	}
	if f.got.Options[0].Description != "内存" { // description 应透传
		t.Fatalf("description not forwarded: %+v", f.got.Options[0])
	}
}

func TestHandleAskUserOther(t *testing.T) {
	a, _ := newAskAgent(tools.Answer{Other: " 我自己写一个 "})
	res := a.handleAskUser(context.Background(), "c", `{"question":"q?","options":[{"label":"A"},{"label":"B"}]}`)
	if !strings.Contains(res.Content, "用户选择（其他）：我自己写一个") {
		t.Fatalf("want trimmed other echoed, got %q", res.Content)
	}
}

func TestHandleAskUserCanceled(t *testing.T) {
	a, _ := newAskAgent(tools.Answer{Canceled: true})
	res := a.handleAskUser(context.Background(), "c", `{"question":"q?","options":[{"label":"A"},{"label":"B"}]}`)
	if res.IsError {
		t.Fatalf("cancel should not be an error result: %s", res.Content)
	}
	if !strings.Contains(res.Content, "用户取消了本次提问") {
		t.Fatalf("want cancel message, got %q", res.Content)
	}
}

func TestHandleAskUserValidation(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"empty question", `{"question":"  ","options":[{"label":"A"},{"label":"B"}]}`},
		{"too few options", `{"question":"q?","options":[{"label":"A"}]}`},
		{"too many options", `{"question":"q?","options":[{"label":"A"},{"label":"B"},{"label":"C"},{"label":"D"},{"label":"E"}]}`},
		{"bad json", `{not json`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := newAskAgent(tools.Answer{Selection: "A"})
			res := a.handleAskUser(context.Background(), "c", c.args)
			if !res.IsError {
				t.Fatalf("expected error for %s, got %q", c.name, res.Content)
			}
		})
	}
}

// TestHandleAskUserDedup 验证 label 重复/空导致有效选项不足 2 时报错。
func TestHandleAskUserDedup(t *testing.T) {
	a, _ := newAskAgent(tools.Answer{Selection: "A"})
	res := a.handleAskUser(context.Background(), "c",
		`{"question":"q?","options":[{"label":"A"},{"label":"A"},{"label":""}]}`)
	if !res.IsError {
		t.Fatalf("expected error for deduped-to-<2 options, got %q", res.Content)
	}
}
