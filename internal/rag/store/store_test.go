// internal/rag/store/store_test.go
package store

import (
	"path/filepath"
	"testing"
)

func TestNew_CreatesSchemaAndLoadsVec(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(dbPath, 768)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer s.Close()

	var ver string
	err = s.db.QueryRow("SELECT vec_version()").Scan(&ver)
	if err != nil {
		t.Fatalf("sqlite-vec not loaded: %v", err)
	}
	if ver == "" {
		t.Fatal("vec_version() returned empty")
	}

	for _, table := range []string{"knowledge_bases", "documents", "chunks", "eval_questions", "eval_expected", "eval_runs"} {
		var name string
		err = s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s not created: %v", table, err)
		}
	}
}
