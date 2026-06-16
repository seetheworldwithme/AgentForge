package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/agent-rust/core/internal/store"
)

type SessionHandler struct {
	DB *store.DB
}

func (h *SessionHandler) Routes(r chi.Router) {
	r.Get("/sessions", h.list)
	r.Post("/sessions", h.create)
	r.Get("/sessions/{id}", h.get)
	r.Delete("/sessions/{id}", h.delete)
	r.Get("/sessions/{id}/messages", h.messages)
}

type sessionDTO struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ProviderID   string `json:"provider_id"`
	KBID         string `json:"kb_id"`
	ToolsEnabled bool   `json:"tools_enabled"`
}

func (h *SessionHandler) list(w http.ResponseWriter, r *http.Request) {
	ss, err := h.DB.ListSessions()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]sessionDTO, len(ss))
	for i, s := range ss {
		out[i] = toSessionDTO(s)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *SessionHandler) create(w http.ResponseWriter, r *http.Request) {
	var dto sessionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	toolsEnabled := 1
	if !dto.ToolsEnabled {
		toolsEnabled = 0
	}
	s := store.Session{
		ID: "sess_" + ulid.Make().String(), Title: dto.Title, ProviderID: dto.ProviderID,
		KBID: dto.KBID, ToolsEnabled: toolsEnabled, CreatedAt: now, UpdatedAt: now,
	}
	if s.Title == "" {
		s.Title = "新对话"
	}
	if err := h.DB.CreateSession(s); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toSessionDTO(s))
}

func (h *SessionHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := h.DB.GetSession(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	msgs, _ := h.DB.ListMessages(id)
	writeJSON(w, http.StatusOK, map[string]any{"session": toSessionDTO(s), "messages": msgs})
}

func (h *SessionHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.DB.DeleteSession(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SessionHandler) messages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	msgs, err := h.DB.ListMessages(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func toSessionDTO(s store.Session) sessionDTO {
	return sessionDTO{
		ID: s.ID, Title: s.Title, ProviderID: s.ProviderID, KBID: s.KBID,
		ToolsEnabled: s.ToolsEnabled != 0,
	}
}
