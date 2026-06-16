// internal/rag/store/crud.go
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/agentforge/agentforge/internal/rag"
)

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateKnowledgeBase(name, embeddingModel string, chunkSize, overlap int) (string, error) {
	id := newID()
	_, err := s.db.Exec(`INSERT INTO knowledge_bases(id,name,embedding_model,chunk_size,overlap) VALUES(?,?,?,?,?)`,
		id, name, embeddingModel, chunkSize, overlap)
	if err != nil {
		return "", fmt.Errorf("create kb: %w", err)
	}
	return id, nil
}

func (s *Store) GetKnowledgeBase(id string) (*rag.KnowledgeBase, error) {
	var kb rag.KnowledgeBase
	var created string
	var embeddingModel sql.NullString
	err := s.db.QueryRow(`SELECT id,name,embedding_model,chunk_size,overlap,created_at FROM knowledge_bases WHERE id=?`, id).
		Scan(&kb.ID, &kb.Name, &embeddingModel, &kb.ChunkSize, &kb.Overlap, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kb.EmbeddingModel = embeddingModel.String
	kb.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
	return &kb, nil
}

func (s *Store) ListKnowledgeBases() ([]rag.KnowledgeBase, error) {
	rows, err := s.db.Query(`SELECT id,name,embedding_model,chunk_size,overlap,created_at FROM knowledge_bases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rag.KnowledgeBase
	for rows.Next() {
		var kb rag.KnowledgeBase
		var created string
		var embeddingModel sql.NullString
		if err := rows.Scan(&kb.ID, &kb.Name, &embeddingModel, &kb.ChunkSize, &kb.Overlap, &created); err != nil {
			return nil, err
		}
		kb.EmbeddingModel = embeddingModel.String
		kb.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		out = append(out, kb)
	}
	return out, rows.Err()
}

func (s *Store) CreateDocument(kbID, filePath, fileType, contentHash string) (string, error) {
	id := newID()
	_, err := s.db.Exec(`INSERT INTO documents(id,kb_id,file_path,file_type,status,content_hash) VALUES(?,?,?,?,?,?)`,
		id, kbID, filePath, fileType, "pending", contentHash)
	if err != nil {
		return "", fmt.Errorf("create doc: %w", err)
	}
	return id, nil
}

// scanDocument 把一行 documents 记录 Scan 到 rag.Document。
// schema 中 file_type/status/error_msg/content_hash/chunk_count 未标 NOT NULL，
// 必须用 NullString/NullInt64，否则 NULL 行 Scan 失败。
func scanDocument(d *rag.Document, scan func(...any) error) error {
	var created, fileType, status, errorMsg, contentHash sql.NullString
	var chunkCount sql.NullInt64
	if err := scan(&d.ID, &d.KBID, &d.FilePath, &fileType, &chunkCount, &status, &errorMsg, &contentHash, &created); err != nil {
		return err
	}
	d.FileType = fileType.String
	d.ChunkCount = int(chunkCount.Int64)
	d.Status = status.String
	d.ErrorMsg = errorMsg.String
	d.ContentHash = contentHash.String
	d.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created.String)
	return nil
}

func (s *Store) GetDocument(id string) (*rag.Document, error) {
	var d rag.Document
	row := s.db.QueryRow(`SELECT id,kb_id,file_path,file_type,chunk_count,status,error_msg,content_hash,created_at FROM documents WHERE id=?`, id)
	if err := scanDocument(&d, row.Scan); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (s *Store) GetDocumentByHash(hash string) (*rag.Document, error) {
	var d rag.Document
	row := s.db.QueryRow(`SELECT id,kb_id,file_path,file_type,chunk_count,status,error_msg,content_hash,created_at FROM documents WHERE content_hash=?`, hash)
	if err := scanDocument(&d, row.Scan); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (s *Store) UpdateDocumentStatus(docID, status string, chunkCount int, errMsg string) error {
	_, err := s.db.Exec(`UPDATE documents SET status=?, chunk_count=?, error_msg=? WHERE id=?`, status, chunkCount, errMsg, docID)
	return err
}
