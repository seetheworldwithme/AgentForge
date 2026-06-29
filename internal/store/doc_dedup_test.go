package store

import (
	"path/filepath"
	"testing"
)

func TestDocumentDedup(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "dedup.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	const now = "2026-01-01T00:00:00Z"
	if err := db.CreateKB(KnowledgeBase{ID: "kb_d", Name: "K", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// d1 已就绪，hash=abc
	if err := db.CreateDocument(Document{ID: "d1", KBID: "kb_d", Status: "ready", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDocumentHash("d1", "abc"); err != nil {
		t.Fatal(err)
	}
	// d2 同 hash → 应检出重复 d1
	if dup, _ := db.FindDuplicateDoc("kb_d", "abc", "d2"); dup != "d1" {
		t.Errorf("same hash: want d1, got %q", dup)
	}
	// 不同 hash → 无重复
	if dup, _ := db.FindDuplicateDoc("kb_d", "xyz", "d2"); dup != "" {
		t.Errorf("different hash: want empty, got %q", dup)
	}
	// 排除自身：d1 查自己的 hash 不应算重复
	if dup, _ := db.FindDuplicateDoc("kb_d", "abc", "d1"); dup != "" {
		t.Errorf("exclude self: want empty, got %q", dup)
	}
	// 未就绪的文档（processing）不算重复目标
	if err := db.CreateDocument(Document{ID: "d3", KBID: "kb_d", Status: "processing", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDocumentHash("d3", "def"); err != nil {
		t.Fatal(err)
	}
	if dup, _ := db.FindDuplicateDoc("kb_d", "def", "d4"); dup != "" {
		t.Errorf("processing doc should not count as duplicate target, got %q", dup)
	}
}
