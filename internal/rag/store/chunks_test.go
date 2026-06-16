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

	// 守护 chunk↔vec_chunks 的 rowid 关联：T17 Search 的 JOIN 依赖此关系。
	// vec_chunks 必须用 chunk 的 rowid，否则两表按各自独立自增会错位。
	var vecN int
	if err := s.DB().QueryRow("SELECT count(*) FROM vec_chunks").Scan(&vecN); err != nil {
		t.Fatalf("count vec_chunks: %v", err)
	}
	if vecN != 2 {
		t.Fatalf("expected 2 vec rows, got %d", vecN)
	}
	rows, err := s.DB().Query(`SELECT c.id, c.content FROM vec_chunks v JOIN chunks c ON c.id = v.rowid ORDER BY c.id`)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	defer rows.Close()
	type pair struct {
		id   int64
		text string
	}
	var got []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.text); err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	if len(got) != 2 || got[0].text != "hello world" || got[1].text != "foo bar baz" {
		t.Fatalf("join results wrong: %+v", got)
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
