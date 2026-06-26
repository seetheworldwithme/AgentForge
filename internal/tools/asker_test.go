package tools

import (
	"context"
	"testing"
	"time"
)

// TestAskerResolve 验证 Resolve 能解除对应 Ask 的阻塞并原样返回用户答案。
// 在 SetEmitter 回调里直接 Resolve：emit 发生在「pending 已注册、select 阻塞前」，
// 故可同步完成，无竞态、无需 sleep。
func TestAskerResolve(t *testing.T) {
	a := NewAsker()
	var emitted Question
	a.SetEmitter(func(q Question) {
		emitted = q
		if !a.Resolve(q.ID, Answer{Selection: "A"}) {
			t.Errorf("Resolve returned false for just-emitted id %s", q.ID)
		}
	})
	ans := a.Ask(context.Background(), Question{
		ID: "q1", Text: "选哪个？",
		Options: []QuestionOption{{Label: "A"}, {Label: "B"}},
	})
	if emitted.ID != "q1" || emitted.Text != "选哪个？" || len(emitted.Options) != 2 {
		t.Fatalf("emitter received wrong question: %+v", emitted)
	}
	if ans.Canceled || ans.Selection != "A" {
		t.Fatalf("expected {Selection:A}, got %+v", ans)
	}
}

// TestAskerCancel 验证上下文取消时 Ask 返回 Canceled，且不再阻塞。
func TestAskerCancel(t *testing.T) {
	a := NewAsker()
	a.SetEmitter(func(Question) {}) // 模拟用户一直不答
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ans := a.Ask(ctx, Question{ID: "q2", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}})
	if !ans.Canceled {
		t.Fatalf("expected Canceled on ctx done, got %+v", ans)
	}
}

// TestAskerResolveUnknown 验证对不存在/已结束的 id 调 Resolve 返回 false。
func TestAskerResolveUnknown(t *testing.T) {
	a := NewAsker()
	if a.Resolve("nope", Answer{Selection: "X"}) {
		t.Fatal("Resolve on unknown id should return false")
	}
}

// TestAskerResolveCleansUp 验证 Ask 返回后其 pending 被清理：再次 Resolve 同一 id 应失败。
func TestAskerResolveCleansUp(t *testing.T) {
	a := NewAsker()
	a.SetEmitter(func(q Question) { a.Resolve(q.ID, Answer{Selection: "A"}) })
	_ = a.Ask(context.Background(), Question{ID: "q3", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}})
	if a.Resolve("q3", Answer{}) {
		t.Fatal("pending entry should be cleaned up after Ask returns")
	}
}
