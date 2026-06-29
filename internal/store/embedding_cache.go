package store

import (
	"encoding/json"
	"log"
	"time"
)

// GetCachedEmbeddings 批量查询 embedding 缓存，返回命中的 hash → 向量映射（未命中
// 的不在 map 中）。model 为空或无 hash 时返回空 map。
func (d *DB) GetCachedEmbeddings(model string, hashes []string) (map[string][]float32, error) {
	out := make(map[string][]float32, len(hashes))
	if model == "" || len(hashes) == 0 {
		return out, nil
	}
	rows, err := d.sql.Query(
		`SELECT text_hash, embedding FROM embedding_cache WHERE model=? AND text_hash IN (`+
			placeholders(len(hashes))+`)`,
		toArgs(model, hashes)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h, emb string
		if err := rows.Scan(&h, &emb); err != nil {
			return nil, err
		}
		var vec []float32
		if err := json.Unmarshal([]byte(emb), &vec); err != nil {
			log.Printf("embedding cache: corrupt entry model=%s hash=%s: %v", model, h, err)
			continue
		}
		if len(vec) > 0 {
			out[h] = vec // 跳过空/损坏向量，避免空向量入库污染 vec0
		}
	}
	return out, rows.Err()
}

// PutCachedEmbedding 写入一条 embedding 缓存（主键冲突时忽略，保证幂等）。
func (d *DB) PutCachedEmbedding(model, hash string, vec []float32) error {
	if model == "" || hash == "" {
		return nil
	}
	blob, err := json.Marshal(vec)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(
		`INSERT OR IGNORE INTO embedding_cache(model, text_hash, embedding, created_at) VALUES(?,?,?,?)`,
		model, hash, string(blob), time.Now().UTC().Format(time.RFC3339))
	return err
}

func toArgs(model string, hashes []string) []any {
	args := make([]any, 0, len(hashes)+1)
	args = append(args, model)
	for _, h := range hashes {
		args = append(args, h)
	}
	return args
}
