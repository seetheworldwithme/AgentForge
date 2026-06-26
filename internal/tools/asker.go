package tools

import (
	"context"
	"log"
	"sync"
	"time"
)

// Question 是 ask_user 工具向用户提出的结构化问题。ID 由调用方（agent 的 tool
// call ID）传入，前端据此把用户的回答回传到正确的阻塞调用上。
type Question struct {
	ID      string
	Text    string
	Options []QuestionOption
}

// QuestionOption 是一个可选项：Label 是回传给模型的简短选项名，Description
// 是该选项的说明或权衡（可选，前端次行展示）。
type QuestionOption struct {
	Label       string
	Description string
}

// Answer 是用户对某个 Question 的回答。
type Answer struct {
	Selection string // 选中的 option Label
	Other     string // 用户在「其他」输入框填写的文本；非空表示走自定义输入
	Canceled  bool   // 用户取消/关闭了本次提问
}

// QuestionAsker 是 agent 依赖的最小接口，便于在测试中替换为不阻塞的 fake 实现。
// 具体实现是 *Asker。Ask 必须阻塞直到用户回答或上下文取消。
type QuestionAsker interface {
	Ask(ctx context.Context, q Question) Answer
}

// Asker 把 ask_user 工具的结构化问题转给用户并阻塞等待回答。它复用了 Gate 的
// 「阻塞 → emit → Resolve 恢复」并发骨架，但语义独立：
//   - 不检查 autoAllow / remember —— 一个问题必须始终到达用户，否则模型拿不到答案；
//   - 回答是「选了哪个选项 / 自定义输入」，而非「允许 / 拒绝」。
//
// 与 Gate 一样是进程级单例：同一时刻只有一个 chat 活跃，emitter 在每次 chat 开始时
// 由 HTTP 层 SetEmitter 绑定到该 chat 的 SSE 流，结束时恢复 no-op。
type Asker struct {
	mu      sync.Mutex
	pending map[string]chan Answer
	emit    func(Question)
}

func NewAsker() *Asker {
	return &Asker{
		pending: map[string]chan Answer{},
		emit:    func(Question) {}, // no-op default
	}
}

// SetEmitter 安装「需要向用户提问」时被调用的回调（HTTP 层用它发 ask_user_req SSE）。
func (a *Asker) SetEmitter(f func(Question)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emit = f
}

// Ask 阻塞直到 UI 通过 Resolve 回传答案，或上下文被取消。它必定 emit 并等待——
// 不像 Gate 那样可能被 autoAllow / remember 短路。
func (a *Asker) Ask(ctx context.Context, q Question) Answer {
	ch := make(chan Answer, 1)
	a.mu.Lock()
	a.pending[q.ID] = ch
	emit := a.emit
	a.mu.Unlock()

	start := time.Now()
	log.Printf("[Asker] wait id=%s options=%d text=%q", q.ID, len(q.Options), q.Text)
	emit(q)
	defer func() {
		a.mu.Lock()
		delete(a.pending, q.ID)
		a.mu.Unlock()
	}()

	select {
	case ans := <-ch:
		log.Printf("[Asker] resolved id=%s canceled=%t selection=%q other_len=%d duration=%s",
			q.ID, ans.Canceled, ans.Selection, len(ans.Other), time.Since(start).Round(time.Millisecond))
		return ans
	case <-ctx.Done():
		log.Printf("[Asker] canceled id=%s duration=%s err=%v", q.ID, time.Since(start).Round(time.Millisecond), ctx.Err())
		return Answer{Canceled: true}
	}
}

// Resolve 把用户的回答投递给对应 ID 的阻塞 Ask 调用。返回 false 表示没有该 ID 的
// 待处理提问（可能已超时/取消或 ID 不匹配）。
func (a *Asker) Resolve(id string, ans Answer) bool {
	a.mu.Lock()
	ch, ok := a.pending[id]
	a.mu.Unlock()
	if !ok {
		return false
	}
	ch <- ans
	return true
}
