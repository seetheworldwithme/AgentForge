package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/agent-rust/core/internal/store"
)

type ConfigHandler struct {
	DB *store.DB
}

func (h *ConfigHandler) Routes(r chi.Router) {
	r.Get("/providers", h.listProviders)
	r.Post("/providers", h.createProvider)
	r.Put("/providers/{id}", h.updateProvider)
	r.Delete("/providers/{id}", h.deleteProvider)
}

type providerDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	ChatModel  string `json:"chat_model"`
	EmbedModel string `json:"embed_model"`
	IsDefault  bool   `json:"is_default"`
}

func (h *ConfigHandler) listProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := h.DB.ListProviders()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]providerDTO, len(ps))
	for i, p := range ps {
		out[i] = toProviderDTO(p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ConfigHandler) createProvider(w http.ResponseWriter, r *http.Request) {
	var dto providerDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p := store.Provider{
		ID: "prov_" + ulid.Make().String(), Name: dto.Name, BaseURL: dto.BaseURL,
		APIKey: dto.APIKey, ChatModel: dto.ChatModel, EmbedModel: dto.EmbedModel,
		IsDefault: dto.IsDefault, CreatedAt: now, UpdatedAt: now,
	}
	if err := h.DB.CreateProvider(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toProviderDTO(p))
}

func (h *ConfigHandler) updateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto providerDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// upsert by id: delete then create to keep it simple
	_ = h.DB.DeleteProvider(id)
	p := store.Provider{
		ID: id, Name: dto.Name, BaseURL: dto.BaseURL, APIKey: dto.APIKey,
		ChatModel: dto.ChatModel, EmbedModel: dto.EmbedModel,
		IsDefault: dto.IsDefault, CreatedAt: now, UpdatedAt: now,
	}
	if err := h.DB.CreateProvider(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toProviderDTO(p))
}

func (h *ConfigHandler) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.DB.DeleteProvider(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toProviderDTO(p store.Provider) providerDTO {
	return providerDTO{
		ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey,
		ChatModel: p.ChatModel, EmbedModel: p.EmbedModel, IsDefault: p.IsDefault,
	}
}

// shared response helpers
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg}})
}
