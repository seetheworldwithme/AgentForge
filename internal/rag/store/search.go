// internal/rag/store/search.go
package store

import (
	"database/sql"
	"fmt"

	"github.com/agentforge/agentforge/internal/rag"
)

// Search 在指定知识库内按向量余弦距离检索 top-K。
func (s *Store) Search(kbID string, queryVec []float32, topK int) ([]rag.ScoredChunk, error) {
	if len(queryVec) != s.dim {
		return nil, fmt.Errorf("query dim %d != store dim %d", len(queryVec), s.dim)
	}

	rows, err := s.db.Query(`
		SELECT c.id, c.doc_id, c.kb_id, c.content, c.heading_path, c.source, c.token_count, c.seq,
		       (1.0 - v.distance) AS score
		FROM (
			SELECT rowid, distance
			FROM vec_chunks
			WHERE embedding MATCH ?
			  AND k = ?
		) v
		JOIN chunks c ON c.id = v.rowid
		WHERE c.kb_id = ?
		ORDER BY v.distance
	`, vecToBlob(queryVec), topK, kbID)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var out []rag.ScoredChunk
	for rows.Next() {
		var sc rag.ScoredChunk
		var docID, kbIDRead, content string
		var headingPath, source sql.NullString
		var tokenCount, seq int
		var id int64
		if err := rows.Scan(&id, &docID, &kbIDRead, &content, &headingPath, &source, &tokenCount, &seq, &sc.Score); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		sc.ID = id
		sc.DocID = docID
		sc.KBID = kbIDRead
		sc.Content = content
		sc.HeadingPath = headingPath.String
		sc.Source = source.String
		sc.TokenCount = tokenCount
		sc.Seq = seq
		out = append(out, sc)
	}
	return out, rows.Err()
}
