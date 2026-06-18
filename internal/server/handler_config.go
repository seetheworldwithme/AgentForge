package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

type ConfigHandler struct {
	DB *store.DB
}

func (h *ConfigHandler) Routes(r chi.Router) {
	r.Get("/providers", h.listProviders)
	r.Post("/providers", h.createProvider)
	r.Post("/providers/test", h.testProvider)
	r.Put("/providers/{id}", h.updateProvider)
	r.Delete("/providers/{id}", h.deleteProvider)
	r.Get("/settings/title-provider", h.getTitleProvider)
	r.Put("/settings/title-provider", h.setTitleProvider)
}

type providerDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	ChatModel   string `json:"chat_model"`
	EmbedModel  string `json:"embed_model"`
	VisionModel string `json:"vision_model"`
	IsDefault   bool   `json:"is_default"`
	// Kind selects which endpoint /providers/test probes: "chat" (default)
	// hits chat/completions; "embed" hits /embeddings. Other endpoints ignore it.
	Kind string `json:"kind"`
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
		VisionModel: dto.VisionModel, IsDefault: dto.IsDefault, CreatedAt: now, UpdatedAt: now,
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
		ChatModel: dto.ChatModel, EmbedModel: dto.EmbedModel, VisionModel: dto.VisionModel,
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

// testProvider validates that the given credentials can reach the right
// endpoint and get a response. It branches on dto.Kind:
//   - "embed": request one embedding from /embeddings (for vector models)
//   - otherwise (chat, default): stream one "hi" through /chat/completions
//
// It does NOT persist anything; the UI calls it before saving so the user
// gets immediate feedback. Embedding models can't answer chat completions, so
// testing them through the chat path would wrongly report "model does not
// exist".
func (h *ConfigHandler) testProvider(w http.ResponseWriter, r *http.Request) {
	var dto providerDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if dto.BaseURL == "" {
		writeErr(w, http.StatusBadRequest, "base_url is required")
		return
	}

	kind := dto.Kind
	if kind == "" {
		kind = "chat"
	}
	var model string
	switch kind {
	case "embed":
		model = dto.EmbedModel
	default:
		model = dto.ChatModel
	}
	if model == "" {
		writeErr(w, http.StatusBadRequest, kind+"_model is required")
		return
	}

	client := llm.NewOpenAIClient(llm.Config{
		BaseURL: dto.BaseURL, APIKey: dto.APIKey, Model: model,
	})
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if kind == "embed" {
		vecs, err := client.Embed(ctx, []string{"hi"})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if len(vecs) == 0 || len(vecs[0]) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "empty embedding"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	stream, err := client.ChatStream(ctx,
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		nil, // no tools; we only want to confirm connectivity
	)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Read until the stream yields a chunk or closes. A successful first
	// chunk proves the endpoint + key + model work end to end; transport
	// errors (bad URL, 401, etc.) surface as the err returned by ChatStream
	// above. We drain the rest of the stream so the server connection closes
	// cleanly.
	got := false
	for range stream {
		got = true
	}
	if !got {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "empty response"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// getTitleProvider / setTitleProvider expose which provider is dedicated to
// conversation-title generation. The chat handler resolves it per request so
// the title call runs on its own connection, parallel to the main reply.
func (h *ConfigHandler) getTitleProvider(w http.ResponseWriter, r *http.Request) {
	id, _ := h.DB.GetSetting("title_provider_id")
	writeJSON(w, http.StatusOK, map[string]any{"provider_id": id})
}

func (h *ConfigHandler) setTitleProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.DB.SetSetting("title_provider_id", body.ProviderID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider_id": body.ProviderID})
}

func toProviderDTO(p store.Provider) providerDTO {
	return providerDTO{
		ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey,
		ChatModel: p.ChatModel, EmbedModel: p.EmbedModel, VisionModel: p.VisionModel,
		IsDefault: p.IsDefault,
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
