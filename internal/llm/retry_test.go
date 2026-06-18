package llm

import (
	"context"
	"errors"
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
		return nil, errors.New("http 429")
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
