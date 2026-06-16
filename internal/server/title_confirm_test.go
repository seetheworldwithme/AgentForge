package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
)

func TestChatEmitsTitleOnFirstTurn(t *testing.T) {
	fake := fakeOpenAIServer(t)
	defer fake.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "title.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.CreateProvider(store.Provider{
		ID: "prov_1", Name: "fake", BaseURL: fake.URL, APIKey: "k",
		ChatModel: "m", IsDefault: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := db.CreateSession(store.Session{
		ID: "sess_1", Title: "新对话", ProviderID: "prov_1", ToolsEnabled: 0,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	gate := tools.NewGate()
	engine := tools.NewEngine(tools.NewRegistry(), gate)
	router := NewRouter(Deps{DB: db, Gate: gate, Engine: engine})

	req, _ := http.NewRequest(http.MethodPost, "/api/sessions/sess_1/chat",
		strings.NewReader(`{"message":"今天天气怎么样"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { router.ServeHTTP(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("chat handler did not return within 5s")
	}

	body := rec.Body.String()
	t.Logf("SSE body:\n%s", body)
	if !strings.Contains(body, "event: title") {
		t.Errorf("expected a title event, got none")
	}
	sess, _ := db.GetSession("sess_1")
	if sess.Title == "新对话" {
		t.Errorf("title still default; got %q", sess.Title)
	}
	t.Logf("persisted title = %q", sess.Title)
}
