// internal/rag/store/store.go
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
	// blank-import：注册 sqlite-vec 扩展，使 vec_version()/vec0 可用。
	// 缺失则 SELECT vec_version() 报 "no such function: vec_version"。
	_ "modernc.org/sqlite/vec"
)

type Store struct {
	db   *sql.DB
	dim  int
	path string
}

func New(dbPath string, dim int) (*Store, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("dim must be positive, got %d", dim)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}

	var ver string
	if err := db.QueryRow("SELECT vec_version()").Scan(&ver); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite-vec not available (modernc version may need upgrade): %w", err)
	}

	if _, err := db.Exec(Schema(dim)); err != nil {
		db.Close()
		return nil, fmt.Errorf("exec schema: %w", err)
	}

	return &Store{db: db, dim: dim, path: dbPath}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Dim() int     { return s.dim }
