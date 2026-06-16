// internal/rag/store/schema.go
package store

import "fmt"

func Schema(dim int) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS knowledge_bases (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, embedding_model TEXT,
    chunk_size INTEGER DEFAULT 512, overlap INTEGER DEFAULT 50,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS documents (
    id TEXT PRIMARY KEY, kb_id TEXT NOT NULL REFERENCES knowledge_bases(id),
    file_path TEXT NOT NULL, file_type TEXT, chunk_count INTEGER,
    status TEXT, error_msg TEXT, content_hash TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(
    embedding float[%d]
);
CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY, doc_id TEXT NOT NULL REFERENCES documents(id),
    kb_id TEXT NOT NULL, content TEXT NOT NULL, heading_path TEXT,
    source TEXT, token_count INTEGER, seq INTEGER
);
CREATE INDEX IF NOT EXISTS idx_chunks_kb ON chunks(kb_id);
CREATE TABLE IF NOT EXISTS eval_questions (
    id INTEGER PRIMARY KEY, kb_id TEXT NOT NULL, question TEXT NOT NULL,
    source TEXT, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS eval_expected (
    question_id INTEGER NOT NULL, chunk_id INTEGER NOT NULL,
    PRIMARY KEY (question_id, chunk_id)
);
CREATE TABLE IF NOT EXISTS eval_runs (
    id INTEGER PRIMARY KEY, kb_id TEXT NOT NULL, params_json TEXT,
    recall_at_k REAL, mrr REAL, precision_at_k REAL, question_count INTEGER,
    run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`, dim)
}
