package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/agent-rust/core/internal/tools"
)

type ToolsHandler struct {
	Gate *tools.Gate
}

func (h *ToolsHandler) Routes(r chi.Router) {
	r.Get("/tools", h.list)
	r.Post("/tools/confirm", h.confirm)
}

func (h *ToolsHandler) list(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": []string{
		"bash", "file_read", "file_write", "file_edit", "grep",
	}})
}

type confirmRequest struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"` // allow | deny
	Remember  string `json:"remember"` // never | session | always
}

func (h *ToolsHandler) confirm(w http.ResponseWriter, r *http.Request) {
	var req confirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	allow := req.Decision == "allow"
	d := tools.Decision{Allow: allow, Remember: tools.RememberScope(req.Remember)}
	ok := h.Gate.Resolve(req.RequestID, d)
	if !ok {
		writeErr(w, http.StatusNotFound, "no pending request "+req.RequestID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": true})
	_ = chi.URLParam
}
