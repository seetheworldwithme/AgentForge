package store

type KnowledgeBase struct {
	ID               string
	Name             string
	Description      string
	EmbedProviderID  string
	ChatProviderID   string
	RerankProviderID string
	IndexMode        string // chunk | qa；空视为 chunk
	ChunkSize        int
	ChunkOverlap     int
	DocCount         int
	CreatedAt        string
}

type Document struct {
	ID          string
	KBID        string
	Filename    string
	FileSize    int64
	MimeType    string
	Status      string // processing | ready | failed | duplicate
	ChunkCount  int
	Error       string
	RawPath     string
	ContentHash string
	ChunkDone   int // 已写入叶子数（进度分子 + 续传锚点）
	ChunkTotal  int // 叶子总数（进度分母）
	CreatedAt   string
}

type Chunk struct {
	ID         string
	DocID      string
	KBID       string
	Ordinal    int
	Text       string
	TokenCount int
	Metadata   string // JSON
	ParentID   string // 父块 ID（父子分块：子块指向父块；父块/顶层为空）
	Kind       string // content | qa | summary；空视为 content
}

func (d *DB) CreateKB(kb KnowledgeBase) error {
	_, err := d.sql.Exec(`INSERT INTO knowledge_bases
		(id,name,description,embed_provider_id,chat_provider_id,rerank_provider_id,index_mode,chunk_size,chunk_overlap,doc_count,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		kb.ID, kb.Name, nullable(kb.Description), nullable(kb.EmbedProviderID),
		nullable(kb.ChatProviderID), nullable(kb.RerankProviderID), nullable(kb.IndexMode),
		kb.ChunkSize, kb.ChunkOverlap, kb.DocCount, kb.CreatedAt)
	return err
}

func (d *DB) UpdateKB(kb KnowledgeBase) error {
	_, err := d.sql.Exec(`UPDATE knowledge_bases
		SET name=?, description=?, embed_provider_id=?, chat_provider_id=?, rerank_provider_id=?, index_mode=?, chunk_size=?, chunk_overlap=?
		WHERE id=?`,
		kb.Name, nullable(kb.Description), nullable(kb.EmbedProviderID),
		nullable(kb.ChatProviderID), nullable(kb.RerankProviderID), nullable(kb.IndexMode),
		kb.ChunkSize, kb.ChunkOverlap, kb.ID)
	return err
}

func (d *DB) GetKB(id string) (KnowledgeBase, error) {
	row := d.sql.QueryRow(`SELECT id,name,description,embed_provider_id,chat_provider_id,rerank_provider_id,index_mode,chunk_size,chunk_overlap,doc_count,created_at
		FROM knowledge_bases WHERE id=?`, id)
	var kb KnowledgeBase
	var desc, embedProv, chatProv, rerankProv, indexMode *string
	err := row.Scan(&kb.ID, &kb.Name, &desc, &embedProv, &chatProv, &rerankProv, &indexMode, &kb.ChunkSize, &kb.ChunkOverlap,
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
	if indexMode != nil {
		kb.IndexMode = *indexMode
	}
	return kb, err
}

func (d *DB) ListKBs() ([]KnowledgeBase, error) {
	rows, err := d.sql.Query(`SELECT id,name,description,embed_provider_id,chat_provider_id,rerank_provider_id,index_mode,chunk_size,chunk_overlap,doc_count,created_at
		FROM knowledge_bases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnowledgeBase
	for rows.Next() {
		var kb KnowledgeBase
		var desc, embedProv, chatProv, rerankProv, indexMode *string
		if err := rows.Scan(&kb.ID, &kb.Name, &desc, &embedProv, &chatProv, &rerankProv, &indexMode, &kb.ChunkSize, &kb.ChunkOverlap,
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
		if indexMode != nil {
			kb.IndexMode = *indexMode
		}
		out = append(out, kb)
	}
	return out, rows.Err()
}

func (d *DB) DeleteKB(id string) error {
	_, err := d.sql.Exec(`DELETE FROM knowledge_bases WHERE id=?`, id)
	return err
}

// KBStatusCount 是单个知识库的文档状态计数，供列表侧边栏直接展示聚合统计。
type KBStatusCount struct {
	Ready      int
	Processing int
	Failed     int
	Duplicate  int
}

// MapKBStatusCounts 一次聚合所有知识库的文档状态计数（ready/processing/failed/duplicate），
// 避免前端逐个拉文档列表才能得到统计。一次 GROUP BY 查询，返回 kbID -> 计数。
func (d *DB) MapKBStatusCounts() (map[string]KBStatusCount, error) {
	rows, err := d.sql.Query(`SELECT kb_id, status, COUNT(*) FROM documents GROUP BY kb_id, status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]KBStatusCount)
	for rows.Next() {
		var kbID, status string
		var n int
		if err := rows.Scan(&kbID, &status, &n); err != nil {
			return nil, err
		}
		c := out[kbID]
		switch status {
		case "ready":
			c.Ready = n
		case "processing":
			c.Processing = n
		case "failed":
			c.Failed = n
		case "duplicate":
			c.Duplicate = n
		}
		out[kbID] = c
	}
	return out, rows.Err()
}

func (d *DB) CreateDocument(doc Document) error {
	_, err := d.sql.Exec(`INSERT INTO documents
		(id,kb_id,filename,file_size,mime_type,status,chunk_count,error,raw_path,content_hash,chunk_done,chunk_total,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		doc.ID, doc.KBID, doc.Filename, doc.FileSize, nullable(doc.MimeType),
		doc.Status, doc.ChunkCount, nullable(doc.Error), nullable(doc.RawPath),
		nullable(doc.ContentHash), doc.ChunkDone, doc.ChunkTotal, doc.CreatedAt)
	if err == nil {
		_ = d.refreshDocCount(doc.KBID)
	}
	return err
}

func (d *DB) GetDocument(id string) (Document, error) {
	row := d.sql.QueryRow(`SELECT id,kb_id,filename,file_size,mime_type,status,chunk_count,error,raw_path,content_hash,chunk_done,chunk_total,created_at
		FROM documents WHERE id=?`, id)
	return scanDocument(row)
}

func (d *DB) ListDocuments(kbID string) ([]Document, error) {
	rows, err := d.sql.Query(`SELECT id,kb_id,filename,file_size,mime_type,status,chunk_count,error,raw_path,content_hash,chunk_done,chunk_total,created_at
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

// SetDocumentHash 记录文档内容哈希（用于入库去重）。
func (d *DB) SetDocumentHash(id, hash string) error {
	_, err := d.sql.Exec(`UPDATE documents SET content_hash=? WHERE id=?`, nullable(hash), id)
	return err
}

// FindDuplicateDoc 返回同 KB 下 content_hash 相同且已就绪（ready）的文档 ID（排除
// 自身）；无重复返回空串。用于入库前跳过重复文档。
func (d *DB) FindDuplicateDoc(kbID, hash, excludeID string) (string, error) {
	rows, err := d.sql.Query(
		`SELECT id FROM documents WHERE kb_id=? AND content_hash=? AND status='ready' AND id<>? LIMIT 1`,
		kbID, hash, excludeID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var id string
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
	}
	return id, rows.Err()
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
	var mime, errMsg, rawPath, contentHash *string
	err := s.Scan(&doc.ID, &doc.KBID, &doc.Filename, &doc.FileSize, &mime,
		&doc.Status, &doc.ChunkCount, &errMsg, &rawPath, &contentHash,
		&doc.ChunkDone, &doc.ChunkTotal, &doc.CreatedAt)
	if mime != nil {
		doc.MimeType = *mime
	}
	if errMsg != nil {
		doc.Error = *errMsg
	}
	if rawPath != nil {
		doc.RawPath = *rawPath
	}
	if contentHash != nil {
		doc.ContentHash = *contentHash
	}
	return doc, err
}

// SetDocumentProgress 更新入库进度（已写入叶子数 + 总数），供进度条与断点续传用。
func (d *DB) SetDocumentProgress(id string, done, total int) error {
	_, err := d.sql.Exec(`UPDATE documents SET chunk_done=?, chunk_total=? WHERE id=?`, done, total, id)
	return err
}

// ListProcessingDocuments 返回所有处于 processing 的文档（core 启动时恢复入库用）。
func (d *DB) ListProcessingDocuments() ([]Document, error) {
	rows, err := d.sql.Query(`SELECT id,kb_id,filename,file_size,mime_type,status,chunk_count,error,raw_path,content_hash,chunk_done,chunk_total,created_at
		FROM documents WHERE status='processing'`)
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

func (d *DB) CreateChunk(c Chunk) error {
	_, err := d.sql.Exec(`INSERT INTO chunks
		(id,doc_id,kb_id,ordinal,text,token_count,metadata,parent_id,kind)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		c.ID, c.DocID, c.KBID, c.Ordinal, c.Text, c.TokenCount, nullable(c.Metadata), nullable(c.ParentID), nullable(c.Kind))
	return err
}

func (d *DB) ListChunksByDoc(docID string) ([]Chunk, error) {
	rows, err := d.sql.Query(`SELECT id,doc_id,kb_id,ordinal,text,token_count,metadata,parent_id,kind
		FROM chunks WHERE doc_id=? ORDER BY ordinal`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var meta, parentID, kind *string
		if err := rows.Scan(&c.ID, &c.DocID, &c.KBID, &c.Ordinal, &c.Text,
			&c.TokenCount, &meta, &parentID, &kind); err != nil {
			return nil, err
		}
		if meta != nil {
			c.Metadata = *meta
		}
		if parentID != nil {
			c.ParentID = *parentID
		}
		if kind != nil {
			c.Kind = *kind
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) ListChunksByKB(kbID string, limit int) ([]Chunk, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.sql.Query(`SELECT id,doc_id,kb_id,ordinal,text,token_count,metadata,parent_id,kind
		FROM chunks WHERE kb_id=? ORDER BY doc_id, ordinal LIMIT ?`, kbID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var meta, parentID, kind *string
		if err := rows.Scan(&c.ID, &c.DocID, &c.KBID, &c.Ordinal, &c.Text,
			&c.TokenCount, &meta, &parentID, &kind); err != nil {
			return nil, err
		}
		if meta != nil {
			c.Metadata = *meta
		}
		if parentID != nil {
			c.ParentID = *parentID
		}
		if kind != nil {
			c.Kind = *kind
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
	q := `SELECT id,doc_id,kb_id,ordinal,text,token_count,metadata,parent_id,kind FROM chunks WHERE id IN (` + placeholders(len(ids)) + `)`
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
		var meta, parentID, kind *string
		if err := rows.Scan(&c.ID, &c.DocID, &c.KBID, &c.Ordinal, &c.Text,
			&c.TokenCount, &meta, &parentID, &kind); err != nil {
			return nil, err
		}
		if meta != nil {
			c.Metadata = *meta
		}
		if parentID != nil {
			c.ParentID = *parentID
		}
		if kind != nil {
			c.Kind = *kind
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
