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

	// embed case: a vector model must be probed via /embeddings, NOT
	// /chat/completions (which 400s with "model does not exist"). The mock
	// only serves /embeddings, so a chat-path probe would 404 and fail.
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("embed case: expected /embeddings, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer embedSrv.Close()

	body = postJSON(t, router, "/api/providers/test", map[string]any{
		"base_url": embedSrv.URL, "api_key": "k", "embed_model": "m", "kind": "embed",
	})
	if !strings.Contains(body, `"ok":true`) {
		t.Errorf("embed case: expected ok=true, got %s", body)
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

func getJSON(t *testing.T, router http.Handler, path string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Body.String()
}

// TestConfirmModeDefaultsToManualAndPersists 验证工具确认规则（手动/自动）：
// 默认 manual；PUT auto 后持久化，再 GET 仍为 auto；非法值回退 manual。
func TestConfirmModeDefaultsToManualAndPersists(t *testing.T) {
	db, _ := store.Open(t.TempDir() + "/confirm.db")
	defer db.Close()
	router := NewRouter(Deps{DB: db})

	if body := getJSON(t, router, "/api/settings/confirm-mode"); !strings.Contains(body, `"mode":"manual"`) {
		t.Fatalf("default mode: expected manual, got %s", body)
	}

	if body := putJSON(t, router, "/api/settings/confirm-mode", map[string]any{"mode": "auto"}, http.StatusOK); !strings.Contains(body, `"mode":"auto"`) {
		t.Fatalf("set auto: got %s", body)
	}

	if body := getJSON(t, router, "/api/settings/confirm-mode"); !strings.Contains(body, `"mode":"auto"`) {
		t.Fatalf("persisted mode: expected auto, got %s", body)
	}

	// 非法值归一为 manual，避免写入脏数据导致后续解析异常
	if body := putJSON(t, router, "/api/settings/confirm-mode", map[string]any{"mode": "bogus"}, http.StatusOK); !strings.Contains(body, `"mode":"manual"`) {
		t.Fatalf("invalid mode: expected manual fallback, got %s", body)
	}
}

// TestProviderDefaultExclusivePerKind 验证「默认」按类别互斥：连续创建多个
// 勾选默认的 chat / embed 模型时，每个类别最终只保留一个默认。
func TestProviderDefaultExclusivePerKind(t *testing.T) {
	db, _ := store.Open(t.TempDir() + "/default.db")
	defer db.Close()
	router := NewRouter(Deps{DB: db})

	mk := func(name, kind string) {
		postJSON(t, router, "/api/providers", map[string]any{
			"name": name, "base_url": "http://x", "api_key": "k",
			"chat_model": "m", "kind": kind, "is_default": true,
		})
	}
	mk("c1", "chat")
	mk("c2", "chat")
	mk("e1", "embed")
	mk("e2", "embed")

	body := getJSON(t, router, "/api/providers")
	var ps []map[string]any
	if err := json.Unmarshal([]byte(body), &ps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	chatDef, embedDef := 0, 0
	for _, p := range ps {
		isDef, _ := p["is_default"].(bool)
		if !isDef {
			continue
		}
		kind, _ := p["kind"].(string)
		if kind == "embed" {
			embedDef++
		} else {
			chatDef++
		}
	}
	if chatDef != 1 {
		t.Errorf("chat defaults = %d, want 1", chatDef)
	}
	if embedDef != 1 {
		t.Errorf("embed defaults = %d, want 1", embedDef)
	}
}
