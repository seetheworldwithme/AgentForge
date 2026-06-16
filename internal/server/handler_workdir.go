package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/agent-rust/core/internal/tools"
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
