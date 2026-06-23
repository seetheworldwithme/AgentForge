package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agent-rust/core/internal/skills"
	"github.com/agent-rust/core/internal/store"
	"github.com/go-chi/chi/v5"
)

// TestSkillsHTTPTogglePersists 防回归：前端用 encodeURIComponent 把含冒号的 skill id
// （如 global:foo）编码成 global%3Afoo 后 PUT。经 HTTP 层后再次 GET list 必须反映
// disabled 状态。此前 chi URLParam 返回未解码值，落库 key（global%3Afoo）与 List 产生
// 的 id（global:foo）不一致，导致开关「关了又开」。
func TestSkillsHTTPTogglePersists(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	root := t.TempDir()
	mustWriteSkillFile(t, filepath.Join(root, ".agent", "skills", "foo", "SKILL.md"))

	mgr := skills.NewManager(skills.Options{DB: db, GlobalRoot: root})
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		(&SkillsHandler{Manager: mgr}).Routes(r)
	})

	list1 := doSkillsList(t, r)
	if len(list1) != 1 || list1[0].ID != "global:foo" || !list1[0].Enabled {
		t.Fatalf("list1 = %+v", list1)
	}

	// encodeURIComponent("global:foo") == "global%3Afoo"
	doSkillsPut(t, r, "/api/skills/global%3Afoo", false)

	list2 := doSkillsList(t, r)
	if len(list2) != 1 || list2[0].Enabled {
		t.Fatalf("expected disabled after PUT, got %+v", list2)
	}
}

func doSkillsList(t *testing.T, r *chi.Mux) []skills.Skill {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/skills", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out []skills.Skill
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func doSkillsPut(t *testing.T, r *chi.Mux, path string, enabled bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]bool{"enabled": enabled})
	req := httptest.NewRequest("PUT", path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("PUT %s status = %d body=%s", path, rec.Code, rec.Body.String())
	}
}

func mustWriteSkillFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: foo\ndescription: d\n---\n\nfoo\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
