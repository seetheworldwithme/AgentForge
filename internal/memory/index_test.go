package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReindexWritesMemoryMd(t *testing.T) {
	s, wd := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "b"})
	_ = s.Save(Entry{Name: "frontend", Description: "前端设计", Type: TypeFeedback, Body: "b"})
	err := s.Reindex()
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(wd, DirName, IndexFile))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	txt := string(idx)
	if !strings.Contains(txt, "Go 环境坑") || !strings.Contains(txt, "前端设计") {
		t.Errorf("index missing entries: %s", txt)
	}
	if !strings.Contains(txt, "(go-env.md)") || !strings.Contains(txt, "(frontend.md)") {
		t.Errorf("index missing links: %s", txt)
	}
	if !strings.Contains(txt, "· project") || !strings.Contains(txt, "· feedback") {
		t.Errorf("index missing type tags: %s", txt)
	}
}

func TestIndexContextInjectableText(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "b"})
	ctx := s.IndexContext()
	if !strings.Contains(ctx, "Go 环境坑") {
		t.Errorf("context missing entry: %s", ctx)
	}
	if !strings.Contains(ctx, "memory_read") || !strings.Contains(ctx, "memory_save") {
		t.Errorf("context missing tool hints: %s", ctx)
	}
}

func TestIndexContextEmptyWhenNoMemory(t *testing.T) {
	s, _ := newTestStore(t)
	if ctx := s.IndexContext(); ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}
}
