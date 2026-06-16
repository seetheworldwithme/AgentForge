// internal/rag/store/chunks_test.go
package store

import (
	"path/filepath"
	"testing"

	"github.com/agentforge/agentforge/internal/rag"
)

func TestSaveChunks_AndCount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, err := New(dbPath, 8)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','test')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('doc1','kb1','a.md','markdown','indexed')`)

	chunks := []rag.Chunk{
		{DocID: "doc1", KBID: "kb1", Content: "hello world", HeadingPath: "intro", TokenCount: 2, Seq: 0},
		{DocID: "doc1", KBID: "kb1", Content: "foo bar baz", HeadingPath: "intro", TokenCount: 3, Seq: 1},
	}
	vectors := [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0, 0, 0},
	}

	ids, err := s.SaveChunks(chunks, vectors)
	if err != nil {
		t.Fatalf("SaveChunks: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}

	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM chunks WHERE kb_id='kb1'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 chunks, got %d", n)
	}
}

func TestSaveChunks_RollbackOnError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','test')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('doc1','kb1','a.md','markdown','indexed')`)

	chunks := []rag.Chunk{{DocID: "doc1", KBID: "kb1", Content: "x"}}
	vectors := [][]float32{{0, 0, 0}} // 维度不匹配

	_, err := s.SaveChunks(chunks, vectors)
	if err == nil {
		t.Fatal("expected error for dimension mismatch")
	}

	var n int
	s.DB().QueryRow("SELECT count(*) FROM chunks").Scan(&n)
	if n != 0 {
		t.Fatalf("expected 0 chunks after rollback, got %d", n)
	}
}

func mustExec(t *testing.T, s *Store, q string) {
	t.Helper()
	if _, err := s.DB().Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}
