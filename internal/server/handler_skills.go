package server

import (
	"encoding/json"
	"net/http"

	"github.com/agent-rust/core/internal/skills"
	"github.com/go-chi/chi/v5"
)

type SkillsHandler struct {
	Manager *skills.Manager
}

func (h *SkillsHandler) Routes(r chi.Router) {
	r.Get("/skills", h.list)
	r.Put("/skills/{id}", h.setEnabled)
}

func (h *SkillsHandler) list(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeJSON(w, http.StatusOK, []skills.Skill{})
		return
	}
	items, err := h.Manager.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *SkillsHandler) setEnabled(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "skills manager unavailable")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.Manager.SetEnabled(id, body.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": body.Enabled})
}
