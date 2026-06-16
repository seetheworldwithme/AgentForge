package store

import (
	"path/filepath"
	"testing"
)

func TestOpenRunsMigrations(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// schema should create all tables; query each to verify
	for _, table := range []string{"providers", "settings", "sessions", "messages",
		"knowledge_bases", "documents", "chunks", "tool_allowlist"} {
		var name string
		err := db.sql.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing after migration: %v", table, err)
		}
	}

	// WAL mode should be enabled
	var journal string
	_ = db.sql.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
}
