// internal/rag/store/crud_test.go
package store

import (
	"path/filepath"
	"testing"
)

func TestCreateKnowledgeBaseAndGet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()

	id, err := s.CreateKnowledgeBase("我的笔记", "text-embedding-3-small", 512, 50)
	if err != nil {
		t.Fatalf("CreateKnowledgeBase: %v", err)
	}
	kb, err := s.GetKnowledgeBase(id)
	if err != nil {
		t.Fatalf("GetKnowledgeBase: %v", err)
	}
	if kb.Name != "我的笔记" || kb.ChunkSize != 512 {
		t.Errorf("unexpected kb: %+v", kb)
	}
}

func TestListKnowledgeBases(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	s.CreateKnowledgeBase("a", "m", 512, 50)
	s.CreateKnowledgeBase("b", "m", 512, 50)
	kbs, _ := s.ListKnowledgeBases()
	if len(kbs) != 2 {
		t.Fatalf("expected 2 kbs, got %d", len(kbs))
	}
}

func TestGetDocumentByHash(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	kbID, _ := s.CreateKnowledgeBase("a", "m", 512, 50)
	s.CreateDocument(kbID, "a.md", "markdown", "hash123")

	existing, _ := s.GetDocumentByHash("hash123")
	if existing == nil {
		t.Fatal("expected to find doc by hash")
	}
	none, _ := s.GetDocumentByHash("nonexist")
	if none != nil {
		t.Error("expected nil for nonexist")
	}
}

// TestGetMissingReturnsNil 守护 by-key 查询在记录不存在时统一返回 (nil, nil)，
// 而非把 sql.ErrNoRows 泄漏给上层（GetDocument/GetKnowledgeBase/GetDocumentByHash 一致）。
func TestGetMissingReturnsNil(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()

	if kb, err := s.GetKnowledgeBase("no-such-kb"); err != nil || kb != nil {
		t.Errorf("GetKnowledgeBase(missing) = (%+v, %v), want (nil, nil)", kb, err)
	}
	if doc, err := s.GetDocument("no-such-doc"); err != nil || doc != nil {
		t.Errorf("GetDocument(missing) = (%+v, %v), want (nil, nil)", doc, err)
	}
}
