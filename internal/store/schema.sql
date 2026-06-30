-- schema version tracking
PRAGMA user_version = 1;

CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    base_url    TEXT NOT NULL,
    api_key     TEXT NOT NULL,
    chat_model  TEXT NOT NULL,
    embed_model TEXT,
    kind        TEXT, -- 'chat' | 'embed' | 'rerank'；NULL 视为 chat（向后兼容老数据）
    vision      TEXT, -- '1' = 视觉(VL)模型，允许粘贴图片；NULL/'' = 纯文本
    context_window INTEGER DEFAULT 0, -- 上下文窗口大小 tokens，0=未知用全局默认
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
    thinking     TEXT, -- 推理模型的思考过程（reasoning_content），仅用于展示，不回传给模型
    images       TEXT, -- 用户消息的图片 dataURL JSON 数组（多模态）
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
    chat_provider_id  TEXT REFERENCES providers(id),
    rerank_provider_id TEXT REFERENCES providers(id),
    index_mode        TEXT DEFAULT 'chunk', -- 'chunk'(父子分块) | 'qa'(问答对索引)
    chunk_size        INTEGER DEFAULT 500,
    chunk_overlap     INTEGER DEFAULT 60,
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
    content_hash TEXT,
    chunk_done  INTEGER DEFAULT 0,
    chunk_total INTEGER DEFAULT 0,
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
    metadata    TEXT,
    parent_id   TEXT,
    kind        TEXT -- 'content'(普通子块) | 'qa'(问答对) | 'summary'(摘要)；NULL/空视为 content
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

-- embedding 缓存：按 (model, text_hash) 复用向量，避免重复文档/重建索引的重复嵌入。
CREATE TABLE IF NOT EXISTS embedding_cache (
    model      TEXT NOT NULL,
    text_hash  TEXT NOT NULL,
    embedding  TEXT NOT NULL, -- JSON 数组，与 vec0 的存储格式一致
    created_at TEXT NOT NULL,
    PRIMARY KEY (model, text_hash)
);

-- 图片描述缓存：按 (model, image_hash) 复用 VLM 对图片的文字描述。图片描述阶段没有
-- leaf 进度，慢模型/大文档下整文档 ctx 超时后会从头重来；此缓存让已描述过的图片下次
-- 直接复用，使入库可跨次收敛（不再死循环）。image_hash 取压缩后图片字节的 sha256。
CREATE TABLE IF NOT EXISTS image_desc_cache (
    model      TEXT NOT NULL,
    image_hash TEXT NOT NULL,
    desc_text  TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (model, image_hash)
);
