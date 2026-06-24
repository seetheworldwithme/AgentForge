package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
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
	r.Get("/settings/tool-limit", h.getToolLimit)
	r.Put("/settings/tool-limit", h.setToolLimit)
	r.Get("/settings/confirm-mode", h.getConfirmMode)
	r.Put("/settings/confirm-mode", h.setConfirmMode)
}

type providerDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	ChatModel  string `json:"chat_model"`
	EmbedModel string `json:"embed_model"`
	IsDefault  bool   `json:"is_default"`
	// Vision 标记该模型支持视觉/图片粘贴（仅 chat 类有意义）。
	Vision bool `json:"vision"`
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
		Kind: dto.Kind, Vision: dto.Vision, IsDefault: dto.IsDefault, CreatedAt: now, UpdatedAt: now,
	}
	// 设为默认时，先清除同类的旧默认：chat 与 embed 各自只保留一个默认模型。
	if p.IsDefault {
		if err := h.DB.ClearDefaultByKind(p.Kind); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
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
		ChatModel: dto.ChatModel, EmbedModel: dto.EmbedModel, Kind: dto.Kind,
		Vision: dto.Vision, IsDefault: dto.IsDefault, CreatedAt: now, UpdatedAt: now,
	}
	// 设为默认时，先清除同类的旧默认：chat 与 embed 各自只保留一个默认模型。
	if p.IsDefault {
		if err := h.DB.ClearDefaultByKind(p.Kind); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
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

// defaultToolLimit 是工具调用硬上限的默认值。用户未配置时使用，可在齿轮
// 配置弹窗中修改。0 表示不限制（仅靠 context 超时兜底）。
const defaultToolLimit = 50

// toolLimitSetting 读取 max_tool_calls 设置：空或无效时回落到默认值。
// 返回 0 表示用户显式配置为"不限"。
func toolLimitSetting(db *store.DB) int {
	if v, _ := db.GetSetting("max_tool_calls"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultToolLimit
}

// getToolLimit / setToolLimit 暴露单次会话的工具调用硬上限。
func (h *ConfigHandler) getToolLimit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"limit": toolLimitSetting(h.DB)})
}

func (h *ConfigHandler) setToolLimit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Limit int `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Limit < 0 {
		body.Limit = 0 // 负数归一为 0（不限）
	}
	if err := h.DB.SetSetting("max_tool_calls", strconv.Itoa(body.Limit)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"limit": body.Limit})
}

// 工具确认规则的两种取值。
const (
	confirmModeManual = "manual" // 手动：危险工具/命令逐次弹窗确认
	confirmModeAuto   = "auto"   // 自动：直接执行，不询问用户
)

// confirmModeSetting 读取工具确认规则。空或非法值回落 manual（更安全）。
func confirmModeSetting(db *store.DB) string {
	if v, _ := db.GetSetting("confirm_mode"); v == confirmModeAuto {
		return confirmModeAuto
	}
	return confirmModeManual
}

// getConfirmMode / setConfirmMode 暴露工具确认规则（手动/自动）。
func (h *ConfigHandler) getConfirmMode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"mode": confirmModeSetting(h.DB)})
}

func (h *ConfigHandler) setConfirmMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := confirmModeManual
	if body.Mode == confirmModeAuto {
		mode = confirmModeAuto
	}
	if err := h.DB.SetSetting("confirm_mode", mode); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode})
}

func toProviderDTO(p store.Provider) providerDTO {
	return providerDTO{
		ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey,
		ChatModel: p.ChatModel, EmbedModel: p.EmbedModel, Kind: p.Kind,
		Vision: p.Vision, IsDefault: p.IsDefault,
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
