package store

import "database/sql"

type Provider struct {
	ID            string
	Name          string
	BaseURL       string
	APIKey        string
	ChatModel     string
	EmbedModel    string
	Kind          string // 'chat' | 'embed' | 'rerank'；空串视为 chat（向后兼容）
	Vision        bool   // 视觉(VL)模型：允许粘贴图片
	ContextWindow int    // 上下文窗口大小 tokens，0=未知用全局默认
	IsDefault     bool
	CreatedAt     string
	UpdatedAt     string
}

func (d *DB) CreateProvider(p Provider) error {
	_, err := d.sql.Exec(`INSERT INTO providers
		(id,name,base_url,api_key,chat_model,embed_model,kind,vision,context_window,is_default,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.BaseURL, p.APIKey, p.ChatModel, nullable(p.EmbedModel), nullable(p.Kind),
		boolToText(p.Vision), p.ContextWindow, boolToInt(p.IsDefault), p.CreatedAt, p.UpdatedAt)
	return err
}

func (d *DB) GetProvider(id string) (Provider, error) {
	row := d.sql.QueryRow(`SELECT id,name,base_url,api_key,chat_model,embed_model,kind,vision,context_window,is_default,created_at,updated_at
		FROM providers WHERE id=?`, id)
	return scanProvider(row)
}

func (d *DB) ListProviders() ([]Provider, error) {
	rows, err := d.sql.Query(`SELECT id,name,base_url,api_key,chat_model,embed_model,kind,vision,context_window,is_default,created_at,updated_at
		FROM providers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// normalizeKind 把 kind 归一为 "chat"、"embed" 或 "rerank"。空串（以及存储后的
// NULL）视为 chat，向后兼容 kind 列出现之前的老数据。
func normalizeKind(kind string) string {
	switch kind {
	case "embed":
		return "embed"
	case "rerank":
		return "rerank"
	default:
		return "chat"
	}
}

// GetDefaultProviderByKind 返回指定类别中标记为默认（is_default=1）的
// provider。chat 匹配 kind 为 NULL/”/'chat' 的行；embed 匹配 kind='embed'；
// rerank 匹配 kind='rerank'。没有默认时返回 sql.ErrNoRows。
func (d *DB) GetDefaultProviderByKind(kind string) (Provider, error) {
	q := `SELECT id,name,base_url,api_key,chat_model,embed_model,kind,vision,context_window,is_default,created_at,updated_at
		FROM providers WHERE is_default=1 `
	switch normalizeKind(kind) {
	case "embed":
		q += `AND kind='embed' `
	case "rerank":
		q += `AND kind='rerank' `
	default:
		q += `AND (kind IS NULL OR kind='' OR kind='chat') `
	}
	q += `LIMIT 1`
	row := d.sql.QueryRow(q)
	return scanProvider(row)
}

// ClearDefaultByKind 清除指定类别下所有 provider 的默认标记，确保设置新
// 默认后每个类别至多保留一个默认模型。
func (d *DB) ClearDefaultByKind(kind string) error {
	var q string
	switch normalizeKind(kind) {
	case "embed":
		q = `UPDATE providers SET is_default=0 WHERE kind='embed'`
	case "rerank":
		q = `UPDATE providers SET is_default=0 WHERE kind='rerank'`
	default:
		q = `UPDATE providers SET is_default=0 WHERE kind IS NULL OR kind='' OR kind='chat'`
	}
	_, err := d.sql.Exec(q)
	return err
}

// DeleteProvider removes a provider and clears any references to it. Sessions
// and knowledge_bases that pointed at it keep their rows but lose the link
// (provider_id / embed_provider_id set to NULL), so the foreign-key constraint
// never blocks deletion.
func (d *DB) DeleteProvider(id string) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE sessions SET provider_id=NULL WHERE provider_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE knowledge_bases SET embed_provider_id=NULL WHERE embed_provider_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE knowledge_bases SET chat_provider_id=NULL WHERE chat_provider_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE knowledge_bases SET rerank_provider_id=NULL WHERE rerank_provider_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM providers WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProvider(s scanner) (Provider, error) {
	var p Provider
	var embedModel *string
	var kind *string
	var vision *string
	var contextWindow sql.NullInt64
	var isDefault int
	err := s.Scan(&p.ID, &p.Name, &p.BaseURL, &p.APIKey, &p.ChatModel,
		&embedModel, &kind, &vision, &contextWindow, &isDefault, &p.CreatedAt, &p.UpdatedAt)
	if embedModel != nil {
		p.EmbedModel = *embedModel
	}
	if kind != nil {
		p.Kind = *kind
	}
	p.Vision = vision != nil && *vision == "1"
	// context_window 列允许 NULL/无效（老数据迁移后默认 NULL），此时置 0
	if contextWindow.Valid {
		p.ContextWindow = int(contextWindow.Int64)
	} else {
		p.ContextWindow = 0
	}
	p.IsDefault = isDefault != 0
	return p, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// boolToText 把布尔序列化为可空文本：true → "1"，false → nil（NULL）。
// 用于 vision 等「是/否」语义的 TEXT 列，与 scanProvider 的 *string 解析配套。
func boolToText(b bool) any {
	if b {
		return "1"
	}
	return nil
}
