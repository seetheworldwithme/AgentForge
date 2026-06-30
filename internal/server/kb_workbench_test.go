package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

type fakeEmbedClient struct{}

func (fakeEmbedClient) Chat(context.Context, []llm.Message) (string, error) {
	return "", nil
}

func (fakeEmbedClient) ChatStream(context.Context, []llm.Message, []llm.ToolSpec) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk)
	close(ch)
	return ch, nil
}

func (fakeEmbedClient) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i, s := range inputs {
		out[i] = []float32{float32(len(s) % 7), float32(i + 1), 1}
	}
	return out, nil
}

type fakeKBRetriever struct{}

func (fakeKBRetriever) Retrieve(context.Context, string, string, int) ([]agent.RetrievedChunk, error) {
	return []agent.RetrievedChunk{
		{ID: "chunk_1", DocID: "doc_1", Filename: "guide.md", Text: "AgentForge supports RAG recall testing."},
	}, nil
}

func TestKBWorkbenchEndpoints(t *testing.T) {
	db, _ := store.Open(filepath.Join(t.TempDir(), "kb.db"))
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	_ = db.CreateProvider(store.Provider{
		ID: "prov_1", Name: "embed", BaseURL: "http://example.invalid",
		APIKey: "k", ChatModel: "chat", EmbedModel: "embed", CreatedAt: now, UpdatedAt: now,
	})
	_ = db.CreateKB(store.KnowledgeBase{
		ID: "kb_1", Name: "Docs", EmbedProviderID: "prov_1",
		ChunkSize: 800, ChunkOverlap: 100, CreatedAt: now,
	})
	_ = db.CreateDocument(store.Document{
		ID: "doc_1", KBID: "kb_1", Filename: "guide.md", FileSize: 42,
		MimeType: "text/markdown", Status: "ready", ChunkCount: 1,
		RawPath: filepath.Join(t.TempDir(), "guide.md"), CreatedAt: now,
	})
	_ = db.CreateChunk(store.Chunk{
		ID: "chunk_1", DocID: "doc_1", KBID: "kb_1", Ordinal: 0,
		Text: "AgentForge supports RAG recall testing.",
	})

	router := NewRouter(Deps{
		DB: db, EmbedClient: fakeEmbedClient{}, RAG: fakeKBRetriever{},
		UploadDir: t.TempDir(),
	})

	putJSON(t, router, "/api/kb/kb_1", map[string]any{
		"name": "Updated Docs", "description": "knowledge workbench",
		"embed_provider_id": "prov_1", "chunk_size": 1200, "chunk_overlap": 160,
	}, http.StatusOK)
	kb, _ := db.GetKB("kb_1")
	if kb.Name != "Updated Docs" || kb.ChunkSize != 1200 || kb.ChunkOverlap != 160 {
		t.Fatalf("kb after PUT = %+v", kb)
	}

	// 列表应携带每个 KB 的文档状态聚合计数，供侧边栏直接展示（doc_1 为 ready）
	kbReq, _ := http.NewRequest(http.MethodGet, "/api/kb", nil)
	kbRec := httptest.NewRecorder()
	router.ServeHTTP(kbRec, kbReq)
	if kbRec.Code != http.StatusOK ||
		!strings.Contains(kbRec.Body.String(), `"id":"kb_1"`) ||
		!strings.Contains(kbRec.Body.String(), `"ready_count":1`) ||
		!strings.Contains(kbRec.Body.String(), `"processing_count":0`) {
		t.Fatalf("kb list must aggregate status counts (ready_count=1), got %s", kbRec.Body.String())
	}

	body := postJSONStatus(t, router, "/api/kb/kb_1/chunk-preview", map[string]any{
		"text": "abcdefghijklmnop", "chunk_size": 8, "chunk_overlap": 2,
	}, http.StatusOK)
	if !strings.Contains(body, `"ordinal":0`) || !strings.Contains(body, `"text":"abcdefgh"`) {
		t.Fatalf("chunk preview body = %s", body)
	}

	body = postJSONStatus(t, router, "/api/kb/kb_1/retrieve", map[string]any{
		"query": "recall", "top_k": 3,
	}, http.StatusOK)
	if !strings.Contains(body, `"chunk_id":"chunk_1"`) ||
		!strings.Contains(body, `"filename":"guide.md"`) ||
		!strings.Contains(body, `"similarity"`) {
		t.Fatalf("retrieve body = %s", body)
	}

	req, _ := http.NewRequest(http.MethodGet, "/api/kb/kb_1/documents", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("documents status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"doc_1"`) ||
		strings.Contains(rec.Body.String(), `"ID"`) {
		t.Fatalf("documents must use snake_case JSON, got %s", rec.Body.String())
	}

	req, _ = http.NewRequest(http.MethodGet, "/api/kb/kb_1/documents/doc_1/chunks", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ordinal":0`) {
		t.Fatalf("chunks status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSessionCanBindKnowledgeBase(t *testing.T) {
	db, _ := store.Open(filepath.Join(t.TempDir(), "session.db"))
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	_ = db.CreateProvider(store.Provider{
		ID: "prov_1", Name: "p", BaseURL: "http://example.invalid",
		APIKey: "k", ChatModel: "chat", CreatedAt: now, UpdatedAt: now,
	})
	_ = db.CreateKB(store.KnowledgeBase{ID: "kb_1", Name: "Docs", CreatedAt: now})

	router := NewRouter(Deps{DB: db})
	body := postJSONStatus(t, router, "/api/sessions", map[string]any{
		"title": "RAG chat", "provider_id": "prov_1",
		"tools_enabled": true, "kb_id": "kb_1",
	}, http.StatusCreated)
	if !strings.Contains(body, `"kb_id":"kb_1"`) {
		t.Fatalf("create session body = %s", body)
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}

	putJSON(t, router, "/api/sessions/"+created.ID, map[string]any{
		"title": "RAG chat renamed", "provider_id": "prov_1",
		"tools_enabled": false, "kb_id": "",
	}, http.StatusOK)
	sess, _ := db.GetSession(created.ID)
	if sess.KBID != "" || sess.ToolsEnabled != 0 || sess.Title != "RAG chat renamed" {
		t.Fatalf("updated session = %+v", sess)
	}
}

func postJSONStatus(t *testing.T, router http.Handler, path string, body any, want int) string {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("POST %s status=%d want=%d body=%s", path, rec.Code, want, rec.Body.String())
	}
	return rec.Body.String()
}

func putJSON(t *testing.T, router http.Handler, path string, body any, want int) string {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("PUT %s status=%d want=%d body=%s", path, rec.Code, want, rec.Body.String())
	}
	return rec.Body.String()
}
