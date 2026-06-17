package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

// TestRetrieverUsesKBProvider verifies retrieval embeds the query with the KB's
// bound provider (the same model used at ingest), NOT the fallback EmbedClient.
// Regression for the bug where query vectors used a different model than the
// document vectors ("model does not exist" / dimension mismatch).
func TestRetrieverUsesKBProvider(t *testing.T) {
	var (
		mu        sync.Mutex
		lastModel string
	)
	// mock /embeddings server: record the requested model, return a fixed vector.
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(b, &req)
		mu.Lock()
		lastModel = req.Model
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer embedSrv.Close()

	db, _ := store.Open(filepath.Join(t.TempDir(), "rag.db"))
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)

	_ = db.CreateProvider(store.Provider{
		ID: "prov_kb", Name: "kb-embed", BaseURL: embedSrv.URL,
		APIKey: "k", EmbedModel: "kb-embed-model", CreatedAt: now, UpdatedAt: now,
	})
	_ = db.CreateKB(store.KnowledgeBase{
		ID: "kb_1", Name: "Docs", EmbedProviderID: "prov_kb", CreatedAt: now,
	})
	// seed a vector so the vec0 table exists and SearchVectors has a match.
	tbl := store.SanitizeTableName("kb_1")
	_ = db.EnsureVecTable(tbl, 3)
	_ = db.CreateChunk(store.Chunk{ID: "chunk_1", DocID: "doc_1", KBID: "kb_1", Text: "hello"})
	_ = db.InsertVector(tbl, "chunk_1", []float32{0.1, 0.2, 0.3})
	_ = db.CreateDocument(store.Document{ID: "doc_1", KBID: "kb_1", Filename: "f.txt", Status: "ready", CreatedAt: now})

	// Fallback embed client built with a DIFFERENT model — proves Search does
	// not fall through to it for a KB that has its own provider bound.
	r := &Retriever{
		DB: db,
		EmbedClient: llm.NewOpenAIClient(llm.Config{
			BaseURL: embedSrv.URL, APIKey: "k", Model: "default-embed-model",
		}),
	}

	if _, err := r.Search(context.Background(), "kb_1", "query", 3); err != nil {
		t.Fatalf("Search: %v", err)
	}
	mu.Lock()
	got := lastModel
	mu.Unlock()
	if got != "kb-embed-model" {
		t.Fatalf("retrieval must embed with the KB's model %q, got %q", "kb-embed-model", got)
	}
}
