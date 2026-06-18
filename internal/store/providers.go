package store

type Provider struct {
	ID         string
	Name       string
	BaseURL    string
	APIKey     string
	ChatModel  string
	EmbedModel string
	IsDefault  bool
	CreatedAt  string
	UpdatedAt  string
}

func (d *DB) CreateProvider(p Provider) error {
	_, err := d.sql.Exec(`INSERT INTO providers
		(id,name,base_url,api_key,chat_model,embed_model,is_default,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.BaseURL, p.APIKey, p.ChatModel, nullable(p.EmbedModel),
		boolToInt(p.IsDefault), p.CreatedAt, p.UpdatedAt)
	return err
}

func (d *DB) GetProvider(id string) (Provider, error) {
	row := d.sql.QueryRow(`SELECT id,name,base_url,api_key,chat_model,embed_model,is_default,created_at,updated_at
		FROM providers WHERE id=?`, id)
	return scanProvider(row)
}

func (d *DB) ListProviders() ([]Provider, error) {
	rows, err := d.sql.Query(`SELECT id,name,base_url,api_key,chat_model,embed_model,is_default,created_at,updated_at
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

// GetDefaultProvider returns the provider flagged is_default=1, if any.
func (d *DB) GetDefaultProvider() (Provider, error) {
	row := d.sql.QueryRow(`SELECT id,name,base_url,api_key,chat_model,embed_model,is_default,created_at,updated_at
		FROM providers WHERE is_default=1 LIMIT 1`)
	return scanProvider(row)
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
	var isDefault int
	err := s.Scan(&p.ID, &p.Name, &p.BaseURL, &p.APIKey, &p.ChatModel,
		&embedModel, &isDefault, &p.CreatedAt, &p.UpdatedAt)
	if embedModel != nil {
		p.EmbedModel = *embedModel
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
