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
	// sqlite-vec ships the extension as vec0.<ext> (the vec0 module).
	name := map[string]string{
		"windows": "vec0.dll",
		"darwin":  "vec0.dylib",
		"linux":   "vec0.so",
	}[runtime.GOOS]
	if name == "" {
		return ""
	}
	candidates := []string{}
	// search relative to the working dir, walking up to 6 levels (finds the
	// repo-root ext/ whether run from the repo root or from a package subdir
	// during `go test`).
	candidates = append(candidates, filepath.Join("ext", runtime.GOOS, name))
	for dir, i := "..", 1; i <= 6; i++ {
		candidates = append(candidates, filepath.Join(dir, "ext", runtime.GOOS, name))
		dir = filepath.Join(dir, "..")
	}
	// then next to the executable (works for packaged builds)
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "ext", runtime.GOOS, name))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
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
