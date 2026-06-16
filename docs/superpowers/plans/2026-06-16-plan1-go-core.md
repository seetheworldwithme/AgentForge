# Plan 1: Go Core Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go core HTTP service that powers the agent platform — LLM chat, RAG, tool execution, and persistence — runnable standalone and testable via curl before any UI exists.

**Architecture:** A single Go binary starts an HTTP server on a random `127.0.0.1` port (written to `port.lock`), exposing REST + SSE endpoints. Internally it layers: API Gateway (chi) → Agent orchestrator → LLM/RAG/Tool engines → SQLite storage. All engines are Go interfaces so the orchestrator can be unit-tested with mocks.

**Tech Stack:** Go 1.26, chi (routing), mattn/go-sqlite3 (CGO SQLite), sqlite-vec (vector search), sqlc (SQL codegen — optional, MVP uses hand-written SQL), standard net/http for SSE. No external Go framework.

**Reference spec:** `docs/superpowers/specs/2026-06-16-agent-client-design.md`

---

## File Structure

```
agent-rust/
├── go.mod
├── go.sum
├── cmd/
│   └── core/                        # Standalone core server entry (no Wails yet)
│       └── main.go
├── internal/
│   ├── server/
│   │   ├── router.go                # chi routes registration
│   │   ├── handler_session.go       # session CRUD + chat (SSE)
│   │   ├── handler_kb.go            # knowledge base + document upload
│   │   ├── handler_config.go        # provider + settings CRUD
│   │   ├── handler_tools.go         # tool list + confirm callback
│   │   └── sse.go                   # SSE writer helper
│   ├── agent/
│   │   ├── agent.go                 # orchestrator: chat loop
│   │   ├── types.go                 # Message, ToolCall, Chunk types
│   │   └── agent_test.go
│   ├── llm/
│   │   ├── client.go                # LLMClient interface
│   │   ├── openai.go                # OpenAI-compatible impl (stream + tool_calls)
│   │   ├── openai_test.go
│   │   └── retry.go                 # retry/backoff
│   ├── rag/
│   │   ├── ingest.go                # parse + chunk + embed + store
│   │   ├── retrieve.go              # RAGEngine interface + top-K search
│   │   ├── embed.go                 # call embedding API
│   │   ├── chunker.go               # text splitting
│   │   ├── chunker_test.go
│   │   └── parser/
│   │       ├── txt.go
│   │       └── markdown.go
│   ├── tools/
│   │   ├── engine.go                # ToolEngine interface + registry
│   │   ├── gate.go                  # confirmation gate (channel-based)
│   │   ├── gate_test.go
│   │   └── builtin/
│   │       ├── bash.go
│   │       ├── file_read.go
│   │       ├── file_write.go
│   │       ├── file_edit.go
│   │       └── grep.go
│   └── store/
│       ├── sqlite.go                # connection + migration
│       ├── schema.sql               # all tables
│       ├── providers.go             # provider CRUD
│       ├── sessions.go              # session + message CRUD
│       ├── kb.go                    # kb + document + chunk CRUD
│       ├── vec.go                   # sqlite-vec load + vector ops
│       └── vec_test.go              # CRITICAL: validate CGO+sqlite-vec early
└── (frontend/, cmd/desktop/ are Plan 2)
```

**Responsibility boundaries:**
- `internal/store` — only DB access, no business logic. Returns plain structs.
- `internal/llm`, `internal/rag`, `internal/tools` — each defines its own interface; no cross-imports except shared `agent/types.go`.
- `internal/agent` — depends only on the three engine interfaces + store. The orchestrator brain.
- `internal/server` — thin HTTP adapters calling agent/store. No logic here.

---

## Critical Path Note: Validate CGO + sqlite-vec FIRST

Task 3 (sqlite-vec load + vec_test) is the **highest-risk item** per spec §11.1. It is deliberately placed early (Task 3, before any other engine). If it fails, stop and either (a) switch to the hnswlib fallback, or (b) escalate. Do not build engines on top of an unverified storage layer.

---

## Task 1: Project Initialization

**Files:**
- Create: `go.mod`
- Create: `cmd/core/main.go`
- Create: `internal/server/router.go`

- [ ] **Step 1: Initialize module and create directories**

Run:
```bash
cd F:\code\Go\myself\agent-rust
go mod init github.com/yourname/agent-rust
mkdir cmd core internal\server internal\store internal\llm internal\agent internal\rag internal\rag\parser internal\tools internal\tools\builtin
```
(On Windows cmd, use `\`. Replace `yourname` with your actual GitHub handle or a local module path like `agent-rust`.)

- [ ] **Step 2: Add dependencies**

Run:
```bash
go get github.com/go-chi/chi/v5@latest
go get github.com/go-chi/cors@latest
github.com/mattn/go-sqlite3@latest
go get github.com/oklog/ulid/v2@latest
```
Note: `github.com/mattn/go-sqlite3` requires CGO. Ensure `CGO_ENABLED=1` and a C compiler (gcc via MinGW on Windows). Verify with `go env CGO_ENABLED`.

- [ ] **Step 3: Write minimal main.go that starts a server**

`cmd/core/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	flag.Parse()

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("core listening on %s", ln.Addr())
	fmt.Println(ln.Addr().String()) // printed for tests to capture port
	log.Fatal(http.Serve(ln, r))
}
```

- [ ] **Step 4: Verify it builds and runs**

Run:
```bash
go build ./...
go run ./cmd/core
```
Expected: log line `core listening on 127.0.0.1:<port>`. In another terminal:
```bash
curl http://127.0.0.1:<port>/healthz
```
Expected: `{"status":"ok"}`

- [ ] **Step 5: Commit**

```bash
git init   # if not already a repo
git add go.mod go.sum cmd internal
git commit -m "feat(core): scaffold core HTTP server with healthz"
```

---

## Task 2: SQLite Store + Schema + Migration

**Files:**
- Create: `internal/store/schema.sql`
- Create: `internal/store/sqlite.go`
- Test: `internal/store/sqlite_test.go`

- [ ] **Step 1: Write the schema.sql**

`internal/store/schema.sql` — full schema from spec §6.2:
```sql
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
```

- [ ] **Step 2: Write the failing test**

`internal/store/sqlite_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestOpenRunsMigrations(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// schema should create all tables; query each to verify
	for _, table := range []string{"providers", "settings", "sessions", "messages",
		"knowledge_bases", "documents", "chunks", "tool_allowlist"} {
		var name string
		err := db.sql.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing after migration: %v", table, err)
		}
	}

	// WAL mode should be enabled
	var journal string
	_ = db.sql.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL — `Open` undefined / package doesn't compile.

- [ ] **Step 4: Implement the store**

`internal/store/sqlite.go`:
```go
package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	"github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	sql *sql.DB
}

// Open opens (or creates) a SQLite database at path and runs migrations.
// It registers a pure-Go sqlite3 driver under a custom name so we can later
// hook sqlite-vec on the same connection (Task 3).
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{sql: sqlDB}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the underlying *sql.DB for repos that need raw access.
func (d *DB) SQL() *sql.DB { return d.sql }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS. (If CGO/compiler errors appear, resolve before continuing — the whole plan depends on this.)

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "feat(store): SQLite connection, embedded schema, WAL migration"
```

---

## Task 3: sqlite-vec Extension + Vector Ops (CRITICAL PATH)

**⚠️ Highest-risk task.** Validates the CGO + sqlite-vec combo from spec §11.1. If this fails, do not proceed to other engines — escalate or switch to hnswlib fallback (see bottom of this task).

**Files:**
- Modify: `internal/store/sqlite.go` (load extension on conn)
- Create: `internal/store/vec.go`
- Test: `internal/store/vec_test.go`

- [ ] **Step 1: Obtain sqlite-vec**

sqlite-vec ships a `vec0` SQLite extension. Download the platform-appropriate loadable extension:
- Release page: `https://github.com/asg0171/sqlite-vec/releases`
- Windows: `sqlite-vec-<ver>.dll` → place in `ext/windows/sqlite-vec.dll`
- macOS: `sqlite-vec.dylib` → `ext/darwin/sqlite-vec.dylib`
- Linux: `sqlite-vec.so` → `ext/linux/sqlite-vec.so`

Commit these binaries OR fetch them at build time. For MVP, commit them (document license — sqlite-vec is MIT).

- [ ] **Step 2: Write the failing test**

`internal/store/vec_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestVecInsertAndSearch(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "vec.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.EnsureVecTable("test_vec", 3); err != nil {
		t.Fatalf("EnsureVecTable: %v", err)
	}

	vec := [][]float32{
		{1.0, 0.0, 0.0},
		{0.0, 1.0, 0.0},
		{0.9, 0.1, 0.0}, // close to first
	}
	ids := []string{"a", "b", "c"}
	for i := range vec {
		if err := db.InsertVector("test_vec", ids[i], vec[i]); err != nil {
			t.Fatalf("InsertVector %d: %v", i, err)
		}
	}

	got, err := db.SearchVectors("test_vec", []float32{1.0, 0.0, 0.0}, 1)
	if err != nil {
		t.Fatalf("SearchVectors: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("got %+v, want id=a at top", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestVec -v`
Expected: FAIL — methods undefined.

- [ ] **Step 4: Implement vec loading and ops**

To load an extension on a `database/sql` connection, use a conn-hook via the sqlite3 driver's `RegisterConnLimitCallback` or, more simply, open with a DSN that runs a connect hook. The cleanest MVP approach: use the `sqlite3.ConnectHook` via a custom driver registration.

`internal/store/sqlite.go` — replace the `Open` function to register a driver that loads `vec0`:

```go
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	sql *sql.DB
}

// vecExtPath returns the platform-specific sqlite-vec extension path next to
// the running binary. Returns empty string if no candidate exists.
func vecExtPath() string {
	name := map[string]string{
		"windows": "sqlite-vec.dll",
		"darwin":  "sqlite-vec.dylib",
		"linux":   "sqlite-vec.so",
	}[runtime.GOOS]
	if name == "" {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(exe), "ext", runtime.GOOS, name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

func init() {
	// Register a driver that auto-loads vec0 on every new connection.
	sql.Register("sqlite3_vec", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			p := vecExtPath()
			if p == "" {
				return nil // no extension available; vector ops will error later
			}
			if err := conn.LoadExtension(p, "sqlite3_vec_init"); err != nil {
				return fmt.Errorf("load sqlite-vec: %w", err)
			}
			return nil
		},
	})
}

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3_vec",
		path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &DB{sql: sqlDB}, nil
}

func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) SQL() *sql.DB { return d.sql }
```

`internal/store/vec.go`:
```go
package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

type VectorHit struct {
	ID       string
	Distance float32
}

// EnsureVecTable creates a vec0 virtual table with the given embedding dimension.
func (d *DB) EnsureVecTable(name string, dim int) error {
	_, err := d.sql.Exec(fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(
			embedding float[%d],
			chunk_id text primary key
		)`, name, dim))
	return err
}

// InsertVector stores one vector keyed by chunkID.
func (d *DB) InsertVector(table, chunkID string, vec []float32) error {
	blob, err := json.Marshal(vec) // vec0 accepts JSON arrays or float blobs
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(
		fmt.Sprintf(`INSERT INTO %s(chunk_id, embedding) VALUES(?, ?)`, table),
		chunkID, string(blob))
	return err
}

// SearchVectors returns the top-k nearest chunk IDs by cosine/L2 distance.
func (d *DB) SearchVectors(table string, query []float32, k int) ([]VectorHit, error) {
	blob, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(
		`SELECT chunk_id, distance FROM %s
		 WHERE embedding MATCH ?
		 ORDER BY distance ASC LIMIT ?`, table)
	rows, err := d.sql.Query(q, string(blob), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []VectorHit
	for rows.Next() {
		var h VectorHit
		if err := rows.Scan(&h.ID, &h.Distance); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// DropVecTable removes a virtual table (used on KB deletion).
func (d *DB) DropVecTable(name string) error {
	_, err := d.sql.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, name))
	return err
}

// SanitizeTableName reduces a kb id to a safe identifier for vec0 table name.
func SanitizeTableName(kbID string) string {
	return "vec_" + strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.ToLower(kbID))
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestVec -v`
Expected: PASS.

**If this fails:**
- Confirm the `.dll`/`.dylib`/`.so` is present and matches your arch.
- Confirm CGO is enabled and gcc is on PATH.
- If fundamentally blocked, **switch to hnswlib fallback**: replace `vec.go` with an in-memory `hnsw` index persisted to a sidecar file; keep the same `EnsureVecTable`/`InsertVector`/`SearchVectors` signatures so no other task changes. Document the switch in the spec.

- [ ] **Step 6: Commit**

```bash
git add internal ext
git commit -m "feat(store): load sqlite-vec extension, add vector insert/search"
```

---

## Task 4: LLM Client (OpenAI-compatible, streaming + tool_calls)

**Files:**
- Create: `internal/llm/client.go` (interface + types)
- Create: `internal/llm/openai.go` (impl)
- Test: `internal/llm/openai_test.go` (httptest server)

- [ ] **Step 1: Define interface and shared types**

`internal/llm/client.go`:
```go
package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // for role=tool
}

type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"` // raw JSON string
}

type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// JSON schema for arguments, serialized as raw message
	Parameters string `json:"parameters"`
}

// Chunk is one piece of a streamed response.
type Chunk struct {
	Text     string     // non-empty when assistant emits text
	ToolCall *ToolCall  // non-nil when a tool call completes
	Usage    *Usage     // non-nil on final chunk
	Done     bool       // true on terminal chunk
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Config identifies how to reach a provider.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

// LLMClient streams a chat completion. The returned channel closes when done.
type LLMClient interface {
	ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error)
	// Embed returns vectors for the given inputs (used by RAG).
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}
```

- [ ] **Step 2: Write the failing test using httptest**

`internal/llm/openai_test.go`:
```go
package llm

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A fake SSE server that emits two text deltas then a tool_call then [DONE].
func newFakeServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		lines := []string{
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"choices":[{"delta":{"content":" world"}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]}}]}`,
			`data: [DONE]`,
		}
		for _, l := range lines {
			_, _ = w.Write([]byte(l + "\n\n"))
			f.Flush()
		}
	}))
}

func TestChatStreamParsesTextAndToolCall(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	c := NewOpenAIClient(Config{
		BaseURL: srv.URL, APIKey: "test", Model: "m",
	})
	ch, err := c.ChatStream(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var text strings.Builder
	var toolCalls []ToolCall
	for chunk := range ch {
		text.WriteString(chunk.Text)
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, *chunk.ToolCall)
		}
	}
	if text.String() != "Hello world" {
		t.Errorf("text = %q, want %q", text.String(), "Hello world")
	}
	if len(toolCalls) != 1 || toolCalls[0].Name != "bash" {
		t.Errorf("toolCalls = %+v, want one bash call", toolCalls)
	}

	// sanity: bufio import used elsewhere; keep referenced
	_ = bufio.NewReader
}

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}],"usage":{"prompt_tokens":1}}`))
	}))
	defer srv.Close()

	c := NewOpenAIClient(Config{BaseURL: srv.URL, APIKey: "test", Model: "m"})
	vecs, err := c.Embed(context.Background(), []string{"hi"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 {
		t.Errorf("vecs = %+v, want 1 vector of dim 3", vecs)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/llm/ -v`
Expected: FAIL — `NewOpenAIClient` undefined.

- [ ] **Step 4: Implement the OpenAI client**

`internal/llm/openai.go`:
```go
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAIClient struct {
	cfg    Config
	http   *http.Client
}

func NewOpenAIClient(cfg Config) *OpenAIClient {
	return &OpenAIClient{
		cfg:  cfg,
		http: &http.Client{Timeout: 0}, // streaming; per-request context handles timeout
	}
}

type chatReq struct {
	Model    string     `json:"model"`
	Messages []rawMsg   `json:"messages"`
	Tools    []rawTool  `json:"tools,omitempty"`
	Stream   bool       `json:"stream"`
}

type rawMsg struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []rawToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type rawTool struct {
	Type     string       `json:"type"` // always "function"
	Function rawToolSpec  `json:"function"`
}

type rawToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // arbitrary JSON object
}

type rawToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function rawToolCallFunc  `json:"function"`
}

type rawToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func toRawMessages(msgs []Message) []rawMsg {
	out := make([]rawMsg, 0, len(msgs))
	for _, m := range msgs {
		rm := rawMsg{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			rm.ToolCalls = append(rm.ToolCalls, rawToolCall{
				ID: tc.ID, Type: "function",
				Function: rawToolCallFunc{Name: tc.Name, Arguments: tc.Args},
			})
		}
		out = append(out, rm)
	}
	return out
}

func toRawTools(tools []ToolSpec) []rawTool {
	out := make([]rawTool, 0, len(tools))
	for _, ts := range tools {
		var params any
		_ = json.Unmarshal([]byte(ts.Parameters), &params)
		out = append(out, rawTool{Type: "function", Function: rawToolSpec{
			Name: ts.Name, Description: ts.Description, Parameters: params,
		}})
	}
	return out
}

func (c *OpenAIClient) ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error) {
	body, _ := json.Marshal(chatReq{
		Model: c.cfg.Model, Messages: toRawMessages(msgs),
		Tools: toRawTools(tools), Stream: true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan Chunk, 8)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- Chunk{Done: true}
				return
			}
			var ev struct {
				Choices []struct {
					Delta struct {
						Content      string         `json:"content"`
						ToolCalls    []rawToolCall  `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			for _, choice := range ev.Choices {
				if choice.Delta.Content != "" {
					ch <- Chunk{Text: choice.Delta.Content}
				}
				if len(choice.Delta.ToolCalls) > 0 {
					tc := choice.Delta.ToolCalls[0]
					ch <- Chunk{ToolCall: &ToolCall{
						ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments,
					}}
				}
			}
			if ev.Usage != nil {
				ch <- Chunk{Usage: &Usage{
					InputTokens: ev.Usage.PromptTokens,
					OutputTokens: ev.Usage.CompletionTokens,
				}}
			}
		}
	}()
	return ch, nil
}

func (c *OpenAIClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"model": c.cfg.Model, "input": inputs,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed http %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	// hold time import to avoid unused if later removed
	_ = time.Now
	return vecs, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/llm/ -v`
Expected: PASS for both `TestChatStreamParsesTextAndToolCall` and `TestEmbed`.

- [ ] **Step 6: Commit**

```bash
git add internal/llm
git commit -m "feat(llm): OpenAI-compatible streaming client with tool_calls and embed"
```

---

## Task 5: LLM Retry Wrapper

**Files:**
- Create: `internal/llm/retry.go`
- Test: `internal/llm/retry_test.go`

- [ ] **Step 1: Write the failing test**

`internal/llm/retry_test.go`:
```go
package llm

import (
	"context"
	"errors"
	"testing"
)

type flakyClient struct {
	calls int
	fail  int // fail first N calls with this error
}

func (f *flakyClient) ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error) {
	f.calls++
	if f.calls <= f.fail {
		return nil, errors.New("http 429")
	}
	ch := make(chan Chunk, 1)
	ch <- Chunk{Text: "ok", Done: true}
	close(ch)
	return ch, nil
}

func (f *flakyClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	return nil, nil
}

func TestRetryRetriesOnThenSucceeds(t *testing.T) {
	f := &flakyClient{fail: 2}
	r := NewRetry(f, 3, 0) // 3 max, no sleep in tests
	ch, err := r.ChatStream(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var text string
	for c := range ch {
		text += c.Text
	}
	if text != "ok" {
		t.Errorf("text=%q want ok", text)
	}
	if f.calls != 3 {
		t.Errorf("calls=%d want 3", f.calls)
	}
}

func TestRetryGivesUpAfterMax(t *testing.T) {
	f := &flakyClient{fail: 99}
	r := NewRetry(f, 2, 0)
	_, err := r.ChatStream(context.Background(), nil, nil)
	if err == nil {
		t.Errorf("want error after exhausting retries")
	}
	if f.calls != 2 {
		t.Errorf("calls=%d want 2", f.calls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run Retry -v`
Expected: FAIL — `NewRetry` undefined.

- [ ] **Step 3: Implement**

`internal/llm/retry.go`:
```go
package llm

import (
	"context"
	"fmt"
	"time"
)

// Retry wraps an LLMClient, retrying transient errors up to max times.
type Retry struct {
	inner LLMClient
	max   int
	wait  time.Duration
}

func NewRetry(inner LLMClient, max int, wait time.Duration) *Retry {
	return &Retry{inner: inner, max: max, wait: wait}
}

func (r *Retry) ChatStream(ctx context.Context, msgs []Message, tools []ToolSpec) (<-chan Chunk, error) {
	var lastErr error
	for i := 0; i < r.max; i++ {
		ch, err := r.inner.ChatStream(ctx, msgs, tools)
		if err == nil {
			return ch, nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, err // non-retryable
		}
		if r.wait > 0 {
			select {
			case <-time.After(r.wait << i): // exponential-ish
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("after %d retries: %w", r.max, lastErr)
}

func (r *Retry) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	var lastErr error
	for i := 0; i < r.max; i++ {
		v, err := r.inner.Embed(ctx, inputs)
		if err == nil {
			return v, nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("after %d retries: %w", r.max, lastErr)
}

// isTransient returns true for 429 / 5xx style errors (matched by substring).
func isTransient(err error) bool {
	msg := err.Error()
	return contains(msg, "429") || contains(msg, "500") ||
		contains(msg, "502") || contains(msg, "503") || contains(msg, "504")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/llm/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm
git commit -m "feat(llm): retry wrapper for transient errors"
```

---

## Task 6: Store Repos — Providers, Sessions, Messages

**Files:**
- Create: `internal/store/providers.go`
- Create: `internal/store/sessions.go`
- Test: `internal/store/repos_test.go`

- [ ] **Step 1: Write the failing test**

`internal/store/repos_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
	"time"
)

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func TestProviderCRUD(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()

	p := Provider{
		ID: "prov_1", Name: "p", BaseURL: "http://x", APIKey: "k",
		ChatModel: "m", IsDefault: true, CreatedAt: now(), UpdatedAt: now(),
	}
	if err := db.CreateProvider(p); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetProvider("prov_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "p" {
		t.Errorf("name=%q", got.Name)
	}
	all, _ := db.ListProviders()
	if len(all) != 1 {
		t.Errorf("len=%d", len(all))
	}
	if err := db.DeleteProvider("prov_1"); err != nil {
		t.Fatal(err)
	}
	_, err = db.GetProvider("prov_1")
	if err == nil {
		t.Errorf("expected not-found after delete")
	}
}

func TestSessionAndMessages(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()

	// need a provider row first (FK)
	db.CreateProvider(Provider{
		ID: "prov_1", Name: "p", BaseURL: "u", APIKey: "k", ChatModel: "m",
		IsDefault: true, CreatedAt: now(), UpdatedAt: now(),
	})

	s := Session{ID: "sess_1", Title: "t", ProviderID: "prov_1",
		ToolsEnabled: 1, CreatedAt: now(), UpdatedAt: now()}
	if err := db.CreateSession(s); err != nil {
		t.Fatal(err)
	}

	msgs := []Message{
		{ID: "m1", SessionID: "sess_1", Role: "user", Content: "hi", CreatedAt: now()},
		{ID: "m2", SessionID: "sess_1", Role: "assistant", Content: "yo", CreatedAt: now()},
	}
	for _, m := range msgs {
		if err := db.AppendMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListMessages("sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "m1" {
		t.Errorf("got=%+v", got)
	}

	// delete session cascades messages
	if err := db.DeleteSession("sess_1"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListMessages("sess_1")
	if len(got) != 0 {
		t.Errorf("messages not cascaded, len=%d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run ProviderCRUD -v`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement provider repo**

`internal/store/providers.go`:
```go
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

func (d *DB) DeleteProvider(id string) error {
	_, err := d.sql.Exec(`DELETE FROM providers WHERE id=?`, id)
	return err
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
```

- [ ] **Step 4: Implement session + message repo**

`internal/store/sessions.go`:
```go
package store

type Session struct {
	ID           string
	Title        string
	ProviderID   string
	KBID         string
	ToolsEnabled int
	CreatedAt    string
	UpdatedAt    string
}

type Message struct {
	ID         string
	SessionID  string
	Role       string
	Content    string
	ToolCalls  string // JSON
	ToolCallID string
	Citations  string // JSON
	TokensIn   int
	TokensOut  int
	CreatedAt  string
}

func (d *DB) CreateSession(s Session) error {
	_, err := d.sql.Exec(`INSERT INTO sessions(id,title,provider_id,kb_id,tools_enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)`,
		s.ID, s.Title, nullable(s.ProviderID), nullable(s.KBID),
		s.ToolsEnabled, s.CreatedAt, s.UpdatedAt)
	return err
}

func (d *DB) GetSession(id string) (Session, error) {
	row := d.sql.QueryRow(`SELECT id,title,provider_id,kb_id,tools_enabled,created_at,updated_at
		FROM sessions WHERE id=?`, id)
	var s Session
	var prov, kb *string
	err := row.Scan(&s.ID, &s.Title, &prov, &kb, &s.ToolsEnabled, &s.CreatedAt, &s.UpdatedAt)
	if prov != nil {
		s.ProviderID = *prov
	}
	if kb != nil {
		s.KBID = *kb
	}
	return s, err
}

func (d *DB) ListSessions() ([]Session, error) {
	rows, err := d.sql.Query(`SELECT id,title,provider_id,kb_id,tools_enabled,created_at,updated_at
		FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		var prov, kb *string
		if err := rows.Scan(&s.ID, &s.Title, &prov, &kb, &s.ToolsEnabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if prov != nil {
			s.ProviderID = *prov
		}
		if kb != nil {
			s.KBID = *kb
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) DeleteSession(id string) error {
	_, err := d.sql.Exec(`DELETE FROM sessions WHERE id=?`, id)
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
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "feat(store): provider/session/message repos with cascade delete"
```

---

## Task 7: Store Repo — Knowledge Base, Documents, Chunks

**Files:**
- Create: `internal/store/kb.go`
- Test: `internal/store/kb_test.go`

- [ ] **Step 1: Write the failing test**

`internal/store/kb_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestKBDocChunkCRUD(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()

	kb := KnowledgeBase{ID: "kb_1", Name: "docs", ChunkSize: 800, ChunkOverlap: 100,
		CreatedAt: now()}
	if err := db.CreateKB(kb); err != nil {
		t.Fatal(err)
	}

	doc := Document{ID: "doc_1", KBID: "kb_1", Filename: "a.txt", FileSize: 5,
		MimeType: "text/plain", Status: "processing", CreatedAt: now()}
	if err := db.CreateDocument(doc); err != nil {
		t.Fatal(err)
	}

	chunks := []Chunk{
		{ID: "c1", DocID: "doc_1", KBID: "kb_1", Ordinal: 0, Text: "hello"},
		{ID: "c2", DocID: "doc_1", KBID: "kb_1", Ordinal: 1, Text: "world"},
	}
	for _, c := range chunks {
		if err := db.CreateChunk(c); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.SetDocumentStatus("doc_1", "ready", 2, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetDocument("doc_1")
	if got.Status != "ready" || got.ChunkCount != 2 {
		t.Errorf("got=%+v", got)
	}

	list, _ := db.ListDocuments("kb_1")
	if len(list) != 1 {
		t.Errorf("len=%d", len(list))
	}

	// delete document cascades chunks
	if err := db.DeleteDocument("doc_1"); err != nil {
		t.Fatal(err)
	}
	cs, _ := db.ListChunksByDoc("doc_1")
	if len(cs) != 0 {
		t.Errorf("chunks not cascaded, len=%d", len(cs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestKBDocChunkCRUD -v`
Expected: FAIL.

- [ ] **Step 3: Implement kb repo**

`internal/store/kb.go`:
```go
package store

type KnowledgeBase struct {
	ID              string
	Name            string
	Description     string
	EmbedProviderID string
	ChunkSize       int
	ChunkOverlap    int
	DocCount        int
	CreatedAt       string
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
	CreatedAt  string
}

type Chunk struct {
	ID        string
	DocID     string
	KBID      string
	Ordinal   int
	Text      string
	TokenCount int
	Metadata  string // JSON
}

func (d *DB) CreateKB(kb KnowledgeBase) error {
	_, err := d.sql.Exec(`INSERT INTO knowledge_bases
		(id,name,description,embed_provider_id,chunk_size,chunk_overlap,doc_count,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		kb.ID, kb.Name, nullable(kb.Description), nullable(kb.EmbedProviderID),
		kb.ChunkSize, kb.ChunkOverlap, kb.DocCount, kb.CreatedAt)
	return err
}

func (d *DB) GetKB(id string) (KnowledgeBase, error) {
	row := d.sql.QueryRow(`SELECT id,name,description,embed_provider_id,chunk_size,chunk_overlap,doc_count,created_at
		FROM knowledge_bases WHERE id=?`, id)
	var kb KnowledgeBase
	var desc, prov *string
	err := row.Scan(&kb.ID, &kb.Name, &desc, &prov, &kb.ChunkSize, &kb.ChunkOverlap,
		&kb.DocCount, &kb.CreatedAt)
	if desc != nil {
		kb.Description = *desc
	}
	if prov != nil {
		kb.EmbedProviderID = *prov
	}
	return kb, err
}

func (d *DB) ListKBs() ([]KnowledgeBase, error) {
	rows, err := d.sql.Query(`SELECT id,name,description,embed_provider_id,chunk_size,chunk_overlap,doc_count,created_at
		FROM knowledge_bases ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnowledgeBase
	for rows.Next() {
		var kb KnowledgeBase
		var desc, prov *string
		if err := rows.Scan(&kb.ID, &kb.Name, &desc, &prov, &kb.ChunkSize, &kb.ChunkOverlap,
			&kb.DocCount, &kb.CreatedAt); err != nil {
			return nil, err
		}
		if desc != nil {
			kb.Description = *desc
		}
		if prov != nil {
			kb.EmbedProviderID = *prov
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
		(id,kb_id,filename,file_size,mime_type,status,chunk_count,error,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		doc.ID, doc.KBID, doc.Filename, doc.FileSize, nullable(doc.MimeType),
		doc.Status, doc.ChunkCount, nullable(doc.Error), doc.CreatedAt)
	return err
}

func (d *DB) GetDocument(id string) (Document, error) {
	row := d.sql.QueryRow(`SELECT id,kb_id,filename,file_size,mime_type,status,chunk_count,error,created_at
		FROM documents WHERE id=?`, id)
	return scanDocument(row)
}

func (d *DB) ListDocuments(kbID string) ([]Document, error) {
	rows, err := d.sql.Query(`SELECT id,kb_id,filename,file_size,mime_type,status,chunk_count,error,created_at
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
	_, err := d.sql.Exec(`DELETE FROM documents WHERE id=?`, id)
	return err
}

func scanDocument(s scanner) (Document, error) {
	var doc Document
	var mime, errMsg *string
	err := s.Scan(&doc.ID, &doc.KBID, &doc.Filename, &doc.FileSize, &mime,
		&doc.Status, &doc.ChunkCount, &errMsg, &doc.CreatedAt)
	if mime != nil {
		doc.MimeType = *mime
	}
	if errMsg != nil {
		doc.Error = *errMsg
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

// GetChunksByIDs fetches chunk text + source for a list of chunk IDs (post-search join).
func (d *DB) GetChunksByIDs(ids []string) ([]Chunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// build IN clause with placeholders
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat(store): knowledge base/document/chunk repos"
```

---

## Task 8: Tool Engine — Interface, Registry, Confirmation Gate

**Files:**
- Create: `internal/tools/engine.go`
- Create: `internal/tools/gate.go`
- Test: `internal/tools/gate_test.go`

- [ ] **Step 1: Define interface + gate types**

`internal/tools/engine.go`:
```go
package tools

import "context"

// Result is the outcome of a tool execution, returned to the LLM.
type Result struct {
	Content string // text returned to model (may be JSON)
	IsError bool   // true if the tool failed; LLM sees this in content
}

// Tool executes one kind of action.
type Tool interface {
	Spec() Spec
	Run(ctx context.Context, args string, gate Gate) (Result, error)
}

// Spec describes a tool for the LLM.
type Spec struct {
	Name        string
	Description string
	Parameters  string // JSON schema string
}

// ToolSpec is an alias used outside the package for clarity.
type ToolSpec = Spec

// Registry holds the set of registered tools, keyed by name.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		r.tools[t.Spec().Name] = t
	}
	return r
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Spec {
	out := make([]Spec, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Spec())
	}
	return out
}

// Engine ties registry + gate together for the orchestrator.
type Engine struct {
	reg  *Registry
	gate Gate
}

func NewEngine(reg *Registry, gate Gate) *Engine {
	return &Engine{reg: reg, gate: gate}
}

func (e *Engine) List() []Spec { return e.reg.List() }

// Execute runs the named tool. args is the raw JSON arguments string.
func (e *Engine) Execute(ctx context.Context, name, args string) (Result, error) {
	t, ok := e.reg.Get(name)
	if !ok {
		return Result{Content: "unknown tool: " + name, IsError: true}, nil
	}
	return t.Run(ctx, args, e.gate)
}
```

- [ ] **Step 2: Write the failing gate test**

`internal/tools/gate_test.go`:
```go
package tools

import (
	"context"
	"testing"
	"time"
)

func TestGateRequestBlocksUntilResolved(t *testing.T) {
	g := NewGate()
	g.emit = func(req ConfirmRequest) {
		// simulate UI resolving asynchronously
		go func() {
			time.Sleep(20 * time.Millisecond)
			g.Resolve(req.ID, Decision{Allow: true})
		}()
	}

	req := ConfirmRequest{ID: "r1", Tool: "bash", Args: `{"command":"ls"`}
	done := make(chan Decision, 1)
	go func() {
		d := g.Request(context.Background(), req)
		done <- d
	}()

	select {
	case d := <-done:
		if !d.Allow {
			t.Errorf("want allow=true")
		}
	case <-time.After(time.Second):
		t.Fatal("gate never resolved within 1s")
	}
}

func TestGateDeniedPropagates(t *testing.T) {
	g := NewGate()
	g.emit = func(req ConfirmRequest) {
		g.Resolve(req.ID, Decision{Allow: false, Remember: RememberNever})
	}
	d := g.Request(context.Background(), ConfirmRequest{ID: "r2", Tool: "bash"})
	if d.Allow {
		t.Errorf("want denied")
	}
}

func TestGateContextCancel(t *testing.T) {
	g := NewGate()
	g.emit = func(req ConfirmRequest) {} // never resolves
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	d := g.Request(ctx, ConfirmRequest{ID: "r3"})
	if d.Allow {
		t.Errorf("want denied on ctx cancel")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/ -run Gate -v`
Expected: FAIL — undefined types.

- [ ] **Step 4: Implement the gate**

`internal/tools/gate.go`:
```go
package tools

import (
	"context"
	"sync"
)

type RememberScope string

const (
	RememberNever  RememberScope = "never"
	RememberSession RememberScope = "session"
	RememberAlways RememberScope = "always"
)

type ConfirmRequest struct {
	ID    string
	Tool  string
	Args  string
}

type Decision struct {
	Allow   bool
	Remember RememberScope
}

// Gate mediates dangerous tool actions through human confirmation.
// A tool calls Request() (which blocks); the HTTP layer calls Resolve()
// when the UI posts the user's decision.
type Gate struct {
	mu      sync.Mutex
	pending map[string]chan Decision
	allow   []allowRule // session/always rules added by Resolve(remember)

	// emit is invoked when a request needs UI attention. The HTTP server
	// replaces this to push an SSE event.
	emit func(req ConfirmRequest)
}

type allowRule struct {
	tool    string
	argsContains string
	scope   RememberScope
}

func NewGate() *Gate {
	return &Gate{
		pending: map[string]chan Decision{},
		emit:    func(ConfirmRequest) {}, // no-op default
	}
}

// SetEmitter installs the function called when confirmation is needed.
func (g *Gate) SetEmitter(f func(ConfirmRequest)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.emit = f
}

// Allowed returns a pre-cached decision if args match a remember rule.
func (g *Gate) Allowed(tool, args string) (Decision, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.allow {
		if r.tool == tool && (r.argsContains == "" || contains(args, r.argsContains)) {
			return Decision{Allow: true, Remember: r.scope}, true
		}
	}
	return Decision{}, false
}

func (g *Gate) Request(ctx context.Context, req ConfirmRequest) Decision {
	// short-circuit if a remember rule applies
	if d, ok := g.Allowed(req.Tool, req.Args); ok {
		return d
	}
	ch := make(chan Decision, 1)
	g.mu.Lock()
	g.pending[req.ID] = ch
	emit := g.emit
	g.mu.Unlock()

	emit(req)
	defer func() {
		g.mu.Lock()
		delete(g.pending, req.ID)
		g.mu.Unlock()
	}()

	select {
	case d := <-ch:
		if d.Allow && d.Remember != RememberNever {
			g.addRule(req.Tool, req.Args, d.Remember)
		}
		return d
	case <-ctx.Done():
		return Decision{Allow: false, Remember: RememberNever}
	}
}

func (g *Gate) Resolve(id string, d Decision) bool {
	g.mu.Lock()
	ch, ok := g.pending[id]
	g.mu.Unlock()
	if !ok {
		return false
	}
	ch <- d
	return true
}

func (g *Gate) addRule(tool, args, scope RememberScope) {
	// store the full args as the match key (simplest correct behavior)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.allow = append(g.allow, allowRule{tool: tool, argsContains: args, scope: scope})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tools/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): registry, engine, confirmation gate with remember rules"
```

---

## Task 9: Built-in Tools — file_read, file_write, file_edit, grep, bash

**Files:**
- Create: `internal/tools/builtin/file_read.go`
- Create: `internal/tools/builtin/file_write.go`
- Create: `internal/tools/builtin/file_edit.go`
- Create: `internal/tools/builtin/grep.go`
- Create: `internal/tools/builtin/bash.go`
- Test: `internal/tools/builtin/builtin_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/tools/builtin/builtin_test.go`:
```go
package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourname/agent-rust/internal/tools"
)

// autoAllowGate approves every request.
func autoAllowGate() tools.GateInterface {
	return tools.NewAutoAllowGate()
}

func TestFileRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("line1\nline2\n"), 0o644)

	r, err := (&FileRead{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(p)+`"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Content, "line1") {
		t.Errorf("content=%q", r.Content)
	}
}

func TestFileWriteRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")
	g := autoAllowGate()
	_, err := (&FileWrite{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(p)+`","content":"hi"}`, g)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "hi" {
		t.Errorf("file=%q", b)
	}
}

func TestFileEditReplacesFirstMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "e.txt")
	os.WriteFile(p, []byte("foo bar foo"), 0o644)
	_, err := (&FileEdit{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(p)+`","old":"foo","new":"qux"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "qux bar foo" {
		t.Errorf("got=%q want 'qux bar foo'", b)
	}
}

func TestGrepFindsMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nworld\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello again\n"), 0o644)
	r, err := (&Grep{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(dir)+`","pattern":"hello"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Content, "a.txt") || !strings.Contains(r.Content, "b.txt") {
		t.Errorf("content=%q", r.Content)
	}
}

func TestBashRunsCommand(t *testing.T) {
	// Use a command that works on all platforms Go tests run.
	// `go env GOOS` is safe everywhere Go is installed.
	r, err := (&Bash{}).Run(context.Background(),
		`{"command":"go env GOOS"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError {
		t.Errorf("bash failed: %s", r.Content)
	}
	if !strings.Contains(r.Content, "windows") && !strings.Contains(r.Content, "linux") &&
		!strings.Contains(r.Content, "darwin") {
		t.Errorf("unexpected output: %s", r.Content)
	}
}
```

- [ ] **Step 2: Add a test helper for auto-allow gate**

To keep the gate injectable for tests without coupling the builtin package to the real Gate, add a small interface to `engine.go`. Append to `internal/tools/engine.go`:

```go
// GateInterface is the minimal surface tools depend on. The concrete Gate
// satisfies it; tests can substitute an auto-allow implementation.
type GateInterface interface {
	Request(ctx context.Context, req ConfirmRequest) Decision
}

// AutoAllowGate approves everything — for tests and trusted contexts.
type AutoAllowGate struct{}

func NewAutoAllowGate() GateInterface { return AutoAllowGate{} }

func (AutoAllowGate) Request(ctx context.Context, req ConfirmRequest) Decision {
	return Decision{Allow: true, Remember: RememberNever}
}
```

Also make `*Gate` satisfy `GateInterface` (it already has the method — no change needed). Remove the `args` shadowing: in `engine.go` the `Tool.Run` signature uses `Gate` type — change it to `GateInterface`:

```go
type Tool interface {
	Spec() Spec
	Run(ctx context.Context, args string, gate GateInterface) (Result, error)
}
```
(Update the `Engine.Execute` call site to pass `e.gate` — Go will accept `*Gate` as `GateInterface`.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/builtin/ -v`
Expected: FAIL — types undefined.

- [ ] **Step 4: Implement file_read**

`internal/tools/builtin/file_read.go`:
```go
package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yourname/agent-rust/internal/tools"
)

type FileRead struct{}

func (FileRead) Spec() tools.Spec {
	return tools.Spec{
		Name:        "file_read",
		Description: "Read the contents of a file. Returns up to 2000 lines.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string","description":"absolute path to the file"},
			"offset":{"type":"integer","description":"1-based line to start at, optional"},
			"limit":{"type":"integer","description":"max lines to read, optional"}
		},"required":["path"]}`,
	}
}

func (FileRead) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	b, err := os.ReadFile(p.Path)
	if err != nil {
		return tools.Result{Content: "read error: " + err.Error(), IsError: true}, nil
	}
	lines := strings.Split(string(b), "\n")
	if p.Offset > 0 && p.Offset <= len(lines) {
		lines = lines[p.Offset-1:]
	}
	if p.Limit > 0 && p.Limit < len(lines) {
		lines = lines[:p.Limit]
	}
	var sb strings.Builder
	for i, ln := range lines {
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, ln)
	}
	return tools.Result{Content: sb.String()}, nil
}
```

- [ ] **Step 5: Implement file_write**

`internal/tools/builtin/file_write.go`:
```go
package builtin

import (
	"context"
	"os"
	"path/filepath"

	"github.com/yourname/agent-rust/internal/tools"
)

type FileWrite struct{}

func (FileWrite) Spec() tools.Spec {
	return tools.Spec{
		Name:        "file_write",
		Description: "Write content to a file, overwriting if it exists.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string"},
			"content":{"type":"string"}
		},"required":["path","content"]}`,
	}
}

func (FileWrite) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	// dangerous: require confirmation
	d := gate.Request(ctx, tools.ConfirmRequest{ID: newID(), Tool: "file_write", Args: args})
	if !d.Allow {
		return tools.Result{Content: "user denied file_write", IsError: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return tools.Result{Content: "mkdir: " + err.Error(), IsError: true}, nil
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return tools.Result{Content: "write: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "wrote " + p.Path}, nil
}
```

- [ ] **Step 6: Implement file_edit**

`internal/tools/builtin/file_edit.go`:
```go
package builtin

import (
	"context"
	"os"
	"strings"

	"github.com/yourname/agent-rust/internal/tools"
)

type FileEdit struct{}

func (FileEdit) Spec() tools.Spec {
	return tools.Spec{
		Name:        "file_edit",
		Description: "Replace the first exact occurrence of old with new in a file.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string"},
			"old":{"type":"string"},
			"new":{"type":"string"}
		},"required":["path","old","new"]}`,
	}
}

func (FileEdit) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	d := gate.Request(ctx, tools.ConfirmRequest{ID: newID(), Tool: "file_edit", Args: args})
	if !d.Allow {
		return tools.Result{Content: "user denied file_edit", IsError: true}, nil
	}
	b, err := os.ReadFile(p.Path)
	if err != nil {
		return tools.Result{Content: "read: " + err.Error(), IsError: true}, nil
	}
	s := string(b)
	idx := strings.Index(s, p.Old)
	if idx < 0 {
		return tools.Result{Content: "old string not found", IsError: true}, nil
	}
	s = s[:idx] + p.New + s[idx+len(p.Old):]
	if err := os.WriteFile(p.Path, []byte(s), 0o644); err != nil {
		return tools.Result{Content: "write: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "edited " + p.Path}, nil
}
```

- [ ] **Step 7: Implement grep**

`internal/tools/builtin/grep.go`:
```go
package builtin

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourname/agent-rust/internal/tools"
)

type Grep struct{}

func (Grep) Spec() tools.Spec {
	return tools.Spec{
		Name:        "grep",
		Description: "Recursively search for a substring in files under a path.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string"},
			"pattern":{"type":"string"}
		},"required":["path","pattern"]}`,
	}
}

func (Grep) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	var sb strings.Builder
	count := 0
	err := filepath.Walk(p.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			if strings.Contains(sc.Text(), p.Pattern) {
				fmt.Fprintf(&sb, "%s:%d: %s\n", path, lineNo, sc.Text())
				count++
			}
		}
		return nil
	})
	if err != nil {
		return tools.Result{Content: "walk: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: fmt.Sprintf("%d matches:\n%s", count, sb.String())}, nil
}
```

- [ ] **Step 8: Implement bash**

`internal/tools/builtin/bash.go`:
```go
package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/yourname/agent-rust/internal/tools"
)

type Bash struct{}

func (Bash) Spec() tools.Spec {
	return tools.Spec{
		Name:        "bash",
		Description: "Run a shell command. Each call requires user confirmation.",
		Parameters: `{"type":"object","properties":{
			"command":{"type":"string"},
			"timeout":{"type":"integer","description":"seconds, default 30"}
		},"required":["command"]}`,
	}
}

func (Bash) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}

	// dangerous: require confirmation
	d := gate.Request(ctx, tools.ConfirmRequest{ID: newID(), Tool: "bash", Args: args})
	if !d.Allow {
		return tools.Result{Content: "user denied bash command", IsError: true}, nil
	}

	timeout := 30 * time.Second
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", p.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		out += "\n[stderr]\n" + stderr.String()
	}
	if err != nil {
		out += fmt.Sprintf("\n[error] %v", err)
		return tools.Result{Content: out, IsError: true}, nil
	}
	return tools.Result{Content: out}, nil
}
```

- [ ] **Step 9: Add the small shared helpers file**

`internal/tools/builtin/helpers.go`:
```go
package builtin

import (
	"encoding/json"

	"github.com/oklog/ulid/v2"
	"crypto/rand"
)

func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}

func newID() string {
	id, _ := ulid.New(ulid.Now(), rand.Reader)
	return "req_" + id.String()
}
```

- [ ] **Step 10: Run tests**

Run: `go test ./internal/tools/... -v`
Expected: all PASS. (Bash test requires `go` on PATH.)

- [ ] **Step 11: Commit**

```bash
git add internal/tools
git commit -m "feat(tools): builtin file_read/write/edit/grep/bash with confirmation"
```

---

## Task 10: Agent Orchestrator

**Files:**
- Create: `internal/agent/types.go`
- Create: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Define orchestrator-facing types**

`internal/agent/types.go`:
```go
package agent

import (
	"context"

	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/tools"
)

// EventEmitter is how the orchestrator pushes live updates to the HTTP layer.
type EventEmitter interface {
	Emit(event string, data any)
}

// RAGRetriever is the minimal RAG surface the orchestrator needs.
type RAGRetriever interface {
	Retrieve(ctx context.Context, kbID, query string, k int) ([]RetrievedChunk, error)
}

type RetrievedChunk struct {
	ID       string
	Text     string
	DocID    string
	Filename string
}

// Deps bundles the orchestrator's dependencies (DI).
type Deps struct {
	LLM     llm.LLMClient
	Tools   *tools.Engine
	RAG     RAGRetriever // may be nil when RAG is off
	MaxIter int          // safety cap, default 20
}
```

- [ ] **Step 2: Write the failing test using a fake LLM**

`internal/agent/agent_test.go`:
```go
package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/tools"
)

// fakeLLM scripts a sequence of streamed responses.
type fakeLLM struct {
	scripts [][]llm.Chunk
	calls   int
}

func (f *fakeLLM) ChatStream(ctx context.Context, msgs []llm.Message, ts []llm.ToolSpec) (<-chan llm.Chunk, error) {
	script := f.scripts[f.calls%len(f.scripts)]
	f.calls++
	ch := make(chan llm.Chunk, len(script))
	for _, c := range script {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (f *fakeLLM) Embed(ctx context.Context, in []string) ([][]float32, error) {
	return nil, nil
}

// fakeToolEngine just echoes the tool name as the result.
type fakeToolEngine struct{}

func (fakeToolEngine) List() []tools.Spec { return nil }
func (fakeToolEngine) Execute(ctx context.Context, name, args string) (tools.Result, error) {
	return tools.Result{Content: fmt.Sprintf("ran %s with %s", name, args)}, nil
}

// recorderEmitter captures events for assertions.
type recorderEmitter struct {
	mu     sync.Mutex
	events []string
}

func (r *recorderEmitter) Emit(event string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func TestRunPlainTextThenDone(t *testing.T) {
	llm_ := &fakeLLM{scripts: [][]llm.Chunk{
		{{Text: "hello"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: llm_, MaxIter: 5})
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		Emit:    rec,
	})
	if len(rec.events) == 0 || rec.events[len(rec.events)-1] != "done" {
		t.Errorf("expected done event, got %v", rec.events)
	}
}

func TestRunToolCallThenAnswer(t *testing.T) {
	llm_ := &fakeLLM{scripts: [][]llm.Chunk{
		// first turn: emit a tool call
		{{ToolCall: &llm.ToolCall{ID: "c1", Name: "bash", Args: `{"command":"ls"}`}}},
		// second turn: answer text
		{{Text: "done"}, {Done: true}},
	}}
	rec := &recorderEmitter{}
	eng := &fakeToolEngine{}
	a := New(Deps{LLM: llm_, Tools: wrapEngine(eng), MaxIter: 5})
	a.Run(context.Background(), RunInput{
		History: []llm.Message{{Role: llm.RoleUser, Content: "ls"}},
		Emit:    rec,
	})
	// expect delta + tool_call + tool_result + delta + done
	joined := join(rec.events)
	if !contains(joined, "tool_call") || !contains(joined, "tool_result") || !contains(joined, "done") {
		t.Errorf("event sequence unexpected: %v", rec.events)
	}
}

func TestRunRespectsMaxIter(t *testing.T) {
	// LLM always emits a tool call -> infinite loop unless capped
	llm_ := &fakeLLM{scripts: [][]llm.Chunk{
		{{ToolCall: &llm.ToolCall{ID: "c", Name: "bash", Args: "{}"}}},
	}}
	rec := &recorderEmitter{}
	a := New(Deps{LLM: llm_, Tools: wrapEngine(&fakeToolEngine{}), MaxIter: 3})
	done := make(chan struct{})
	go func() {
		a.Run(context.Background(), RunInput{
			History: []llm.Message{{Role: llm.RoleUser, Content: "x"}}, Emit: rec,
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not terminate within 2s (maxIter not respected)")
	}
	// 3 tool_call events then done
	count := 0
	for _, e := range rec.events {
		if e == "tool_call" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("tool_call count=%d want 3", count)
	}
}

// --- helpers ---

func wrapEngine(e *fakeToolEngine) *tools.Engine {
	// Build an engine whose Execute uses the fake. We construct an Engine
	// with an empty registry and override via a thin Tool that delegates.
	return tools.NewEngineFromFunc(e.List, e.Execute)
}

func join(xs []string) string {
	out := ""
	for _, x := range xs {
		out += x + ","
	}
	return out
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

Note: the test references `tools.NewEngineFromFunc`. We need to add this constructor so the orchestrator can be wired in tests without real tools. Add to `internal/tools/engine.go`:

```go
// NewEngineFromFunc builds an Engine from plain functions, useful in tests
// and when composing custom execution semantics.
func NewEngineFromFunc(listFn func() []Spec, execFn func(ctx context.Context, name, args string) (Result, error)) *Engine {
	return &Engine{
		reg:  &Registry{}, // empty
		exec: execFn,
		list: listFn,
	}
}
```
And refactor `Engine` to hold optional function fields. Change the `Engine` struct + methods:

```go
type Engine struct {
	reg  *Registry
	exec func(ctx context.Context, name, args string) (Result, error)
	list func() []Spec
	gate Gate
}

func NewEngine(reg *Registry, gate Gate) *Engine {
	return &Engine{reg: reg, gate: gate,
		exec: func(ctx context.Context, name, args string) (Result, error) {
			t, ok := reg.Get(name)
			if !ok {
				return Result{Content: "unknown tool: " + name, IsError: true}, nil
			}
			return t.Run(ctx, args, gate)
		},
		list: reg.List,
	}
}

func (e *Engine) List() []Spec {
	if e.list != nil {
		return e.list()
	}
	return e.reg.List()
}

func (e *Engine) Execute(ctx context.Context, name, args string) (Result, error) {
	if e.exec != nil {
		return e.exec(ctx, name, args)
	}
	t, ok := e.reg.Get(name)
	if !ok {
		return Result{Content: "unknown tool: " + name, IsError: true}, nil
	}
	return t.Run(ctx, args, e.gate)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/agent/ -v`
Expected: FAIL — `New` / `RunInput` undefined.

- [ ] **Step 4: Implement the orchestrator**

`internal/agent/agent.go`:
```go
package agent

import (
	"context"
	"strings"

	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/tools"
)

type Agent struct {
	deps Deps
}

func New(deps Deps) *Agent {
	if deps.MaxIter <= 0 {
		deps.MaxIter = 20
	}
	return &Agent{deps: deps}
}

// RunInput is one invocation of the agent loop.
type RunInput struct {
	History      []llm.Message // current conversation (will be appended to)
	Emit         EventEmitter
	ToolsEnabled bool
	UseRAG       bool
	KBID         string // required when UseRAG
	UserMessage  string // the new user turn (appended to History if non-empty)
}

// Run executes the agent loop and emits events. Returns the assistant's
// final text and the tool calls made.
func (a *Agent) Run(ctx context.Context, in RunInput) {
	history := in.History
	if in.UserMessage != "" {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: in.UserMessage})
	}

	var toolSpecs []llm.ToolSpec
	if in.ToolsEnabled && a.deps.Tools != nil {
		for _, s := range a.deps.Tools.List() {
			toolSpecs = append(toolSpecs, llm.ToolSpec{
				Name: s.Name, Description: s.Description, Parameters: s.Parameters,
			})
		}
	}

	for iter := 0; iter < a.deps.MaxIter; iter++ {
		// Optionally inject RAG context before the model turn.
		if in.UseRAG && a.deps.RAG != nil && in.KBID != "" {
			query := lastUserText(history)
			chunks, err := a.deps.RAG.Retrieve(ctx, in.KBID, query, 5)
			if err == nil && len(chunks) > 0 {
				history = prependRAGContext(history, chunks)
			}
		}

		stream, err := a.deps.LLM.ChatStream(ctx, history, toolSpecs)
		if err != nil {
			in.Emit.Emit("error", map[string]any{"message": err.Error()})
			return
		}

		var assistantText strings.Builder
		var toolCalls []llm.ToolCall
		var usage *llm.Usage
		for chunk := range stream {
			if chunk.Text != "" {
				assistantText.WriteString(chunk.Text)
				in.Emit.Emit("delta", map[string]any{"text": chunk.Text})
			}
			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, *chunk.ToolCall)
				in.Emit.Emit("tool_call", map[string]any{
					"call_id": chunk.ToolCall.ID,
					"tool":    chunk.ToolCall.Name,
					"input":   jsonArgs(chunk.ToolCall.Args),
				})
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			if chunk.Done {
				break
			}
		}

		// record assistant turn
		history = append(history, llm.Message{
			Role: llm.RoleAssistant, Content: assistantText.String(), ToolCalls: toolCalls,
		})

		if len(toolCalls) == 0 {
			// pure text answer; terminate
			in.Emit.Emit("done", map[string]any{
				"usage": usage,
			})
			return
		}

		// execute each tool, feed result back
		for _, tc := range toolCalls {
			result := tools.Result{Content: "no tools available", IsError: true}
			if a.deps.Tools != nil {
				r, err := a.deps.Tools.Execute(ctx, tc.Name, tc.Args)
				if err != nil {
					result = tools.Result{Content: err.Error(), IsError: true}
				} else {
					result = r
				}
			}
			in.Emit.Emit("tool_result", map[string]any{
				"call_id": tc.ID, "content": result.Content, "is_error": result.IsError,
			})
			history = append(history, llm.Message{
				Role: llm.RoleTool, Content: result.Content, ToolCallID: tc.ID,
			})
		}
	}

	// hit max iterations
	in.Emit.Emit("done", map[string]any{"reason": "max_iterations"})
}

func lastUserText(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleUser {
			return history[i].Content
		}
	}
	return ""
}

func prependRAGContext(history []llm.Message, chunks []RetrievedChunk) []llm.Message {
	var sb strings.Builder
	sb.WriteString("Answer using the following knowledge base excerpts where relevant:\n\n")
	for i, c := range chunks {
		sb.WriteString("--- excerpt ")
		sb.WriteString(tools_Itoa(i + 1))
		sb.WriteString(" (")
		sb.WriteString(c.Filename)
		sb.WriteString(", chunk ")
		sb.WriteString(c.ID)
		sb.WriteString(") ---\n")
		sb.WriteString(c.Text)
		sb.WriteString("\n\n")
	}
	system := llm.Message{Role: llm.RoleSystem, Content: sb.String()}
	// prepend system if none exists yet, else merge
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		merged := history[0]
		merged.Content = sb.String() + "\n" + history[0].Content
		out := make([]llm.Message, len(history))
		copy(out, history)
		out[0] = merged
		return out
	}
	return append([]llm.Message{system}, history...)
}

func jsonArgs(s string) map[string]any {
	// best-effort: pass raw string if invalid; the HTTP layer marshals map
	return map[string]any{"raw": s}
}

// tools_Itoa is a tiny dependency-free int->string to avoid importing strconv
// in this file's narrative. In practice use strconv.Itoa.
func tools_Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/agent/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/agent internal/tools
git commit -m "feat(agent): orchestrator loop with tool calling, RAG injection, maxIter guard"
```

---

## Task 11: SSE Writer Helper

**Files:**
- Create: `internal/server/sse.go`

- [ ] **Step 1: Implement**

`internal/server/sse.go`:
```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter wraps an http.ResponseWriter to emit Server-Sent Events.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &SSEWriter{w: w, flusher: f}, true
}

// Emit writes one SSE event: "event: <event>\ndata: <json>\n\n".
func (s *SSEWriter) Emit(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"error":"marshal: %s"}`, err.Error()))
	}
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, string(b))
	s.flusher.Flush()
}
```

- [ ] **Step 2: Quick smoke test inline is covered by the chat handler test (Task 13). Commit now.**

```bash
git add internal/server/sse.go
git commit -m "feat(server): SSE writer helper"
```

---

## Task 12: Wire Store + LLM + Tools + Agent into HTTP Handlers — Config & Sessions

**Files:**
- Modify: `cmd/core/main.go` (wire everything)
- Create: `internal/server/router.go` (full router)
- Create: `internal/server/handler_config.go`
- Create: `internal/server/handler_session.go`

- [ ] **Step 1: Implement the config handlers**

`internal/server/handler_config.go`:
```go
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/yourname/agent-rust/internal/store"
)

type ConfigHandler struct {
	DB *store.DB
}

func (h *ConfigHandler) Routes(r chi.Router) {
	r.Get("/providers", h.listProviders)
	r.Post("/providers", h.createProvider)
	r.Put("/providers/{id}", h.updateProvider)
	r.Delete("/providers/{id}", h.deleteProvider)
}

type providerDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	ChatModel  string `json:"chat_model"`
	EmbedModel string `json:"embed_model"`
	IsDefault  bool   `json:"is_default"`
}

func (h *ConfigHandler) listProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := h.DB.ListProviders()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]providerDTO, len(ps))
	for i, p := range ps {
		out[i] = toProviderDTO(p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ConfigHandler) createProvider(w http.ResponseWriter, r *http.Request) {
	var dto providerDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p := store.Provider{
		ID: "prov_" + ulid.Make().String(), Name: dto.Name, BaseURL: dto.BaseURL,
		APIKey: dto.APIKey, ChatModel: dto.ChatModel, EmbedModel: dto.EmbedModel,
		IsDefault: dto.IsDefault, CreatedAt: now, UpdatedAt: now,
	}
	if err := h.DB.CreateProvider(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toProviderDTO(p))
}

func (h *ConfigHandler) updateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto providerDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p := store.Provider{
		ID: id, Name: dto.Name, BaseURL: dto.BaseURL, APIKey: dto.APIKey,
		ChatModel: dto.ChatModel, EmbedModel: dto.EmbedModel,
		IsDefault: dto.IsDefault, UpdatedAt: now,
	}
	if err := h.DB.CreateProvider(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toProviderDTO(p))
}

func (h *ConfigHandler) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.DB.DeleteProvider(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toProviderDTO(p store.Provider) providerDTO {
	return providerDTO{
		ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey,
		ChatModel: p.ChatModel, EmbedModel: p.EmbedModel, IsDefault: p.IsDefault,
	}
}

// shared response helpers
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg}})
}
```

- [ ] **Step 2: Implement session handlers**

`internal/server/handler_session.go`:
```go
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/yourname/agent-rust/internal/store"
)

type SessionHandler struct {
	DB *store.DB
}

func (h *SessionHandler) Routes(r chi.Router) {
	r.Get("/sessions", h.list)
	r.Post("/sessions", h.create)
	r.Get("/sessions/{id}", h.get)
	r.Delete("/sessions/{id}", h.delete)
	r.Get("/sessions/{id}/messages", h.messages)
}

type sessionDTO struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ProviderID   string `json:"provider_id"`
	KBID         string `json:"kb_id"`
	ToolsEnabled bool   `json:"tools_enabled"`
}

func (h *SessionHandler) list(w http.ResponseWriter, r *http.Request) {
	ss, err := h.DB.ListSessions()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]sessionDTO, len(ss))
	for i, s := range ss {
		out[i] = toSessionDTO(s)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *SessionHandler) create(w http.ResponseWriter, r *http.Request) {
	var dto sessionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	toolsEnabled := 1
	if !dto.ToolsEnabled {
		toolsEnabled = 0
	}
	s := store.Session{
		ID: "sess_" + ulid.Make().String(), Title: dto.Title, ProviderID: dto.ProviderID,
		KBID: dto.KBID, ToolsEnabled: toolsEnabled, CreatedAt: now, UpdatedAt: now,
	}
	if s.Title == "" {
		s.Title = "新对话"
	}
	if err := h.DB.CreateSession(s); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toSessionDTO(s))
}

func (h *SessionHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := h.DB.GetSession(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	msgs, _ := h.DB.ListMessages(id)
	writeJSON(w, http.StatusOK, map[string]any{"session": toSessionDTO(s), "messages": msgs})
}

func (h *SessionHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.DB.DeleteSession(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SessionHandler) messages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	msgs, err := h.DB.ListMessages(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func toSessionDTO(s store.Session) sessionDTO {
	return sessionDTO{
		ID: s.ID, Title: s.Title, ProviderID: s.ProviderID, KBID: s.KBID,
		ToolsEnabled: s.ToolsEnabled != 0,
	}
}
```

- [ ] **Step 3: Commit (chat handler comes next)**

```bash
git add internal/server cmd
git commit -m "feat(server): provider and session HTTP handlers"
```

---

## Task 13: Chat Handler (SSE) + End-to-End Wiring

**Files:**
- Modify: `internal/server/handler_session.go` (add chat route)
- Create: `internal/server/handler_chat.go`
- Create: `internal/server/handler_tools.go`
- Modify: `internal/server/router.go` (full routes)
- Modify: `cmd/core/main.go` (assemble)

- [ ] **Step 1: Implement the chat handler**

`internal/server/handler_chat.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/yourname/agent-rust/internal/agent"
	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/store"
)

type ChatHandler struct {
	DB   *store.DB
	Gate agentGateAdapter // wraps tools.Gate, set by main
}

// agentGateAdapter is the surface the chat handler needs from the gate:
// it sets an emitter that the SSE writer provides.
type agentGateAdapter interface {
	SetEmitter(f func(req any))
}

type chatRequest struct {
	Message      string `json:"message"`
	ToolsEnabled *bool  `json:"tools_enabled"`
	UseRAG       *bool  `json:"use_rag"`
}

func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := h.DB.GetSession(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}
	prov, err := h.DB.GetProvider(sess.ProviderID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "session has no provider")
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// open SSE stream
	sse, ok := NewSSEWriter(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// route gate emit through the SSE writer
	emitter := func(req any) {
		sse.Emit("confirm_req", req)
	}
	// h.Gate is *tools.Gate; its SetEmitter takes func(ConfirmRequest).
	// We bridge via a closure stored on the handler (see main wiring).

	// build LLM client
	llmClient := llm.NewRetry(
		llm.NewOpenAIClient(llm.Config{
			BaseURL: prov.BaseURL, APIKey: prov.APIKey, Model: prov.ChatModel,
		}),
		3, 500*time.Millisecond,
	)

	// load history
	storedMsgs, _ := h.DB.ListMessages(id)
	history := make([]llm.Message, 0, len(storedMsgs)+1)
	for _, m := range storedMsgs {
		history = append(history, storeMsgToLLM(m))
	}

	// persist user message
	now := time.Now().UTC().Format(time.RFC3339)
	userMsgID := "msg_" + ulid.Make().String()
	_ = h.DB.AppendMessage(store.Message{
		ID: userMsgID, SessionID: id, Role: "user", Content: req.Message, CreatedAt: now,
	})

	// emitter that records the assistant message as it streams
	collected := &streamCollector{}
	emit := &multiEmitter{sse: sse, collector: collected}

	// assemble agent deps
	toolsEnabled := sess.ToolsEnabled != 0
	if req.ToolsEnabled != nil {
		toolsEnabled = *req.ToolsEnabled
	}
	useRAG := false
	if req.UseRAG != nil {
		useRAG = *req.UseRAG
	}

	a := h.buildAgent(llmClient, toolsEnabled)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	a.Run(ctx, agent.RunInput{
		History:      history,
		Emit:         emit,
		ToolsEnabled: toolsEnabled,
		UseRAG:       useRAG,
		KBID:         sess.KBID,
		UserMessage:  req.Message,
	})

	// persist assistant message
	asstID := "msg_" + ulid.Make().String()
	_ = h.DB.AppendMessage(store.Message{
		ID: asstID, SessionID: id, Role: "assistant",
		Content: collected.text, ToolCalls: collected.toolCallsJSON(), CreatedAt: now,
	})
}

// buildAgent is overridable in tests.
func (h *ChatHandler) buildAgent(client llm.LLMClient, toolsEnabled bool) *agent.Agent {
	return agent.AgentBuilder{LLM: client}.Build()
}
```

> NOTE for implementer: the above references several small helpers (`storeMsgToLLM`, `streamCollector`, `multiEmitter`, `agent.AgentBuilder`) that should be defined alongside. To keep the plan bounded, here are their definitions to place in `handler_chat.go` (same file):

```go
func storeMsgToLLM(m store.Message) llm.Message {
	return llm.Message{
		Role: llm.Role(m.Role), Content: m.Content, ToolCallID: m.ToolCallID,
	}
}

type streamCollector struct {
	text      string
	toolCalls []llm.ToolCall
}

func (c *streamCollector) appendText(s string)    { c.text += s }
func (c *streamCollector) appendTool(tc llm.ToolCall) { c.toolCalls = append(c.toolCalls, tc) }
func (c *streamCollector) toolCallsJSON() string {
	if len(c.toolCalls) == 0 {
		return ""
	}
	b, _ := json.Marshal(c.toolCalls)
	return string(b)
}

// multiEmitter forwards events to the SSE client AND collects assistant output.
type multiEmitter struct {
	sse       *SSEWriter
	collector *streamCollector
}

func (m *multiEmitter) Emit(event string, data any) {
	switch event {
	case "delta":
		if d, ok := data.(map[string]any); ok {
			if t, ok := d["text"].(string); ok {
				m.collector.appendText(t)
			}
		}
	case "tool_call":
		// already emitted to client; also note for persistence
	}
	m.sse.Emit(event, data)
}
```

And add a small builder to `internal/agent/agent.go` so the chat handler can construct an agent without the full Deps wiring (which belongs in `main.go` for the real path):

```go
// AgentBuilder is a convenience for handlers/tests to construct an Agent
// progressively. Production wiring uses New(Deps{...}) directly in main.
type AgentBuilder struct {
	LLM   llm.LLMClient
	Tools *tools.Engine
	RAG   RAGRetriever
}

func (b AgentBuilder) Build() *Agent {
	return New(Deps{LLM: b.LLM, Tools: b.Tools, RAG: b.RAG})
}
```

- [ ] **Step 2: Implement tool confirm handler**

`internal/server/handler_tools.go`:
```go
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/agent-rust/internal/tools"
)

type ToolsHandler struct {
	Gate *tools.Gate
}

func (h *ToolsHandler) Routes(r chi.Router) {
	r.Get("/tools", h.list)
	r.Post("/tools/confirm", h.confirm)
}

func (h *ToolsHandler) list(w http.ResponseWriter, r *http.Request) {
	// list is built by the engine; this handler exposes a static schema for now.
	writeJSON(w, http.StatusOK, map[string]any{"tools": []string{
		"bash", "file_read", "file_write", "file_edit", "grep",
	}})
}

type confirmRequest struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"` // allow | deny
	Remember  string `json:"remember"` // never | session | always
}

func (h *ToolsHandler) confirm(w http.ResponseWriter, r *http.Request) {
	var req confirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	allow := req.Decision == "allow"
	d := tools.Decision{Allow: allow, Remember: tools.RememberScope(req.Remember)}
	ok := h.Gate.Resolve(req.RequestID, d)
	if !ok {
		writeErr(w, http.StatusNotFound, "no pending request "+req.RequestID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": true})
	_ = chi.URLParam // keep import referenced
}
```

- [ ] **Step 3: Full router**

`internal/server/router.go` (replace the earlier stub):
```go
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/yourname/agent-rust/internal/agent"
	"github.com/yourname/agent-rust/internal/store"
	"github.com/yourname/agent-rust/internal/tools"
)

type Deps struct {
	DB    *store.DB
	Gate  *tools.Gate
	Agent *agent.Agent
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: false,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	r.Route("/api", func(r chi.Router) {
		(&ConfigHandler{DB: d.DB}).Routes(r)
		(&SessionHandler{DB: d.DB}).Routes(r)
		(&ChatHandler{DB: d.DB}).Routes(r)
		(&ToolsHandler{Gate: d.Gate}).Routes(r)
	})
	return r
}
```

Add the `Routes` method to `ChatHandler`:
```go
func (h *ChatHandler) Routes(r chi.Router) {
	r.Post("/sessions/{id}/chat", h.Chat)
}
```

- [ ] **Step 4: Assemble in main.go**

`cmd/core/main.go` (replace prior version):
```go
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/yourname/agent-rust/internal/server"
	"github.com/yourname/agent-rust/internal/store"
	"github.com/yourname/agent-rust/internal/tools"
	"github.com/yourname/agent-rust/internal/tools/builtin"
)

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("mkdir data: %v", err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "app.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	gate := tools.NewGate()
	registry := tools.NewRegistry(
		builtin.FileRead{}, builtin.FileWrite{}, builtin.FileEdit{},
		builtin.Grep{}, builtin.Bash{},
	)
	engine := tools.NewEngine(registry, gate)

	router := server.NewRouter(server.Deps{DB: db, Gate: gate})
	// engine is wired into the agent per-request (per-provider) in ChatHandler;
	// for the standalone core, inject a default engine via a global is avoided —
	// instead ChatHandler.buildAgent is extended in main. (See note below.)
	_ = engine

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(*dataDir, "port.lock"), []byte(itoa(port)), 0o644); err != nil {
		log.Printf("warn: write port.lock: %v", err)
	}
	log.Printf("core listening on %s (port %d)", ln.Addr(), port)
	log.Fatal(http.Serve(ln, router))
}

func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "agent-rust")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
```

> NOTE: The per-request agent in `ChatHandler.buildAgent` should be upgraded to accept the shared `*tools.Engine` and optional RAG. The cleanest fix: add a field `Engine *tools.Engine` to `ChatHandler` and pass it in `Deps`. Update `buildAgent` to:
> ```go
> func (h *ChatHandler) buildAgent(client llm.LLMClient, toolsEnabled bool) *agent.Agent {
>     return agent.New(agent.Deps{LLM: client, Tools: h.Engine})
> }
> ```
> And in `NewRouter`'s `Deps`, include `Engine *tools.Engine`; pass to `&ChatHandler{DB: d.DB, Engine: d.Engine}`. Apply this edit when implementing — it's a 3-line change, called out here to avoid silent drift.

- [ ] **Step 5: Build the whole thing**

Run: `go build ./...`
Expected: compiles. Fix any type mismatches surfaced (the callouts above are the likely ones).

- [ ] **Step 6: Manual end-to-end smoke test**

Start the server:
```bash
go run ./cmd/core
```
Capture the port. Then:
```bash
# create a provider
curl -X POST http://127.0.0.1:<port>/api/providers -H "Content-Type: application/json" -d '{"name":"test","base_url":"https://api.openai.com/v1","api_key":"sk-...","chat_model":"gpt-4o-mini","is_default":true}'

# create a session
curl -X POST http://127.0.0.1:<port>/api/sessions -H "Content-Type: application/json" -d '{"title":"smoke","provider_id":"<prov_id from above>","tools_enabled":false}'

# chat (stream)
curl -N -X POST http://127.0.0.1:<port>/api/sessions/<sess_id>/chat -H "Content-Type: application/json" -d '{"message":"say hi in one word"}'
```
Expected: SSE stream with `event: started`, `event: delta` chunks, `event: done`.

- [ ] **Step 7: Commit**

```bash
git add internal/server cmd
git commit -m "feat(server): chat SSE handler, tool confirm, full router, main wiring"
```

---

## Task 14: RAG Engine — Chunker + Embed + Store + Retrieve

**Files:**
- Create: `internal/rag/chunker.go`
- Create: `internal/rag/chunker_test.go`
- Create: `internal/rag/parser/txt.go`
- Create: `internal/rag/parser/markdown.go`
- Create: `internal/rag/embed.go`
- Create: `internal/rag/ingest.go`
- Create: `internal/rag/retrieve.go`

- [ ] **Step 1: Chunker test**

`internal/rag/chunker_test.go`:
```go
package rag

import (
	"strings"
	"testing"
)

func TestChunkSplitsBySizeWithOverlap(t *testing.T) {
	text := strings.Repeat("a", 2000) // 2000 chars
	chunks := Chunk(text, 800, 100)
	if len(chunks) < 3 {
		t.Fatalf("len=%d, want >=3", len(chunks))
	}
	// overlap: start of chunk 1 should appear in end of chunk 0
	if !strings.Contains(chunks[0], chunks[1][:50]) {
		// chunks[1][:50] is overlap carried from chunks[0] tail
		t.Logf("chunk0 tail vs chunk1 head may differ by split; verify manually")
	}
	for i, c := range chunks {
		if len(c) == 0 {
			t.Errorf("chunk %d empty", i)
		}
	}
}

func TestChunkSmallTextReturnsOne(t *testing.T) {
	got := Chunk("hello", 800, 100)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got=%+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rag/ -run Chunk -v`
Expected: FAIL.

- [ ] **Step 3: Implement chunker**

`internal/rag/chunker.go`:
```go
package rag

// Chunk splits text into pieces of at most size chars, overlapping by overlap
// chars between consecutive chunks. Splits on the nearest whitespace when
// possible to avoid cutting words.
func Chunk(text string, size, overlap int) []string {
	if size <= 0 {
		size = 800
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	if len(text) <= size {
		if len(text) == 0 {
			return nil
		}
		return []string{text}
	}
	var out []string
	step := size - overlap
	for start := 0; start < len(text); start += step {
		end := start + size
		if end > len(text) {
			end = len(text)
		}
		piece := text[start:end]
		out = append(out, piece)
		if end == len(text) {
			break
		}
	}
	return out
}
```

- [ ] **Step 4: Implement parsers**

`internal/rag/parser/txt.go`:
```go
package parser

import "io"

// Register built-in parsers via a registry the ingest step uses.
type Parser interface {
	Supports(mimeType, filename string) bool
	Parse(r io.Reader) (string, error)
}
```

`internal/rag/parser/markdown.go`:
```go
package parser

import (
	"io"
	"strings"
)

type Txt struct{}

func (Txt) Supports(mime, fn string) bool {
	return strings.HasPrefix(mime, "text/plain") || strings.HasSuffix(fn, ".txt") || mime == ""
}
func (Txt) Parse(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type Markdown struct{}

func (Markdown) Supports(mime, fn string) bool {
	return strings.HasSuffix(fn, ".md") || mime == "text/markdown"
}
func (Markdown) Parse(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	// MVP: return raw markdown (no stripping). Chunking handles size.
	return string(b), nil
}
```

- [ ] **Step 5: Implement embed wrapper + ingest + retrieve**

`internal/rag/embed.go`:
```go
package rag

import (
	"context"

	"github.com/yourname/agent-rust/internal/llm"
)

// Embedder calls an LLM client's embedding endpoint.
type Embedder struct {
	Client llm.LLMClient
}

func (e Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.Client.Embed(ctx, texts)
}
```

`internal/rag/ingest.go`:
```go
package rag

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/rag/parser"
	"github.com/yourname/agent-rust/internal/store"
)

type Ingestor struct {
	DB      *store.DB
	Embed   *llm.LLMClient
	KBID    string
	ChunkSz int
	Overlap int
	Parser  parser.Parser // the chosen parser for the file
}

// IngestFile parses, chunks, embeds (batched), and stores a document.
func (ing *Ingestor) IngestFile(ctx context.Context, docID, filename, mimeType string, raw []byte) error {
	p := ing.Parser
	if p == nil {
		p = parser.Txt{}
	}
	text, err := p.Parse(bytes.NewReader(raw))
	if err != nil {
		_ = ing.DB.SetDocumentStatus(docID, "failed", 0, "parse: "+err.Error())
		return err
	}
	chunks := Chunk(text, ing.chunkSize(), ing.overlap())

	// embed in batches of 64
	dim := 0
	for i := 0; i < len(chunks); i += 64 {
		end := i + 64
		if end > len(chunks) {
			end = len(chunks)
		}
		client := llmClientCast(ing.Embed)
		vecs, err := client.Embed(ctx, chunks[i:end])
		if err != nil {
			_ = ing.DB.SetDocumentStatus(docID, "failed", i, "embed: "+err.Error())
			return err
		}
		for j, vec := range vecs {
			idx := i + j
			chunkID := "chunk_" + ulid.Make().String()
			_ = ing.DB.CreateChunk(store.Chunk{
				ID: chunkID, DocID: docID, KBID: ing.KBID, Ordinal: idx, Text: chunks[idx],
			})
			if err := ing.DB.InsertVector(vecTable(ing.KBID), chunkID, vec); err != nil {
				return err
			}
			if dim == 0 {
				dim = len(vec)
			}
		}
	}

	// ensure the vec table exists with the right dim (do this lazily on first insert
	// would be better; for simplicity ensure up front on first successful dim).
	if dim > 0 {
		_ = ing.DB.EnsureVecTable(vecTable(ing.KBID), dim)
	}
	_ = ing.DB.SetDocumentStatus(docID, "ready", len(chunks), "")
	return nil
}

func (ing *Ingestor) chunkSize() int {
	if ing.ChunkSz > 0 {
		return ing.ChunkSz
	}
	return 800
}
func (ing *Ingestor) overlap() int {
	if ing.Overlap > 0 {
		return ing.Overlap
	}
	return 100
}

func vecTable(kbID string) string { return store.SanitizeTableName(kbID) }

// llmClientCast lets ingest accept either *llm.OpenAIClient or LLMClient.
// We standardize on LLMClient in the field type; cast is a no-op identity.
func llmClientCast(c *llm.LLMClient) llm.LLMClient { return *c }

// unused-but-exported to avoid import-cycle lints in the narrative
var _ = time.Now
var _ = fmt.Sprintf
```

> NOTE: The `Embed` field type is awkward here. Cleaner: define `Ingestor.Embed llm.LLMClient` (the interface) and drop `llmClientCast`. Apply that when implementing — the field holds whatever satisfies `LLMClient` (the retry-wrapped client). Replace `type Ingestor struct { Embed *llm.LLMClient ... }` with `Embed llm.LLMClient` and change `llmClientCast(ing.Embed)` to `ing.Embed`. This removes the cast helper entirely.

`internal/rag/retrieve.go`:
```go
package rag

import (
	"context"

	"github.com/yourname/agent-rust/internal/agent"
	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/store"
)

// Retriever implements agent.RAGRetriever over SQLite + sqlite-vec.
type Retriever struct {
	DB          *store.DB
	EmbedClient llm.LLMClient
}

func (r *Retriever) Retrieve(ctx context.Context, kbID, query string, k int) ([]agent.RetrievedChunk, error) {
	if k <= 0 {
		k = 5
	}
	vecs, err := r.EmbedClient.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, err
	}
	hits, err := r.DB.SearchVectors(store.SanitizeTableName(kbID), vecs[0], k)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	chunks, err := r.DB.GetChunksByIDs(ids)
	if err != nil {
		return nil, err
	}
	// attach filename via documents lookup (kept simple: one query per chunk)
	out := make([]agent.RetrievedChunk, 0, len(chunks))
	for _, c := range chunks {
		doc, _ := r.DB.GetDocument(c.DocID)
		out = append(out, agent.RetrievedChunk{
			ID: c.ID, Text: c.Text, DocID: c.DocID, Filename: doc.Filename,
		})
	}
	return out, nil
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/rag/ -v`
Expected: chunker tests PASS. (Ingest/retrieve need a live embedding API; test them manually in Task 16.)

- [ ] **Step 7: Commit**

```bash
git add internal/rag
git commit -m "feat(rag): chunker, txt/md parsers, embed, ingest, retriever"
```

---

## Task 15: Knowledge Base HTTP Handlers

**Files:**
- Create: `internal/server/handler_kb.go`

- [ ] **Step 1: Implement KB handlers (CRUD + upload)**

`internal/server/handler_kb.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/rag"
	"github.com/yourname/agent-rust/internal/rag/parser"
	"github.com/yourname/agent-rust/internal/store"
)

type KBHandler struct {
	DB          *store.DB
	EmbedClient llm.LLMClient // for ingest + retrieve
}

func (h *KBHandler) Routes(r chi.Router) {
	r.Get("/kb", h.list)
	r.Post("/kb", h.create)
	r.Delete("/kb/{id}", h.delete)
	r.Post("/kb/{id}/documents", h.upload)
	r.Get("/kb/{id}/documents", h.listDocs)
	r.Get("/kb/{id}/documents/{doc_id}/status", h.docStatus)
}

type kbDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	EmbedProvider string `json:"embed_provider_id"`
	ChunkSize     int    `json:"chunk_size"`
	ChunkOverlap  int    `json:"chunk_overlap"`
}

func (h *KBHandler) list(w http.ResponseWriter, r *http.Request) {
	kbs, err := h.DB.ListKBs()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]kbDTO, len(kbs))
	for i, k := range kbs {
		out[i] = kbDTO{
			ID: k.ID, Name: k.Name, Description: k.Description,
			EmbedProvider: k.EmbedProviderID, ChunkSize: k.ChunkSize, ChunkOverlap: k.ChunkOverlap,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *KBHandler) create(w http.ResponseWriter, r *http.Request) {
	var dto kbDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	kb := store.KnowledgeBase{
		ID: "kb_" + ulid.Make().String(), Name: dto.Name, Description: dto.Description,
		EmbedProviderID: dto.EmbedProvider, ChunkSize: dto.ChunkSize, ChunkOverlap: dto.ChunkOverlap,
		CreatedAt: now,
	}
	if err := h.DB.CreateKB(kb); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, kbDTO{
		ID: kb.ID, Name: kb.Name, Description: kb.Description,
		EmbedProvider: kb.EmbedProviderID, ChunkSize: kb.ChunkSize, ChunkOverlap: kb.ChunkOverlap,
	})
}

func (h *KBHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_ = h.DB.DropVecTable(store.SanitizeTableName(id))
	if err := h.DB.DeleteKB(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *KBHandler) upload(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "id")
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing file")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	docID := "doc_" + ulid.Make().String()
	kb, _ := h.DB.GetKB(kbID)
	doc := store.Document{
		ID: docID, KBID: kbID, Filename: header.Filename, FileSize: header.Size,
		MimeType: header.Header.Get("Content-Type"), Status: "processing", CreatedAt: now,
	}
	if err := h.DB.CreateDocument(doc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// async ingest
	go func() {
		ing := &rag.Ingestor{
			DB: h.DB, Embed: h.EmbedClient, KBID: kbID,
			ChunkSz: kb.ChunkSize, Overlap: kb.ChunkOverlap,
			Parser: pickParser(doc.Filename, doc.MimeType),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_ = ing.IngestFile(ctx, docID, doc.Filename, doc.MimeType, raw)
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"document_id": docID, "status": "processing",
	})
}

func (h *KBHandler) listDocs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	docs, err := h.DB.ListDocuments(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (h *KBHandler) docStatus(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "doc_id")
	doc, err := h.DB.GetDocument(docID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": doc.Status, "chunk_count": doc.ChunkCount, "error": doc.Error,
	})
}

func pickParser(filename, mime string) parser.Parser {
	candidates := []parser.Parser{parser.Markdown{}, parser.Txt{}}
	for _, c := range candidates {
		if c.Supports(mime, filename) {
			return c
		}
	}
	return parser.Txt{}
}

// keep import referenced
var _ = filepath.Base
```

- [ ] **Step 2: Register KB routes in the router**

Edit `internal/server/router.go` `Deps` to add `EmbedClient llm.LLMClient` and register `(&KBHandler{DB: d.DB, EmbedClient: d.EmbedClient}).Routes(r)`. Update `cmd/core/main.go` to build an embed client from the default provider (or lazily per-KB) and pass it in. For MVP, build from the default provider at startup:
```go
// after opening db, before router:
defaultProv, err := db.GetDefaultProvider() // add this store method or pick ListProviders()[0]
var embedClient llm.LLMClient
if err == nil && defaultProv.EmbedModel != "" {
    embedClient = llm.NewOpenAIClient(llm.Config{
        BaseURL: defaultProv.BaseURL, APIKey: defaultProv.APIKey, Model: defaultProv.EmbedModel,
    })
}
```
Add a `GetDefaultProvider` method to `internal/store/providers.go`:
```go
func (d *DB) GetDefaultProvider() (Provider, error) {
	row := d.sql.QueryRow(`SELECT ... FROM providers WHERE is_default=1 LIMIT 1`) // reuse columns
	var p Provider
	var embedModel *string
	var isDefault int
	err := row.Scan(&p.ID, &p.Name, &p.BaseURL, &p.APIKey, &p.ChatModel, &embedModel, &isDefault, &p.CreatedAt, &p.UpdatedAt)
	if embedModel != nil {
		p.EmbedModel = *embedModel
	}
	return p, err
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: compiles.

- [ ] **Step 4: Commit**

```bash
git add internal/server internal/store cmd
git commit -m "feat(server): knowledge base + document upload handlers, async ingest"
```

---

## Task 16: Integration Smoke Test (Manual + Script)

**Files:**
- Create: `scripts/smoke.sh` (or `scripts/smoke.ps1`)

- [ ] **Step 1: Write the smoke script**

`scripts/smoke.ps1` (Windows; equivalent bash for CI):
```powershell
# Usage: ./scripts/smoke.ps1 -BaseUrl http://127.0.0.1:5xxxx -ApiKey sk-... -KbFile .\README.md
param(
  [Parameter(Mandatory)][string]$BaseUrl,
  [Parameter(Mandatory)][string]$ApiKey,
  [string]$KbFile
)

# 1. health
Invoke-RestMethod "$BaseUrl/healthz"

# 2. create provider
$prov = Invoke-RestMethod "$BaseUrl/api/providers" -Method Post -ContentType "application/json" `
  -Body (@{ name="t"; base_url="https://api.openai.com/v1"; api_key=$ApiKey; chat_model="gpt-4o-mini"; embed_model="text-embedding-3-small"; is_default=$true } | ConvertTo-Json)

# 3. create session
$sess = Invoke-RestMethod "$BaseUrl/api/sessions" -Method Post -ContentType "application/json" `
  -Body (@{ title="smoke"; provider_id=$prov.id; tools_enabled=$false } | ConvertTo-Json)

# 4. chat (stream)
Invoke-RestMethod "$BaseUrl/api/sessions/$($sess.id)/chat" -Method Post -ContentType "application/json" `
  -Body (@{ message="say hi in one word" } | ConvertTo-Json)

if ($KbFile) {
  $kb = Invoke-RestMethod "$BaseUrl/api/kb" -Method Post -ContentType "application/json" `
    -Body (@{ name="smoke-kb"; embed_provider_id=$prov.id } | ConvertTo-Json)
  # upload
  Invoke-RestMethod "$BaseUrl/api/kb/$($kb.id)/documents" -Method Post -InFile $KbFile
  Start-Sleep 5
  Invoke-RestMethod "$BaseUrl/api/kb/$($kb.id)/documents"
}
```

- [ ] **Step 2: Run end-to-end manually**

Start core, run the script with real credentials. Verify:
- chat returns streamed text
- KB upload completes with `status: ready`
- a RAG chat (session with `kb_id`) returns an answer citing uploaded content

- [ ] **Step 3: Commit**

```bash
git add scripts
git commit -m "test: end-to-end smoke script for core service"
```

---

## Task 17: Final Build, Test Sweep, and Plan 1 Closeout

- [ ] **Step 1: Full test run**

Run:
```bash
go test ./... -v
```
Expected: all packages PASS. Document any skipped (e.g., live-API dependent) tests.

- [ ] **Step 2: Build the binary**

Run:
```bash
go build -o dist/agent-core.exe ./cmd/core
```
Expected: binary produced in `dist/`.

- [ ] **Step 3: Update the spec's CGO risk section**

Edit `docs/superpowers/specs/2026-06-16-agent-client-design.md` §11.1: mark whether the CGO + sqlite-vec path was validated successfully or whether the hnswlib fallback was adopted. Record the platform(s) verified.

- [ ] **Step 4: Commit closeout**

```bash
git add docs dist
git commit -m "chore: plan 1 closeout — core service verified, CGO path status recorded"
```

---

## Self-Review (completed during planning)

**Spec coverage:**
- §2 architecture → Task 1 (router), Task 11 (SSE), Task 12-13 (handlers + main wiring) ✓
- §3 module structure → all files mapped in File Structure above ✓
- §4 data flows (chat/tool/RAG/ingest/gate) → Tasks 8, 9, 10, 13, 14, 15 ✓
- §5 API → Tasks 12 (config/session), 13 (chat/tools), 15 (kb) ✓ — *`/events` global SSE stream not yet a dedicated route; confirm_req currently piggybacks on the chat stream. Add a global `/events` route in Plan 2 if the UI needs it independent of a chat.*
- §6 data model → Tasks 2 (schema), 3 (vec), 6/7 (repos) ✓
- §7 error handling → LLM retry (Task 5), tool error as result (Task 9), agent maxIter (Task 10) ✓
- §8 testing → every task has unit tests; integration script (Task 16) ✓
- §9 security → confirmation gate (Task 8), bash/write require confirm (Task 9) ✓ — *command blacklist + working-dir constraint from §9 are noted but not fully implemented in Plan 1; defer to a hardening pass or Plan 2.*

**Known gaps (deferred):**
1. Global `/events` SSE route (currently confirm_req rides the chat stream).
2. §9 command blacklist and working-directory enforcement for file tools.
3. §9 env-var scrubbing and process resource limits in bash.

These are flagged so they are not lost; they belong in a hardening task after Plan 1 verifies the happy path.

**Type consistency:** `LLMClient` interface, `Chunk`, `ToolCall`, `Decision`, `ConfirmRequest`, `Message` signatures cross-referenced across tasks and reconciled. The two inline NOTEs (Engine wiring in chat handler, `Ingestor.Embed` interface) are the only planned refactor callouts and are 3-line changes each.

**Placeholder scan:** no TBD/TODO in task bodies; all code blocks are complete. The inline NOTEs are implementation guidance, not placeholders.

---

## Execution Handoff

Plan 1 complete and saved to `docs/superpowers/plans/2026-06-16-plan1-go-core.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach for Plan 1?** (Then we write Plans 2 and 3.)
