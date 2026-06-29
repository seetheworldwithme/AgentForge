package store

import (
	"fmt"
	"strings"
)

// FTSHit 是一次 FTS5 关键词检索结果。BM25 是 SQLite bm25() 的原始值（越小越相关，
// 因为 FTS5 内部已乘 -1）。
type FTSHit struct {
	ChunkID string
	BM25    float64
}

// FTSTableName 返回某 KB 对应的 FTS5 虚拟表名（与 vec0 表一一对应，前缀 fts_）。
func FTSTableName(kbID string) string {
	return "fts_" + strings.TrimPrefix(SanitizeTableName(kbID), "vec_")
}

// EnsureFTSTable 创建带 trigram tokenizer 的 FTS5 虚拟表。trigram 按 unicode 字符
// 3-滑窗，对中文友好（CJK 1 字 = 1 字符），支持子串 phrase 匹配与 bm25 排序。
// 用普通 FTS5 表（非 contentless/external-content），支持直接 DELETE，简单可靠。
func (d *DB) EnsureFTSTable(name string) error {
	_, err := d.sql.Exec(fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
			chunk_id UNINDEXED,
			text,
			tokenize='trigram'
		)`, name))
	return err
}

// InsertFTS 写入一个 chunk 的文本到 FTS5 索引。
func (d *DB) InsertFTS(table, chunkID, text string) error {
	_, err := d.sql.Exec(fmt.Sprintf(
		`INSERT INTO %s(chunk_id, text) VALUES(?, ?)`, table), chunkID, text)
	return err
}

// DeleteFTSByChunkIDs 删除指定 chunk 的 FTS5 条目（普通 FTS5 表支持直接 DELETE）。
func (d *DB) DeleteFTSByChunkIDs(table string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := d.sql.Exec(fmt.Sprintf(
		`DELETE FROM %s WHERE chunk_id IN (`+placeholders(len(ids))+`)`, table), args...)
	return err
}

// SearchFTS 执行 trigram 关键词检索，返回按 bm25 升序（越相关越前）的 top-k。
// queryExpr 是已构造的 FTS5 表达式（双引号包裹的 phrase），由调用方负责转义内部
// 双引号；< 3 个 unicode 字符的 query 不会命中任何行，调用方应据此回退向量路。
func (d *DB) SearchFTS(table, queryExpr string, k int) ([]FTSHit, error) {
	q := fmt.Sprintf(
		`SELECT chunk_id, bm25(%s) FROM %s WHERE %s MATCH ? ORDER BY bm25(%s) LIMIT ?`,
		table, table, table, table)
	rows, err := d.sql.Query(q, queryExpr, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []FTSHit
	for rows.Next() {
		var h FTSHit
		if err := rows.Scan(&h.ChunkID, &h.BM25); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// DropFTSTable 删除 FTS5 虚拟表（删 KB 时清理）。
func (d *DB) DropFTSTable(name string) error {
	_, err := d.sql.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, name))
	return err
}
