package store

import (
	"database/sql"
	"time"
)

// GetImageDesc 查询图片描述缓存。命中返回 (描述, true, nil)；未命中返回 ("", false, nil)。
// model 为空时视为未启用缓存，直接返回未命中（不查库）。
func (d *DB) GetImageDesc(model, hash string) (string, bool, error) {
	if model == "" || hash == "" {
		return "", false, nil
	}
	var desc string
	err := d.sql.QueryRow(
		`SELECT desc_text FROM image_desc_cache WHERE model=? AND image_hash=?`,
		model, hash).Scan(&desc)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return desc, true, nil
}

// PutImageDesc 写入一条图片描述缓存（主键冲突时忽略，保证幂等）。任一参数为空则不写。
func (d *DB) PutImageDesc(model, hash, desc string) error {
	if model == "" || hash == "" || desc == "" {
		return nil
	}
	_, err := d.sql.Exec(
		`INSERT OR IGNORE INTO image_desc_cache(model, image_hash, desc_text, created_at) VALUES(?,?,?,?)`,
		model, hash, desc, time.Now().UTC().Format(time.RFC3339))
	return err
}
