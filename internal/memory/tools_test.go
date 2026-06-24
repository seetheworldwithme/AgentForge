package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/tools"
)

func TestMemorySaveTool(t *testing.T) {
	s, _ := newTestStore(t)
	tt := &saveTool{store: s}
	spec := tt.Spec()
	if spec.Name != "memory_save" {
		t.Fatalf("spec name: %s", spec.Name)
	}
	args := `{"name":"go-env","description":"Go 环境坑","type":"project","body":"正文"}`
	res, err := tt.Run(context.Background(), args, tools.NewAutoAllowGate())
	if err != nil || res.IsError {
		t.Fatalf("run: %v %+v", err, res)
	}
	got, _ := s.List()
	if len(got) != 1 || got[0].Name != "go-env" || got[0].Body != "正文" {
		t.Fatalf("not saved: %+v", got)
	}
}

func TestMemoryReadTool(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "d", Type: TypeProject, Body: "正文"})
	rt := &readTool{store: s}
	res, _ := rt.Run(context.Background(), `{"name":"go-env"}`, tools.NewAutoAllowGate())
	if res.IsError || !strings.Contains(res.Content, "正文") {
		t.Fatalf("read result: %+v", res)
	}
}

func TestMemoryDeleteTool(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "d", Type: TypeProject, Body: "b"})
	dt := &deleteTool{store: s}
	res, _ := dt.Run(context.Background(), `{"name":"go-env"}`, tools.NewAutoAllowGate())
	if res.IsError {
		t.Fatalf("delete failed: %+v", res)
	}
	got, _ := s.List()
	if len(got) != 0 {
		t.Fatalf("not deleted: %+v", got)
	}
}

func TestToolsRegistry(t *testing.T) {
	s, _ := newTestStore(t)
	ts := Tools(s)
	names := map[string]bool{}
	for _, tk := range ts {
		names[tk.Spec().Name] = true
	}
	for _, want := range []string{"memory_save", "memory_read", "memory_delete"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}
