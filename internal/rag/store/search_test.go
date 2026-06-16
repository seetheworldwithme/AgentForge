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

// TestSearch_UsesCosineMetric 守护距离度量必须是 cosine 而非默认 L2。
// 用非正交、非单位长度的向量构造场景：query=(2,0,...)，a=(1,0,...) 与 query 同向，
// b=(0,1,0,...) 与 query 正交。cosine 下 a 完全匹配(distance=0, score=1)；
// 若误用 L2，a 的 distance=1。同时守护 score = 1 - cosine_distance ∈ [0,1]。
func TestSearch_UsesCosineMetric(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	mustExec(t, s, `INSERT INTO knowledge_bases(id,name) VALUES('kb1','t')`)
	mustExec(t, s, `INSERT INTO documents(id,kb_id,file_path,file_type,status) VALUES('doc1','kb1','a.md','markdown','indexed')`)

	// a 与 query 同向(不同长度)，b 与 query 正交。这是区分 L2/cosine 的关键场景。
	s.SaveChunks([]rag.Chunk{
		{DocID: "doc1", KBID: "kb1", Content: "aligned", Seq: 0},
		{DocID: "doc1", KBID: "kb1", Content: "orthogonal", Seq: 1},
	}, [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0}, // a: 与 query 同向，长度不同
		{0, 1, 0, 0, 0, 0, 0, 0}, // b: 与 query 正交
	})

	queryVec := []float32{2, 0, 0, 0, 0, 0, 0, 0}
	results, err := s.Search("kb1", queryVec, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Content != "aligned" {
		t.Errorf("cosine should rank same-direction 'aligned' first, got %q", results[0].Content)
	}
	// cosine 距离：aligned=0 → score=1.0；orthogonal=1 → score=0.0。
	// 若误用 L2：aligned 的 distance=1（不是 0），score 会是 0，此断言会失败。
	if results[0].Score < 0.99 || results[0].Score > 1.01 {
		t.Errorf("aligned score should be ~1.0 (cosine same direction), got %f", results[0].Score)
	}
	if results[1].Score < -0.01 || results[1].Score > 0.01 {
		t.Errorf("orthogonal score should be ~0.0, got %f", results[1].Score)
	}
}

func TestSearch_RejectsNonPositiveTopK(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	s, _ := New(dbPath, 8)
	defer s.Close()
	for _, k := range []int{0, -1} {
		if _, err := s.Search("kb1", []float32{1, 0, 0, 0, 0, 0, 0, 0}, k); err == nil {
			t.Fatalf("expected error for topK=%d", k)
		}
	}
}
