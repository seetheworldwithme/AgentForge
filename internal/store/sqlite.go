package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	sql *sql.DB
}

// vecExtPath returns the platform-specific sqlite-vec extension path next to
// the running binary (or test temp dir). Returns empty string if none exists.
func vecExtPath() string {
	name := map[string]string{
		"windows": "sqlite-vec.dll",
		"darwin":  "sqlite-vec.dylib",
		"linux":   "sqlite-vec.so",
	}[runtime.GOOS]
	if name == "" {
		return ""
	}
	// search next to the executable first
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "ext", runtime.GOOS, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// also search relative to the working dir (useful in tests / dev)
	candidate := filepath.Join("ext", runtime.GOOS, name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func init() {
	// Register a driver that auto-loads vec0 on every new connection, but
	// tolerates the extension being absent (vector ops will then error later).
	sql.Register("sqlite3_vec", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			p := vecExtPath()
			if p == "" {
				return nil // no extension available; vector ops error later
			}
			if err := conn.LoadExtension(p, "sqlite3_vec_init"); err != nil {
				return fmt.Errorf("load sqlite-vec: %w", err)
			}
			return nil
		},
	})
}

// Open opens (or creates) a SQLite database at path and runs migrations.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3_vec", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{sql: sqlDB}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the underlying *sql.DB for repos that need raw access.
func (d *DB) SQL() *sql.DB { return d.sql }
