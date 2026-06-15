package tool

import (
	"context"
	"reflect"
	"testing"
)

// fakeTool 是测试用的 Tool 实现。
type fakeTool struct {
	name string
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "fake" }
func (f *fakeTool) Schema() []byte      { return []byte(`{}`) }
func (f *fakeTool) Execute(ctx context.Context, args []byte) (<-chan Event, error) {
	ch := make(chan Event)
	go func() {
		defer close(ch)
		ch <- Event{Kind: EventResult, Result: &Result{Content: "ok"}}
	}()
	return ch, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	t1 := &fakeTool{name: "tool_a"}

	r.Register(t1)

	got, ok := r.Get("tool_a")
	if !ok {
		t.Fatal("expected to find registered tool")
	}
	if got.Name() != "tool_a" {
		t.Fatalf("got name %q, want %q", got.Name(), "tool_a")
	}
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get("nonexistent"); ok {
		t.Fatal("expected miss for unregistered tool")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeTool{name: "a"})
	r.Register(&fakeTool{name: "b"})

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(list))
	}
	names := map[string]bool{}
	for _, tool := range list {
		names[tool.Name()] = true
	}
	if !reflect.DeepEqual(names, map[string]bool{"a": true, "b": true}) {
		t.Fatalf("unexpected tool names: %v", names)
	}
}
