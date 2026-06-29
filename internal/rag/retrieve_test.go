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

// TestRetrieverRerankReorders 验证 rerank 生效：FTS 不命中（纯向量路，RRF 顺序
// 确定 = [c1,c2]），rerank 把顺序反转为 [c2,c1]，且 Score 取自 relevance_score。
func TestRetrieverRerankReorders(t *testing.T) {
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1.0,0.0,0.0]}]}`))
	}))
	defer embedSrv.Close()
	rerankSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"index": 1, "relevance_score": 0.9},
			{"index": 0, "relevance_score": 0.5},
		}})
	}))
	defer rerankSrv.Close()

	db, _ := store.Open(filepath.Join(t.TempDir(), "rag4.db"))
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_ = db.CreateProvider(store.Provider{ID: "pe", BaseURL: embedSrv.URL, EmbedModel: "m", Kind: "embed", CreatedAt: now, UpdatedAt: now})
	_ = db.CreateProvider(store.Provider{ID: "pr", BaseURL: rerankSrv.URL, ChatModel: "rrm", Kind: "rerank", CreatedAt: now, UpdatedAt: now})
	_ = db.CreateKB(store.KnowledgeBase{ID: "kb4", EmbedProviderID: "pe", RerankProviderID: "pr", CreatedAt: now})

	vtbl := store.SanitizeTableName("kb4")
	_ = db.EnsureVecTable(vtbl, 3)
	_ = db.CreateDocument(store.Document{ID: "d4", KBID: "kb4", Filename: "f.txt", Status: "ready", CreatedAt: now})
	_ = db.CreateChunk(store.Chunk{ID: "c1", DocID: "d4", KBID: "kb4", Text: "alpha content"})
	_ = db.InsertVector(vtbl, "c1", []float32{1.0, 0.0, 0.0})
	_ = db.CreateChunk(store.Chunk{ID: "c2", DocID: "d4", KBID: "kb4", Text: "beta content"})
	_ = db.InsertVector(vtbl, "c2", []float32{0.0, 1.0, 0.0})

	r := &Retriever{DB: db, EmbedClient: llm.NewOpenAIClient(llm.Config{BaseURL: embedSrv.URL, Model: "m"})}
	hits, err := r.Search(context.Background(), "kb4", "zzzzz", 2) // zzzzz 不命中 FTS → 纯向量路
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("len=%d, want 2", len(hits))
	}
	if hits[0].ChunkID != "c2" {
		t.Errorf("rerank should reorder c2 first, got order %v", []string{hits[0].ChunkID, hits[1].ChunkID})
	}
	if hits[0].Score < 0.89 {
		t.Errorf("rerank score should be ~0.9, got %f", hits[0].Score)
	}
}

// TestRetrieverRerankFailureFallback 验证 rerank 失败时回退 RRF 顺序，不报错。
func TestRetrieverRerankFailureFallback(t *testing.T) {
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1.0,0.0,0.0]}]}`))
	}))
	defer embedSrv.Close()
	rerankSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer rerankSrv.Close()

	db, _ := store.Open(filepath.Join(t.TempDir(), "rag3.db"))
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_ = db.CreateProvider(store.Provider{ID: "pe", BaseURL: embedSrv.URL, EmbedModel: "m", Kind: "embed", CreatedAt: now, UpdatedAt: now})
	_ = db.CreateProvider(store.Provider{ID: "pr", BaseURL: rerankSrv.URL, ChatModel: "rrm", Kind: "rerank", CreatedAt: now, UpdatedAt: now})
	_ = db.CreateKB(store.KnowledgeBase{ID: "kb3", EmbedProviderID: "pe", RerankProviderID: "pr", CreatedAt: now})

	vtbl := store.SanitizeTableName("kb3")
	_ = db.EnsureVecTable(vtbl, 3)
	_ = db.CreateDocument(store.Document{ID: "d3", KBID: "kb3", Filename: "f.txt", Status: "ready", CreatedAt: now})
	_ = db.CreateChunk(store.Chunk{ID: "c1", DocID: "d3", KBID: "kb3", Text: "hello"})
	_ = db.InsertVector(vtbl, "c1", []float32{1.0, 0.0, 0.0})

	r := &Retriever{DB: db, EmbedClient: llm.NewOpenAIClient(llm.Config{BaseURL: embedSrv.URL, Model: "m"})}
	hits, err := r.Search(context.Background(), "kb3", "query", 3)
	if err != nil {
		t.Fatalf("rerank failure should fallback, not error: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != "c1" {
		t.Errorf("got %+v, want c1", hits)
	}
}

// TestGenerateSubQueries 验证 query 改写的解析：按行拆分、去编号前缀/引号、
// 过滤与原 query 相同或过短的行、限制最多 maxSubQueries 个。
func TestGenerateSubQueries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 模型返回带编号 + 引号 + 一行重复原问题 + 一行过短
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"1. 召回率优化方法\n2) \"向量检索相关性\"\n原问题\nx\n"}}]}`))
	}))
	defer srv.Close()
	chat := llm.NewOpenAIClient(llm.Config{BaseURL: srv.URL, Model: "m"})
	r := &Retriever{}
	subs, err := r.generateSubQueries(context.Background(), chat, "原问题")
	if err != nil {
		t.Fatalf("generateSubQueries: %v", err)
	}
	if len(subs) != 2 || subs[0] != "召回率优化方法" || subs[1] != "向量检索相关性" {
		t.Errorf("got %+v, want [召回率优化方法, 向量检索相关性]", subs)
	}
}

// TestRetrieverParentChildJoin 验证父子分块：召回子块后按 parent_id 上溯返回父块，
// 同一父块的多个子块去重为一个父块。
func TestRetrieverParentChildJoin(t *testing.T) {
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1.0,0.0,0.0]}]}`))
	}))
	defer embedSrv.Close()

	db, _ := store.Open(filepath.Join(t.TempDir(), "ragpc.db"))
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_ = db.CreateProvider(store.Provider{ID: "pe", BaseURL: embedSrv.URL, EmbedModel: "m", Kind: "embed", CreatedAt: now, UpdatedAt: now})
	_ = db.CreateKB(store.KnowledgeBase{ID: "kbpc", EmbedProviderID: "pe", CreatedAt: now})

	vtbl := store.SanitizeTableName("kbpc")
	_ = db.EnsureVecTable(vtbl, 3)
	_ = db.CreateDocument(store.Document{ID: "dpc", KBID: "kbpc", Filename: "f.txt", Status: "ready", CreatedAt: now})
	// 父块（不向量化）
	_ = db.CreateChunk(store.Chunk{ID: "p1", DocID: "dpc", KBID: "kbpc", Ordinal: 0, Text: "父块1完整内容"})
	_ = db.CreateChunk(store.Chunk{ID: "p2", DocID: "dpc", KBID: "kbpc", Ordinal: 1, Text: "父块2完整内容"})
	// 子块（向量化，parent_id 指向父）
	_ = db.CreateChunk(store.Chunk{ID: "c1a", DocID: "dpc", KBID: "kbpc", Ordinal: 2, Text: "子1a片段", ParentID: "p1"})
	_ = db.InsertVector(vtbl, "c1a", []float32{1.0, 0.0, 0.0})
	_ = db.CreateChunk(store.Chunk{ID: "c1b", DocID: "dpc", KBID: "kbpc", Ordinal: 3, Text: "子1b片段", ParentID: "p1"})
	_ = db.InsertVector(vtbl, "c1b", []float32{0.9, 0.1, 0.0})
	_ = db.CreateChunk(store.Chunk{ID: "c2a", DocID: "dpc", KBID: "kbpc", Ordinal: 4, Text: "子2a片段", ParentID: "p2"})
	_ = db.InsertVector(vtbl, "c2a", []float32{0.0, 1.0, 0.0})

	r := &Retriever{DB: db, EmbedClient: llm.NewOpenAIClient(llm.Config{BaseURL: embedSrv.URL, Model: "m"})}
	hits, err := r.Search(context.Background(), "kbpc", "zzzzz", 5) // zzzzz 不命中 FTS → 纯向量路
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// c1a, c1b 同父 p1 去重；c2a → p2。结果应为 [p1, p2]
	if len(hits) != 2 {
		t.Fatalf("len=%d, want 2 (p1+p2 after dedup): %+v", len(hits), hits)
	}
	if hits[0].ChunkID != "p1" || hits[0].Text != "父块1完整内容" {
		t.Errorf("hits[0]=%+v, want p1 with parent text", hits[0])
	}
	if hits[1].ChunkID != "p2" {
		t.Errorf("hits[1]=%+v, want p2", hits[1])
	}
}
