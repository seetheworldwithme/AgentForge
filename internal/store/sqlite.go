package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
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
				log.Printf("ERROR: failed to load sqlite-vec extension from %s: %v", p, err)
				return fmt.Errorf("load sqlite-vec: %w", err)
			}
			return nil
		},
	})
}

// Open opens (or creates) a SQLite database at path and runs migrations.
func Open(path string) (*DB, error) {
	if vecExtPath() == "" {
		log.Printf("WARN: sqlite-vec extension (vec0) not found under ext/%s/ — "+
			"vector/RAG features are disabled and documents will fail to index. "+
			"Place vec0.<ext> there (see Makefile).", runtime.GOOS)
	}
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
	db := &DB{sql: sqlDB}
	if err := db.applyMigrations(); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return db, nil
}

// applyMigrations runs additive, idempotent schema patches for databases that
// predate a column. Fresh databases already get these columns from schema.sql,
// so each step guards itself with a column-existence check.
func (d *DB) applyMigrations() error {
	if err := d.ensureColumn("sessions", "workdir"); err != nil {
		return err
	}
	if err := d.ensureColumn("documents", "raw_path"); err != nil {
		return err
	}
	if err := d.ensureColumn("providers", "vision_model"); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds col to table if it does not already exist.
func (d *DB) ensureColumn(table, col string) error {
	rows, err := d.sql.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.sql.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s TEXT", table, col))
	return err
}

func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the underlying *sql.DB for repos that need raw access.
func (d *DB) SQL() *sql.DB { return d.sql }
