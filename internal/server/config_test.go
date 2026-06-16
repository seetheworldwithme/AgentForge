package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/store"
)

// TestProviderTestEndpoint verifies the connection-test endpoint: a working
// OpenAI-compatible server yields ok=true, and a server that 401s yields
// ok=false with the error surfaced. This endpoint is what the UI calls before
// saving a provider, so the user sees a test result instead of a silent 500.
func TestProviderTestEndpoint(t *testing.T) {
	// working chat server: stream one delta then [DONE]
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
		f.Flush()
	}))
	defer okSrv.Close()

	// failing server: 401 unauthorized
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer badSrv.Close()

	db, _ := store.Open(t.TempDir() + "\\config.db")
	defer db.Close()
	router := NewRouter(Deps{DB: db})

	// success case
	body := postJSON(t, router, "/api/providers/test", map[string]any{
		"base_url": okSrv.URL, "api_key": "k", "chat_model": "m",
	})
	if !strings.Contains(body, `"ok":true`) {
		t.Errorf("ok case: expected ok=true, got %s", body)
	}

	// failure case
	body = postJSON(t, router, "/api/providers/test", map[string]any{
		"base_url": badSrv.URL, "api_key": "bad", "chat_model": "m",
	})
	if !strings.Contains(body, `"ok":false`) {
		t.Errorf("fail case: expected ok=false, got %s", body)
	}
	if !strings.Contains(body, "401") && !strings.Contains(body, "Unauthorized") && !strings.Contains(body, "invalid api key") {
		t.Errorf("fail case: expected error detail, got %s", body)
	}
}

func postJSON(t *testing.T, router http.Handler, path string, body any) string {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Body.String()
}
