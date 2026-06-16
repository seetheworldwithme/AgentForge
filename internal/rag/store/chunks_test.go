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

	// 制造 rowid 偏移：先插一条占位 chunk（id=1，保留不删），使 SaveChunks
	// 的 chunk id 从 2 起。这样若 vec_chunks 用独立自增 rowid（从 1 起），
	// 会与 chunks.id 错位，JOIN 守护就能抓到回归。
	mustExec(t, s, `INSERT INTO chunks(id,doc_id,kb_id,content) VALUES(1,'doc1','kb1','placeholder')`)

	ids, err := s.SaveChunks(chunks, vectors)
	if err != nil {
		t.Fatalf("SaveChunks: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}
	// 有占位行时 chunk id 应为 2,3；vec_chunks 必须用同样 rowid 才能正确 JOIN。
	if ids[0] != 2 || ids[1] != 3 {
		t.Fatalf("expected ids [2,3] with placeholder, got %v", ids)
	}

	var n int
	if err := s.DB().QueryRow("SELECT count(*) FROM chunks WHERE kb_id='kb1'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	// 占位 1 + SaveChunks 2 = 3
	if n != 3 {
		t.Fatalf("expected 3 chunks (incl placeholder), got %d", n)
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
	// JOIN 限定到非占位的两条（content != 'placeholder'），验证内容按 id 顺序正确。
	rows, err := s.DB().Query(`SELECT c.id, c.content FROM vec_chunks v JOIN chunks c ON c.id = v.rowid WHERE c.content != 'placeholder' ORDER BY c.id`)
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
	if got[0].id != 2 || got[1].id != 3 {
		t.Fatalf("join ids wrong, expected [2,3], got %v", got)
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
