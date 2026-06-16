// internal/rag/embedder/embedder_test.go
package embedder

import "testing"

func TestFakeEmbedder_Deterministic(t *testing.T) {
	e := NewFakeEmbedder(8)
	v1, _ := e.EmbedOne("hello world")
	v2, _ := e.EmbedOne("hello world")
	if len(v1) != 8 {
		t.Fatalf("expected dim 8, got %d", len(v1))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("not deterministic at idx %d", i)
		}
	}
}

func TestFakeEmbedder_EmbedBatch(t *testing.T) {
	e := NewFakeEmbedder(4)
	vecs, err := e.Embed([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vecs))
	}
}
