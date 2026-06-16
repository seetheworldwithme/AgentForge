package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// VectorHit is one nearest-neighbor search result.
type VectorHit struct {
	ID       string
	Distance float32
}

// EnsureVecTable creates a vec0 virtual table with the given embedding dimension.
func (d *DB) EnsureVecTable(name string, dim int) error {
	_, err := d.sql.Exec(fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(
			embedding float[%d],
			chunk_id text primary key
		)`, name, dim))
	return err
}

// InsertVector stores one vector keyed by chunkID. vec0 accepts JSON arrays
// for the embedding column.
func (d *DB) InsertVector(table, chunkID string, vec []float32) error {
	blob, err := json.Marshal(vec)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(
		fmt.Sprintf(`INSERT INTO %s(chunk_id, embedding) VALUES(?, ?)`, table),
		chunkID, string(blob))
	return err
}

// SearchVectors returns the top-k nearest chunk IDs by distance.
func (d *DB) SearchVectors(table string, query []float32, k int) ([]VectorHit, error) {
	blob, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(
		`SELECT chunk_id, distance FROM %s
		 WHERE embedding MATCH ?
		 ORDER BY distance ASC LIMIT ?`, table)
	rows, err := d.sql.Query(q, string(blob), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []VectorHit
	for rows.Next() {
		var h VectorHit
		if err := rows.Scan(&h.ID, &h.Distance); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// DropVecTable removes a virtual table (used on KB deletion).
func (d *DB) DropVecTable(name string) error {
	_, err := d.sql.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, name))
	return err
}

// SanitizeTableName reduces a kb id to a safe identifier for a vec0 table name.
func SanitizeTableName(kbID string) string {
	return "vec_" + strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.ToLower(kbID))
}
