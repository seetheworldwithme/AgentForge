package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRerankParsesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("path=%s, want /rerank", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req["query"] != "问题" || req["top_n"] != float64(2) {
			t.Errorf("unexpected req body: %+v", req)
		}
		// Jina/Cohere 统一格式：按相关性返回 index + relevance_score
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 2, "relevance_score": 0.98},
				{"index": 0, "relevance_score": 0.45},
			},
		})
	}))
	defer srv.Close()

	c := NewRerankClient(Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	got, err := c.Rerank(context.Background(), "问题", []string{"a", "b", "c"}, 2)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 2 || got[0].Index != 2 || got[0].RelevanceScore < 0.97 {
		t.Errorf("got %+v", got)
	}
}

func TestRerankHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad key"))
	}))
	defer srv.Close()
	c := NewRerankClient(Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if _, err := c.Rerank(context.Background(), "q", []string{"a"}, 1); err == nil {
		t.Fatal("want error for 401")
	}
}

func TestRerankEmptyDocs(t *testing.T) {
	c := NewRerankClient(Config{BaseURL: "http://x", Model: "m"})
	got, err := c.Rerank(context.Background(), "q", nil, 1)
	if err != nil || len(got) != 0 {
		t.Errorf("got=%+v err=%v", got, err)
	}
}
