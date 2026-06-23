package server

import (
	"encoding/json"
	"net/http"
	"net/url"

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
	// chi URLParam 可能返回未 URL 解码的路径段（前端 encodeURIComponent 把
	// global:foo 编成 global%3Afoo）。统一解码，使其与 List() 产生的 id 一致，
	// 否则 disabled 落库 key 与查询 key 不匹配，开关状态查不回来。
	if dec, err := url.PathUnescape(id); err == nil {
		id = dec
	}
	if err := h.Manager.SetEnabled(id, body.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": body.Enabled})
}
