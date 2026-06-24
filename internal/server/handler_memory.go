package server

import (
	"encoding/json"
	"net/http"

	"github.com/agent-rust/core/internal/memory"
	"github.com/go-chi/chi/v5"
)

// MemoryHandler 暴露记忆库的 CRUD 给前端 UI（agent 写记忆走工具，不经此处）。
type MemoryHandler struct {
	Store *memory.MemoryStore
}

func (h *MemoryHandler) Routes(r chi.Router) {
	r.Get("/memory", h.List)
	r.Get("/memory/{name}", h.Get)
	r.Put("/memory/{name}", h.Put)
	r.Delete("/memory/{name}", h.Delete)
}

func (h *MemoryHandler) List(w http.ResponseWriter, r *http.Request) {
	es, err := h.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": es})
}

func (h *MemoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	e, err := h.Store.Get(name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

type memoryPutBody struct {
	Description string      `json:"description"`
	Type        memory.Type `json:"type"`
	Body        string      `json:"body"`
}

func (h *MemoryHandler) Put(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body memoryPutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body: "+err.Error())
		return
	}
	e := memory.Entry{Name: name, Description: body.Description, Type: body.Type, Body: body.Body}
	if err := h.Store.Save(e); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name})
}

func (h *MemoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.Store.Delete(name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name})
}
