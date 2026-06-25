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
	Citations  string `json:"citations"`          // JSON
	Thinking   string `json:"thinking,omitempty"` // 推理过程（reasoning_content），仅展示，不回传模型
	Images     string `json:"images,omitempty"`   // 用户消息图片 dataURL JSON 数组（多模态）
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

// DeleteMessagesFrom 删除指定会话中 created_at 严格大于 since 的所有消息。
// 用于「重新回答」：截断某条 user 消息之后的旧 assistant / tool 回答，
// 让 agent 基于保留的历史重新生成，避免旧回答残留在上下文里。
func (d *DB) DeleteMessagesFrom(sessionID, since string) error {
	_, err := d.sql.Exec(
		`DELETE FROM messages WHERE session_id=? AND created_at > ?`,
		sessionID, since)
	return err
}

// UpdateMessageContent 改写一条消息的正文与图片（「编辑重发」使用）。
// 仅按 id 定位，刻意不改 created_at——否则会破坏 DeleteMessagesFrom 按时间截断的语义。
func (d *DB) UpdateMessageContent(sessionID, msgID, content, imagesJSON string) error {
	_, err := d.sql.Exec(
		`UPDATE messages SET content=?, images=? WHERE id=? AND session_id=?`,
		nullable(content), nullable(imagesJSON), msgID, sessionID)
	return err
}

// RenameSession updates a session's title and its updated_at timestamp.
func (d *DB) RenameSession(id, title, now string) error {
	_, err := d.sql.Exec(`UPDATE sessions SET title=?, updated_at=? WHERE id=?`, title, now, id)
	return err
}

// CompactHistoryPrefix 把某条消息（created_at < beforeCreatedAt）之前的较早历史
// 替换为单条摘要：先删头，再插摘要。在一个事务内完成，任一步失败即回滚。
// 用于「上下文压缩」：截断冗长的前置对话，下次加载历史变短，腾出 token 预算。
// 调用方应把 summary.CreatedAt 设为 beforeCreatedAt 前一刻（如 -1ns），
// 以保证摘要排在保留的 tail 之前，维持 ListMessages 的时间顺序。
func (d *DB) CompactHistoryPrefix(sessionID, beforeCreatedAt string, summary Message) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM messages WHERE session_id=? AND created_at < ?`,
		sessionID, beforeCreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO messages
		(id,session_id,role,content,tool_calls,tool_call_id,citations,thinking,images,tokens_in,tokens_out,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		summary.ID, summary.SessionID, summary.Role, nullable(summary.Content), nullable(summary.ToolCalls),
		nullable(summary.ToolCallID), nullable(summary.Citations), nullable(summary.Thinking), nullable(summary.Images),
		summary.TokensIn, summary.TokensOut, summary.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) AppendMessage(m Message) error {
	_, err := d.sql.Exec(`INSERT INTO messages
		(id,session_id,role,content,tool_calls,tool_call_id,citations,thinking,images,tokens_in,tokens_out,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.SessionID, m.Role, nullable(m.Content), nullable(m.ToolCalls),
		nullable(m.ToolCallID), nullable(m.Citations), nullable(m.Thinking), nullable(m.Images),
		m.TokensIn, m.TokensOut, m.CreatedAt)
	return err
}

func (d *DB) ListMessages(sessionID string) ([]Message, error) {
	rows, err := d.sql.Query(`SELECT id,session_id,role,content,tool_calls,tool_call_id,citations,thinking,images,tokens_in,tokens_out,created_at
		FROM messages WHERE session_id=? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var content, tc, tcid, cit, thinking, images *string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &content, &tc, &tcid, &cit,
			&thinking, &images, &m.TokensIn, &m.TokensOut, &m.CreatedAt); err != nil {
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
		if thinking != nil {
			m.Thinking = *thinking
		}
		if images != nil {
			m.Images = *images
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
