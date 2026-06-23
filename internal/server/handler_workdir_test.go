package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/agent-rust/core/internal/tools"
)

// newTreeHandler 构造一个指向临时目录的 WorkDirHandler,目录内含一个文件夹与若干文件。
func newTreeHandler(t *testing.T) (*WorkDirHandler, string) {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "cmd"))
	mustWrite(t, filepath.Join(root, "main.go"), "x")
	mustWrite(t, filepath.Join(root, "a.txt"), "x")
	wd := tools.NewWorkDir()
	wd.Set(root)
	return &WorkDirHandler{WorkDir: wd}, root
}

// 列目录:文件夹在前,文件在后,path 为相对路径,is_dir 正确。
func TestTreeListsFoldersFirst(t *testing.T) {
	h, _ := newTreeHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workdir/tree", nil)
	h.tree(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []treeItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) < 2 {
		t.Fatalf("expected >=2 items, got %d", len(resp.Items))
	}
	if !resp.Items[0].IsDir {
		t.Errorf("expected first item to be a dir, got %+v", resp.Items[0])
	}
	if resp.Items[0].Path != "cmd" {
		t.Errorf("expected path=cmd, got %q", resp.Items[0].Path)
	}
}

// 二级目录:列出子文件夹内容。
func TestTreeSubdir(t *testing.T) {
	h, _ := newTreeHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workdir/tree?path=cmd", nil)
	h.tree(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for subdir, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// 越界路径(path 含 ../)必须返回 400。
func TestTreeRejectsEscape(t *testing.T) {
	h, _ := newTreeHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workdir/tree?path=../escape", nil)
	h.tree(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for escape, got %d body=%s", rec.Code, rec.Body.String())
	}
}
