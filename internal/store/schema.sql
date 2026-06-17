-- schema version tracking
PRAGMA user_version = 1;

CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    base_url    TEXT NOT NULL,
    api_key     TEXT NOT NULL,
    chat_model  TEXT NOT NULL,
    embed_model TEXT,
    is_default  INTEGER DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL DEFAULT '新对话',
    provider_id   TEXT REFERENCES providers(id),
    kb_id         TEXT,
    tools_enabled INTEGER DEFAULT 1,
    workdir       TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    content      TEXT,
    tool_calls   TEXT,
    tool_call_id TEXT,
    citations    TEXT,
    tokens_in    INTEGER,
    tokens_out   INTEGER,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS knowledge_bases (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    description       TEXT,
    embed_provider_id TEXT REFERENCES providers(id),
    chunk_size        INTEGER DEFAULT 800,
    chunk_overlap     INTEGER DEFAULT 100,
    doc_count         INTEGER DEFAULT 0,
    created_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS documents (
    id          TEXT PRIMARY KEY,
    kb_id       TEXT NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    file_size   INTEGER,
    mime_type   TEXT,
    status      TEXT NOT NULL,
    chunk_count INTEGER DEFAULT 0,
    error       TEXT,
    raw_path    TEXT,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_documents_kb ON documents(kb_id);

CREATE TABLE IF NOT EXISTS chunks (
    id          TEXT PRIMARY KEY,
    doc_id      TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    kb_id       TEXT NOT NULL,
    ordinal     INTEGER NOT NULL,
    text        TEXT NOT NULL,
    token_count INTEGER,
    metadata    TEXT
);
CREATE INDEX IF NOT EXISTS idx_chunks_doc ON chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_chunks_kb ON chunks(kb_id);

CREATE TABLE IF NOT EXISTS tool_allowlist (
    id         TEXT PRIMARY KEY,
    scope      TEXT NOT NULL,
    tool       TEXT NOT NULL,
    pattern    TEXT NOT NULL,
    created_at TEXT NOT NULL
);
