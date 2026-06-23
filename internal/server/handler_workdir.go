package server

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/agent-rust/core/internal/tools"
	"github.com/go-chi/chi/v5"
)

// WorkDirHandler exposes the process-wide working directory so the UI can
// read and switch the project root. Bash (and other tools) honour it at
// execution time.
type WorkDirHandler struct {
	WorkDir *tools.WorkDir
}

func (h *WorkDirHandler) Routes(r chi.Router) {
	r.Get("/workdir", h.get)
	r.Put("/workdir", h.set)
	// 列出工作目录下的文件/文件夹,供 @ 文件 mention 菜单使用。
	r.Get("/workdir/tree", h.tree)
}

func (h *WorkDirHandler) get(w http.ResponseWriter, r *http.Request) {
	dir := ""
	if h.WorkDir != nil {
		dir = h.WorkDir.Get()
	}
	writeJSON(w, http.StatusOK, map[string]string{"workdir": dir})
}

func (h *WorkDirHandler) set(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkDir string `json:"workdir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.WorkDir != nil {
		h.WorkDir.Set(body.WorkDir)
	}
	writeJSON(w, http.StatusOK, map[string]string{"workdir": body.WorkDir})
}

// tree 列出指定相对路径(默认根)下的子项:文件夹在前、文件在后,
// 各自按名排序(os.ReadDir 保证按名返回)。相对路径越出工作目录返回 400。
func (h *WorkDirHandler) tree(w http.ResponseWriter, r *http.Request) {
	root := ""
	if h.WorkDir != nil {
		root = h.WorkDir.Get()
	}
	if root == "" {
		writeErr(w, http.StatusBadRequest, "workdir not set")
		return
	}
	rel := r.URL.Query().Get("path")
	abs, ok := resolveUnder(root, rel)
	if !ok {
		writeErr(w, http.StatusBadRequest, "path escapes workdir")
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var dirs, files []treeItem
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if excludedDirNames[name] {
				continue // 跳过 .git/node_modules 等噪音目录,保持菜单整洁
			}
			dirs = append(dirs, treeItem{Name: name, IsDir: true, Path: slashJoin(rel, name)})
		} else {
			files = append(files, treeItem{Name: name, IsDir: false, Path: slashJoin(rel, name)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": append(dirs, files...)})
}
