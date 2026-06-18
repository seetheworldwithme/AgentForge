package llm

import (
	"context"
	"fmt"
	"time"
)

// Retry wraps an LLMClient, retrying transient errors up to max times.
type Retry struct {
	inner LLMClient
	max   int
	wait  time.Duration
}

func NewRetry(inner LLMClient, max int, wait time.Duration) *Retry {
	return &Retry{inner: inner, max: max, wait: wait}
}

func (r *Retry) ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error) {
	var lastErr error
	for i := 0; i < r.max; i++ {
		ch, err := r.inner.ChatStream(ctx, msgs, tools)
		if err == nil {
			return ch, nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, err // non-retryable
		}
		if r.wait > 0 {
			select {
			case <-time.After(r.wait << uint(i)): // exponential-ish backoff
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("after %d retries: %w", r.max, lastErr)
}

func (r *Retry) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	var lastErr error
	for i := 0; i < r.max; i++ {
		v, err := r.inner.Embed(ctx, inputs)
		if err == nil {
			return v, nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, err
		}
		if r.wait > 0 {
			select {
			case <-time.After(r.wait << uint(i)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("after %d retries: %w", r.max, lastErr)
}

func (r *Retry) Chat(ctx context.Context, msgs []Message) (string, error) {
	var lastErr error
	for i := 0; i < r.max; i++ {
		s, err := r.inner.Chat(ctx, msgs)
		if err == nil {
			return s, nil
		}
		lastErr = err
		if !isTransient(err) {
			return "", err
		}
		if r.wait > 0 {
			select {
			case <-time.After(r.wait << uint(i)): // exponential-ish backoff
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	return "", fmt.Errorf("after %d retries: %w", r.max, lastErr)
}

// isTransient returns true for 429 / 5xx style errors (matched by substring).
func isTransient(err error) bool {
	msg := err.Error()
	return contains(msg, "429") || contains(msg, "500") ||
		contains(msg, "502") || contains(msg, "503") || contains(msg, "504")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
