package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

// TestGenerateQA 验证 QA 索引模式的问答对解析：按行、竖线分隔、过滤空。
func TestGenerateQA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"什么是RAG|RAG是检索增强生成\nRAG有什么用|提升事实准确性\n空行|\n|缺问题"}}]}`))
	}))
	defer srv.Close()
	chat := llm.NewOpenAIClient(llm.Config{BaseURL: srv.URL, Model: "m"})
	ing := &Ingestor{Chat: chat}
	qas := ing.generateQA(context.Background(), "d1", "文本")
	if len(qas) != 2 {
		t.Fatalf("got %d qas, want 2: %+v", len(qas), qas)
	}
	if qas[0].q != "什么是RAG" || qas[0].a != "RAG是检索增强生成" {
		t.Errorf("qa[0]=%+v", qas[0])
	}
}

// TestGenerateQANoChat 验证未配 chat 时返回 nil（回退 content 子块）。
func TestGenerateQANoChat(t *testing.T) {
	ing := &Ingestor{}
	if qas := ing.generateQA(context.Background(), "d1", "文本"); qas != nil {
		t.Errorf("no chat should return nil, got %+v", qas)
	}
}

// TestIngestFileFull 验证全量入库(resumeFrom=0)：进度 total>0、done==total、status=ready。
func TestIngestFileFull(t *testing.T) {
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 按输入数量返回向量（ingest 批量嵌入）
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"embedding": []float32{1.0, 0.0, 0.0}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer embedSrv.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "ing.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_ = db.CreateProvider(store.Provider{ID: "pe", BaseURL: embedSrv.URL, EmbedModel: "m", Kind: "embed", CreatedAt: now, UpdatedAt: now})
	_ = db.CreateKB(store.KnowledgeBase{ID: "kb_i", EmbedProviderID: "pe", CreatedAt: now})
	_ = db.CreateDocument(store.Document{ID: "d_i", KBID: "kb_i", Filename: "f.txt", Status: "processing", CreatedAt: now})

	text := strings.Repeat("这是一段用于测试入库的中文文本内容。", 200)
	ing := &Ingestor{DB: db, Embed: llm.NewOpenAIClient(llm.Config{BaseURL: embedSrv.URL, Model: "m"}), KBID: "kb_i", EmbedModel: "m"}
	if err := ing.IngestFile(context.Background(), "d_i", "f.txt", "text/plain", []byte(text), 0); err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	doc, _ := db.GetDocument("d_i")
	if doc.Status != "ready" {
		t.Errorf("status=%s, want ready", doc.Status)
	}
	if doc.ChunkTotal <= 0 {
		t.Errorf("ChunkTotal=%d, want >0", doc.ChunkTotal)
	}
	if doc.ChunkDone != doc.ChunkTotal {
		t.Errorf("done=%d != total=%d", doc.ChunkDone, doc.ChunkTotal)
	}
}
