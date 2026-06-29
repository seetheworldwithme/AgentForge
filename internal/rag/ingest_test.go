package rag

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agent-rust/core/internal/llm"
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
