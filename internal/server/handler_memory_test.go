package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/memory"
	"github.com/go-chi/chi/v5"
)

func newMemoryTestHandler(t *testing.T) (*MemoryHandler, *memory.MemoryStore) {
	t.Helper()
	wd := t.TempDir()
	s := memory.New(func() string { return wd }, "")
	return &MemoryHandler{Store: s}, s
}

func TestMemoryListEmpty(t *testing.T) {
	h, _ := newMemoryTestHandler(t)
	r := chi.NewRouter()
	r.Get("/api/memory", h.List)
	req := httptest.NewRequest("GET", "/api/memory", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Entries []memory.Entry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 0 {
		t.Fatalf("expected empty, got %+v", body.Entries)
	}
}

func TestMemoryPutAndDelete(t *testing.T) {
	h, _ := newMemoryTestHandler(t)
	r := chi.NewRouter()
	r.Put("/api/memory/{name}", h.Put)
	r.Delete("/api/memory/{name}", h.Delete)
	body := `{"description":"Go 环境坑","type":"project","body":"正文"}`
	req := httptest.NewRequest("PUT", "/api/memory/go-env", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest("DELETE", "/api/memory/go-env", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMemoryPutRejectsBadName(t *testing.T) {
	h, _ := newMemoryTestHandler(t)
	r := chi.NewRouter()
	r.Put("/api/memory/{name}", h.Put)
	req := httptest.NewRequest("PUT", "/api/memory/..", strings.NewReader(`{"description":"d","type":"user","body":"b"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
