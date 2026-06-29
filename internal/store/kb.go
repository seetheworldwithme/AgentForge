package store

type KnowledgeBase struct {
	ID               string
	Name             string
	Description      string
	EmbedProviderID  string
	ChatProviderID   string
	RerankProviderID string
	ChunkSize        int
	ChunkOverlap     int
	DocCount         int
	CreatedAt        string
}

type Document struct {
	ID         string
	KBID       string
	Filename   string
	FileSize   int64
	MimeType   string
	Status     string // processing | ready | failed
	ChunkCount int
	Error      string
	RawPath    string
	CreatedAt  string
}

type Chunk struct {
	ID         string
	DocID      string
	KBID       string
	Ordinal    int
	Text       string
	TokenCount int
	Metadata   string // JSON
}

func (d *DB) CreateKB(kb KnowledgeBase) error {
	_, err := d.sql.Exec(`INSERT INTO knowledge_bases
		(id,name,description,embed_provider_id,chat_provider_id,rerank_provider_id,chunk_size,chunk_overlap,doc_count,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		kb.ID, kb.Name, nullable(kb.Description), nullable(kb.EmbedProviderID),
		nullable(kb.ChatProviderID), nullable(kb.RerankProviderID),
		kb.ChunkSize, kb.ChunkOverlap, kb.DocCount, kb.CreatedAt)
	return err
}

func (d *DB) UpdateKB(kb KnowledgeBase) error {
	_, err := d.sql.Exec(`UPDATE knowledge_bases
		SET name=?, description=?, embed_provider_id=?, chat_provider_id=?, rerank_provider_id=?, chunk_size=?, chunk_overlap=?
		WHERE id=?`,
		kb.Name, nullable(kb.Description), nullable(kb.EmbedProviderID),
		nullable(kb.ChatProviderID), nullable(kb.RerankProviderID),
		kb.ChunkSize, kb.ChunkOverlap, kb.ID)
	return err
}

func (d *DB) GetKB(id string) (KnowledgeBase, error) {
	row := d.sql.QueryRow(`SELECT id,name,description,embed_provider_id,chat_provider_id,rerank_provider_id,chunk_size,chunk_overlap,doc_count,created_at
		FROM knowledge_bases WHERE id=?`, id)
	var kb KnowledgeBase
	var desc, embedProv, chatProv, rerankProv *string
	err := row.Scan(&kb.ID, &kb.Name, &desc, &embedProv, &chatProv, &rerankProv, &kb.ChunkSize, &kb.ChunkOverlap,
		&kb.DocCount, &kb.CreatedAt)
	if desc != nil {
		kb.Description = *desc
	}
	if embedProv != nil {
		kb.EmbedProviderID = *embedProv
	}
	if chatProv != nil {
		kb.ChatProviderID = *chatProv
	}
	if rerankProv != nil {
		kb.RerankProviderID = *rerankProv
	}
	return kb, err
}

func (d *DB) ListKBs() ([]KnowledgeBase, error) {
	rows, err := d.sql.Query(`SELECT id,name,description,embed_provider_id,chat_provider_id,rerank_provider_id,chunk_size,chunk_overlap,doc_count,created_at
		FROM knowledge_bases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnowledgeBase
	for rows.Next() {
		var kb KnowledgeBase
		var desc, embedProv, chatProv, rerankProv *string
		if err := rows.Scan(&kb.ID, &kb.Name, &desc, &embedProv, &chatProv, &rerankProv, &kb.ChunkSize, &kb.ChunkOverlap,
			&kb.DocCount, &kb.CreatedAt); err != nil {
			return nil, err
		}
		if desc != nil {
			kb.Description = *desc
		}
		if embedProv != nil {
			kb.EmbedProviderID = *embedProv
		}
		if chatProv != nil {
			kb.ChatProviderID = *chatProv
		}
		if rerankProv != nil {
			kb.RerankProviderID = *rerankProv
		}
		out = append(out, kb)
	}
	return out, rows.Err()
}

func (d *DB) DeleteKB(id string) error {
	_, err := d.sql.Exec(`DELETE FROM knowledge_bases WHERE id=?`, id)
	return err
}

func (d *DB) CreateDocument(doc Document) error {
	_, err := d.sql.Exec(`INSERT INTO documents
		(id,kb_id,filename,file_size,mime_type,status,chunk_count,error,raw_path,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		doc.ID, doc.KBID, doc.Filename, doc.FileSize, nullable(doc.MimeType),
		doc.Status, doc.ChunkCount, nullable(doc.Error), nullable(doc.RawPath), doc.CreatedAt)
	if err == nil {
		_ = d.refreshDocCount(doc.KBID)
	}
	return err
}

func (d *DB) GetDocument(id string) (Document, error) {
	row := d.sql.QueryRow(`SELECT id,kb_id,filename,file_size,mime_type,status,chunk_count,error,raw_path,created_at
		FROM documents WHERE id=?`, id)
	return scanDocument(row)
}

func (d *DB) ListDocuments(kbID string) ([]Document, error) {
	rows, err := d.sql.Query(`SELECT id,kb_id,filename,file_size,mime_type,status,chunk_count,error,raw_path,created_at
		FROM documents WHERE kb_id=? ORDER BY created_at`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func (d *DB) SetDocumentStatus(id, status string, chunkCount int, errMsg string) error {
	_, err := d.sql.Exec(`UPDATE documents SET status=?, chunk_count=?, error=? WHERE id=?`,
		status, chunkCount, nullable(errMsg), id)
	return err
}

func (d *DB) DeleteDocument(id string) error {
	doc, _ := d.GetDocument(id)
	_, err := d.sql.Exec(`DELETE FROM documents WHERE id=?`, id)
	if err == nil && doc.KBID != "" {
		_ = d.refreshDocCount(doc.KBID)
	}
	return err
}

func (d *DB) DeleteChunksByDoc(docID string) error {
	_, err := d.sql.Exec(`DELETE FROM chunks WHERE doc_id=?`, docID)
	return err
}

func scanDocument(s scanner) (Document, error) {
	var doc Document
	var mime, errMsg, rawPath *string
	err := s.Scan(&doc.ID, &doc.KBID, &doc.Filename, &doc.FileSize, &mime,
		&doc.Status, &doc.ChunkCount, &errMsg, &rawPath, &doc.CreatedAt)
	if mime != nil {
		doc.MimeType = *mime
	}
	if errMsg != nil {
		doc.Error = *errMsg
	}
	if rawPath != nil {
		doc.RawPath = *rawPath
	}
	return doc, err
}

func (d *DB) CreateChunk(c Chunk) error {
	_, err := d.sql.Exec(`INSERT INTO chunks
		(id,doc_id,kb_id,ordinal,text,token_count,metadata)
		VALUES(?,?,?,?,?,?,?)`,
		c.ID, c.DocID, c.KBID, c.Ordinal, c.Text, c.TokenCount, nullable(c.Metadata))
	return err
}

func (d *DB) ListChunksByDoc(docID string) ([]Chunk, error) {
	rows, err := d.sql.Query(`SELECT id,doc_id,kb_id,ordinal,text,token_count,metadata
		FROM chunks WHERE doc_id=? ORDER BY ordinal`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var meta *string
		if err := rows.Scan(&c.ID, &c.DocID, &c.KBID, &c.Ordinal, &c.Text,
			&c.TokenCount, &meta); err != nil {
			return nil, err
		}
		if meta != nil {
			c.Metadata = *meta
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) ListChunksByKB(kbID string, limit int) ([]Chunk, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.sql.Query(`SELECT id,doc_id,kb_id,ordinal,text,token_count,metadata
		FROM chunks WHERE kb_id=? ORDER BY doc_id, ordinal LIMIT ?`, kbID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var meta *string
		if err := rows.Scan(&c.ID, &c.DocID, &c.KBID, &c.Ordinal, &c.Text,
			&c.TokenCount, &meta); err != nil {
			return nil, err
		}
		if meta != nil {
			c.Metadata = *meta
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChunksByIDs fetches chunk text + source for a list of chunk IDs (post-search join).
func (d *DB) GetChunksByIDs(ids []string) ([]Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id,doc_id,kb_id,ordinal,text,token_count,metadata FROM chunks WHERE id IN (` + placeholders(len(ids)) + `)`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var meta *string
		if err := rows.Scan(&c.ID, &c.DocID, &c.KBID, &c.Ordinal, &c.Text,
			&c.TokenCount, &meta); err != nil {
			return nil, err
		}
		if meta != nil {
			c.Metadata = *meta
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}

func (d *DB) refreshDocCount(kbID string) error {
	_, err := d.sql.Exec(`UPDATE knowledge_bases
		SET doc_count=(SELECT COUNT(*) FROM documents WHERE kb_id=?)
		WHERE id=?`, kbID, kbID)
	return err
}
