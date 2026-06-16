package store

import (
	"path/filepath"
	"testing"
)

func TestVecInsertAndSearch(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "vec.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify the extension actually loaded; if vec0 isn't available the whole
	// RAG subsystem is unusable, so we fail loudly rather than skip.
	if err := db.EnsureVecTable("test_vec", 3); err != nil {
		t.Fatalf("EnsureVecTable (vec0 likely not loaded): %v", err)
	}

	vec := [][]float32{
		{1.0, 0.0, 0.0},
		{0.0, 1.0, 0.0},
		{0.9, 0.1, 0.0}, // close to the first
	}
	ids := []string{"a", "b", "c"}
	for i := range vec {
		if err := db.InsertVector("test_vec", ids[i], vec[i]); err != nil {
			t.Fatalf("InsertVector %d: %v", i, err)
		}
	}

	got, err := db.SearchVectors("test_vec", []float32{1.0, 0.0, 0.0}, 1)
	if err != nil {
		t.Fatalf("SearchVectors: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("got %+v, want top hit id=a", got)
	}
}
