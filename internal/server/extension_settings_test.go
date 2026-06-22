package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/mcp"
	"github.com/agent-rust/core/internal/skills"
	"github.com/agent-rust/core/internal/store"
)

func TestSkillsSettingsEndpoints(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	root := t.TempDir()
	writeTestSkill(t, filepath.Join(root, ".agent", "skills", "demo", "SKILL.md"))
	manager := skills.NewManager(skills.Options{DB: db, GlobalRoot: root})
	router := NewRouter(Deps{DB: db, Skills: manager})

	body := requestJSON(t, router, http.MethodGet, "/api/skills", nil)
	if !strings.Contains(body, `"name":"demo"`) || !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("expected enabled demo skill, got %s", body)
	}

	requestJSON(t, router, http.MethodPut, "/api/skills/"+url.PathEscape("global:demo"), map[string]any{
		"enabled": false,
	})
	body = requestJSON(t, router, http.MethodGet, "/api/skills", nil)
	if !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("expected disabled demo skill, got %s", body)
	}
}

func TestMCPSettingsEndpoints(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	manager := mcp.NewManager(db)
	router := NewRouter(Deps{DB: db, MCP: manager})

	requestJSON(t, router, http.MethodPut, "/api/mcp/servers", []mcp.ServerConfig{{
		ID: "srv_demo", Name: "Demo", Command: "demo", Args: []string{"--stdio"}, Enabled: true,
	}})
	body := requestJSON(t, router, http.MethodGet, "/api/mcp/servers", nil)
	if !strings.Contains(body, `"id":"srv_demo"`) || !strings.Contains(body, `"command":"demo"`) {
		t.Fatalf("expected persisted MCP server, got %s", body)
	}
}

func requestJSON(t *testing.T, router http.Handler, method, path string, body any) string {
	t.Helper()
	var rbody *strings.Reader
	if body == nil {
		rbody = strings.NewReader("")
	} else {
		b, _ := json.Marshal(body)
		rbody = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, rbody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func writeTestSkill(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: demo\ndescription: Demo\n---\n\nUse demo.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
