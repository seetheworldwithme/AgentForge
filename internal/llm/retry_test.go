package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type flakyClient struct {
	calls int
	fail  int // fail first N calls with this error
}

func (f *flakyClient) Chat(ctx context.Context, msgs []Message) (string, error) {
	return "", nil
}

func (f *flakyClient) ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error) {
	f.calls++
	if f.calls <= f.fail {
		return nil, &HTTPError{StatusCode: 429, Body: "rate limited"}
	}
	ch := make(chan Chunk, 1)
	ch <- Chunk{Text: "ok", Done: true}
	close(ch)
	return ch, nil
}

func (f *flakyClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	return nil, nil
}

func TestRetryRetriesThenSucceeds(t *testing.T) {
	f := &flakyClient{fail: 2}
	r := NewRetry(f, 3, 0) // 3 max, no sleep in tests
	ch, err := r.ChatStream(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var text string
	for c := range ch {
		text += c.Text
	}
	if text != "ok" {
		t.Errorf("text=%q want ok", text)
	}
	if f.calls != 3 {
		t.Errorf("calls=%d want 3", f.calls)
	}
}

func TestRetryGivesUpAfterMax(t *testing.T) {
	f := &flakyClient{fail: 99}
	r := NewRetry(f, 2, 0)
	_, err := r.ChatStream(context.Background(), nil, nil)
	if err == nil {
		t.Errorf("want error after exhausting retries")
	}
	if f.calls != 2 {
		t.Errorf("calls=%d want 2", f.calls)
	}
}

// TestIsTransient 验证 🟡-4：瞬态判定基于 *HTTPError 类型（errors.As），
// 429/5xx 可重试，4xx 与普通错误不可重试，且对包装错误仍能命中。
func TestIsTransient(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&HTTPError{StatusCode: 429}, true},
		{&HTTPError{StatusCode: 500}, true},
		{&HTTPError{StatusCode: 503}, true},
		{&HTTPError{StatusCode: 401}, false},
		{&HTTPError{StatusCode: 400}, false},
		{errors.New("some transport error"), false},
		{fmt.Errorf("wrapped: %w", &HTTPError{StatusCode: 429}), true},
	}
	for _, c := range cases {
		if got := isTransient(c.err); got != c.want {
			t.Errorf("isTransient(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
