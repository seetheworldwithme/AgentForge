// internal/rag/store/search_test.go
package store

import (
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
)

func TestSearch_ReturnsTopKBySimilarity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','t')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('doc1','kb1','a.md','markdown','indexed')`)

	chunks := []rag.Chunk{
		{DocID: "doc1", KBID: "kb1", Content: "target", Seq: 0},
		{DocID: "doc1", KBID: "kb1", Content: "other", Seq: 1},
	}
	vectors := [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 1},
	}
	if _, err := s.SaveChunks(chunks, vectors); err != nil {
		t.Fatal(err)
	}

	queryVec := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	results, err := s.Search("kb1", queryVec, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Content != "target" {
		t.Errorf("expected top result 'target', got %q", results[0].Content)
	}
}

func TestSearch_FiltersByKnowledgeBase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','t')`)
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb2','t')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('d1','kb1','a','m','indexed')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('d2','kb2','b','m','indexed')`)

	s.SaveChunks([]rag.Chunk{{DocID: "d1", KBID: "kb1", Content: "in kb1"}}, [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}})
	s.SaveChunks([]rag.Chunk{{DocID: "d2", KBID: "kb2", Content: "in kb2"}}, [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}})

	q := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	res, err := s.Search("kb1", q, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Content != "in kb1" {
		t.Fatalf("kb1 search should only return kb1 chunks, got %v", res)
	}
}
