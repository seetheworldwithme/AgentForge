package server

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

type SessionHandler struct {
	DB      *store.DB
	WorkDir *tools.WorkDir // optional; stamped onto new sessions for grouping
}

func (h *SessionHandler) Routes(r chi.Router) {
	r.Get("/sessions", h.list)
	r.Post("/sessions", h.create)
	r.Get("/sessions/{id}", h.get)
	r.Put("/sessions/{id}", h.update)
	r.Delete("/sessions/{id}", h.delete)
	r.Get("/sessions/{id}/messages", h.messages)
}

type sessionDTO struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ProviderID   string `json:"provider_id"`
	KBID         string `json:"kb_id"`
	ToolsEnabled bool   `json:"tools_enabled"`
	WorkDir      string `json:"workdir"`
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
		WorkDir: h.currentWorkDir(),
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
	writeJSON(w, http.StatusOK, map[string]any{"session": toSessionDTO(s), "messages": messagesForDisplay(msgs)})
}

func (h *SessionHandler) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto sessionDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if dto.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	toolsEnabled := 0
	if dto.ToolsEnabled {
		toolsEnabled = 1
	}
	if err := h.DB.UpdateSession(store.Session{
		ID: id, Title: dto.Title, ProviderID: dto.ProviderID, KBID: dto.KBID,
		ToolsEnabled: toolsEnabled, UpdatedAt: now,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s, err := h.DB.GetSession(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toSessionDTO(s))
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
	writeJSON(w, http.StatusOK, messagesForDisplay(msgs))
}

func toSessionDTO(s store.Session) sessionDTO {
	return sessionDTO{
		ID: s.ID, Title: s.Title, ProviderID: s.ProviderID, KBID: s.KBID,
		ToolsEnabled: s.ToolsEnabled != 0, WorkDir: s.WorkDir,
	}
}

// currentWorkDir returns the process-wide working directory, or "" if unset.
func (h *SessionHandler) currentWorkDir() string {
	if h.WorkDir != nil {
		return h.WorkDir.Get()
	}
	return ""
}

func messagesForDisplay(msgs []store.Message) []store.Message {
	results := make(map[string]store.Message)
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			results[m.ToolCallID] = m
		}
	}

	out := make([]store.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			continue
		}
		if m.Role != "assistant" || strings.TrimSpace(m.ToolCalls) == "" {
			out = append(out, m)
			continue
		}

		calls := decodeStoredToolCalls(m.ToolCalls)
		if strings.TrimSpace(m.Content) != "" {
			cp := m
			cp.ToolCalls = ""
			out = append(out, cp)
		}
		for _, tc := range calls {
			toolMsg := store.Message{
				ID:         "tool_" + stableToolDisplayID(m.ID, tc.ID),
				SessionID:  m.SessionID,
				Role:       "tool",
				Content:    toolDisplayContent(tc, results[tc.ID]),
				ToolCallID: tc.ID,
				CreatedAt:  m.CreatedAt,
			}
			out = append(out, toolMsg)
		}
	}
	return out
}

func decodeStoredToolCalls(raw string) []llm.ToolCall {
	var calls []llm.ToolCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil {
		return nil
	}
	return calls
}

func stableToolDisplayID(msgID, callID string) string {
	sum := sha1.Sum([]byte(msgID + ":" + callID))
	return hex.EncodeToString(sum[:8])
}

func toolDisplayContent(tc llm.ToolCall, result store.Message) string {
	content := fmt.Sprintf("→ %s(%s)", tc.Name, displayToolArgs(tc.Args))
	if result.Content != "" {
		content += "\n─────────\n" + strings.TrimRight(result.Content, "\r\n")
	}
	return content
}

func displayToolArgs(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	v = flattenNestedJSON(v)
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(b)
}

func flattenNestedJSON(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := make(map[string]any, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := m[k]
		switch nested := val.(type) {
		case map[string]any, []any:
			b, err := json.Marshal(nested)
			if err == nil {
				out[k] = string(b)
				continue
			}
		}
		out[k] = val
	}
	return out
}
