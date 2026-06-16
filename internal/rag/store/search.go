// internal/rag/store/search.go
package store

import (
	"database/sql"
	"fmt"

	"github.com/agentforge/agentforge/internal/rag"
)

// Search 在指定知识库内按向量余弦相似度检索 top-K。
//
// 实现说明（设计权衡，已知局限）：
// sqlite-vec v0.1.x 的 KNN 子句只接受 MATCH + k，无法在向量检索阶段
// 直接按 kb_id 过滤。本方法采用「放大召回再裁剪」策略：先在全库取
// candidateK（= topK 的若干倍）个最近邻，外层按 kb_id 过滤后再截断到
// topK。这在绝大多数场景能保证返回目标 KB 的 top-K，但当目标 KB 的
// 向量在全库排不进 candidateK 时仍可能漏检（无法严格保证 per-KB top-K，
// 除非每 KB 独立 vec0 表）。
func (s *Store) Search(kbID string, queryVec []float32, topK int) ([]rag.ScoredChunk, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK must be positive, got %d", topK)
	}
	if len(queryVec) != s.dim {
		return nil, fmt.Errorf("query dim %d != store dim %d", len(queryVec), s.dim)
	}

	// 放大召回候选以降低「目标 KB 向量排不进全库 top-K」的漏检概率。
	candidateK := topK * 10
	if candidateK < 100 {
		candidateK = 100
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
		LIMIT ?
	`, vecToBlob(queryVec), candidateK, kbID, topK)
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
