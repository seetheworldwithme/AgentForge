// internal/rag/store/chunks.go
package store

import (
	"errors"
	"fmt"

	"github.com/agentforge/agentforge/internal/rag"
)

// SaveChunks 批量写入 chunk 元数据 + 向量，一个事务。
func (s *Store) SaveChunks(chunks []rag.Chunk, vectors [][]float32) ([]int64, error) {
	if len(chunks) != len(vectors) {
		return nil, errors.New("chunks and vectors length mismatch")
	}
	for i, v := range vectors {
		if len(v) != s.dim {
			return nil, fmt.Errorf("vector[%d] dim %d != store dim %d", i, len(v), s.dim)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	ids := make([]int64, 0, len(chunks))
	chunkInsert, err := tx.Prepare(`INSERT INTO chunks(doc_id,kb_id,content,heading_path,source,token_count,seq) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare chunk insert: %w", err)
	}
	defer chunkInsert.Close()

	vecInsert, err := tx.Prepare(`INSERT INTO vec_chunks(rowid, embedding) VALUES (?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare vec insert: %w", err)
	}
	defer vecInsert.Close()

	for i, c := range chunks {
		res, err := chunkInsert.Exec(c.DocID, c.KBID, c.Content, c.HeadingPath, c.Source, c.TokenCount, c.Seq)
		if err != nil {
			return nil, fmt.Errorf("insert chunk[%d]: %w", i, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("last insert id[%d]: %w", i, err)
		}
		ids = append(ids, id)

		// 用 chunk 的 rowid 作为 vec_chunks 的显式 rowid，保证两表按 id 关联，
		// 供 T17 Search 的 JOIN chunks c ON c.id = v.rowid 成立。
		// 否则 vec_chunks 会用独立自增 rowid，与 chunks.id 错位。
		if _, err := vecInsert.Exec(id, vecToBlob(vectors[i])); err != nil {
			return nil, fmt.Errorf("insert vec[%d]: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return ids, nil
}
