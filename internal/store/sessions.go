package store

type Session struct {
	ID           string
	Title        string
	ProviderID   string
	KBID         string
	ToolsEnabled int
	WorkDir      string
	CreatedAt    string
	UpdatedAt    string
}

type Message struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCalls  string `json:"tool_calls"` // JSON
	ToolCallID string `json:"tool_call_id"`
	Citations  string `json:"citations"` // JSON
	TokensIn   int    `json:"tokens_in"`
	TokensOut  int    `json:"tokens_out"`
	CreatedAt  string `json:"created_at"`
}

func (d *DB) CreateSession(s Session) error {
	_, err := d.sql.Exec(`INSERT INTO sessions(id,title,provider_id,kb_id,tools_enabled,workdir,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		s.ID, s.Title, nullable(s.ProviderID), nullable(s.KBID),
		s.ToolsEnabled, nullable(s.WorkDir), s.CreatedAt, s.UpdatedAt)
	return err
}

func (d *DB) GetSession(id string) (Session, error) {
	row := d.sql.QueryRow(`SELECT id,title,provider_id,kb_id,tools_enabled,workdir,created_at,updated_at
		FROM sessions WHERE id=?`, id)
	var s Session
	var prov, kb, wd *string
	err := row.Scan(&s.ID, &s.Title, &prov, &kb, &s.ToolsEnabled, &wd, &s.CreatedAt, &s.UpdatedAt)
	if prov != nil {
		s.ProviderID = *prov
	}
	if kb != nil {
		s.KBID = *kb
	}
	if wd != nil {
		s.WorkDir = *wd
	}
	return s, err
}

func (d *DB) ListSessions() ([]Session, error) {
	rows, err := d.sql.Query(`SELECT id,title,provider_id,kb_id,tools_enabled,workdir,created_at,updated_at
		FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		var prov, kb, wd *string
		if err := rows.Scan(&s.ID, &s.Title, &prov, &kb, &s.ToolsEnabled, &wd, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if prov != nil {
			s.ProviderID = *prov
		}
		if kb != nil {
			s.KBID = *kb
		}
		if wd != nil {
			s.WorkDir = *wd
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) DeleteSession(id string) error {
	_, err := d.sql.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

func (d *DB) UpdateSession(s Session) error {
	_, err := d.sql.Exec(`UPDATE sessions
		SET title=?, provider_id=?, kb_id=?, tools_enabled=?, updated_at=?
		WHERE id=?`,
		s.Title, nullable(s.ProviderID), nullable(s.KBID), s.ToolsEnabled,
		s.UpdatedAt, s.ID)
	return err
}

// RenameSession updates a session's title and its updated_at timestamp.
func (d *DB) RenameSession(id, title, now string) error {
	_, err := d.sql.Exec(`UPDATE sessions SET title=?, updated_at=? WHERE id=?`, title, now, id)
	return err
}

func (d *DB) AppendMessage(m Message) error {
	_, err := d.sql.Exec(`INSERT INTO messages
		(id,session_id,role,content,tool_calls,tool_call_id,citations,tokens_in,tokens_out,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.SessionID, m.Role, nullable(m.Content), nullable(m.ToolCalls),
		nullable(m.ToolCallID), nullable(m.Citations), m.TokensIn, m.TokensOut, m.CreatedAt)
	return err
}

func (d *DB) ListMessages(sessionID string) ([]Message, error) {
	rows, err := d.sql.Query(`SELECT id,session_id,role,content,tool_calls,tool_call_id,citations,tokens_in,tokens_out,created_at
		FROM messages WHERE session_id=? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var content, tc, tcid, cit *string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &content, &tc, &tcid, &cit,
			&m.TokensIn, &m.TokensOut, &m.CreatedAt); err != nil {
			return nil, err
		}
		if content != nil {
			m.Content = *content
		}
		if tc != nil {
			m.ToolCalls = *tc
		}
		if tcid != nil {
			m.ToolCallID = *tcid
		}
		if cit != nil {
			m.Citations = *cit
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
