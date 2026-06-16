package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
)

type ChatHandler struct {
	DB     *store.DB
	Gate   *tools.Gate // wires tool confirmations onto this chat's SSE stream
	Engine *tools.Engine
	RAG    agent.RAGRetriever // optional; nil disables RAG
}

func (h *ChatHandler) Routes(r chi.Router) {
	r.Post("/sessions/{id}/chat", h.Chat)
}

type chatRequest struct {
	Message      string `json:"message"`
	ToolsEnabled *bool  `json:"tools_enabled"`
	UseRAG       *bool  `json:"use_rag"`
}

func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := h.DB.GetSession(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}
	prov, err := h.DB.GetProvider(sess.ProviderID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "session has no provider")
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// open SSE stream
	sse, ok := NewSSEWriter(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	sse.Emit("started", map[string]any{"session_id": id})

	// Wire tool confirmations onto this chat's SSE stream. When a dangerous
	// tool (e.g. bash) calls gate.Request, the gate invokes the emitter, which
	// emits a confirm_req event here; the UI resolves it via
	// POST /api/tools/confirm and the gate unblocks. Restored to a no-op on
	// return so a stale emitter never leaks into another chat. (Gate is a
	// process-wide singleton; only one chat is active at a time.)
	if h.Gate != nil {
		h.Gate.SetEmitter(func(req tools.ConfirmRequest) {
			sse.Emit("confirm_req", map[string]any{
				"request_id": req.ID,
				"tool":       req.Tool,
				"input":      map[string]any{"raw": req.Args},
			})
		})
		defer h.Gate.SetEmitter(func(tools.ConfirmRequest) {})
	}

	// build LLM client with retry
	llmClient := llm.NewRetry(
		llm.NewOpenAIClient(llm.Config{
			BaseURL: prov.BaseURL, APIKey: prov.APIKey, Model: prov.ChatModel,
		}),
		3, 500*time.Millisecond,
	)

	// load history
	storedMsgs, _ := h.DB.ListMessages(id)
	history := make([]llm.Message, 0, len(storedMsgs)+1)
	for _, m := range storedMsgs {
		history = append(history, storeMsgToLLM(m))
	}

	// persist user message
	now := time.Now().UTC().Format(time.RFC3339)
	userMsgID := "msg_" + ulid.Make().String()
	_ = h.DB.AppendMessage(store.Message{
		ID: userMsgID, SessionID: id, Role: "user", Content: req.Message, CreatedAt: now,
	})

	// emitter that records the assistant message as it streams
	collected := &streamCollector{}
	emit := &multiEmitter{sse: sse, collector: collected}

	toolsEnabled := sess.ToolsEnabled != 0
	if req.ToolsEnabled != nil {
		toolsEnabled = *req.ToolsEnabled
	}
	useRAG := false
	if req.UseRAG != nil {
		useRAG = *req.UseRAG
	}

	a := agent.New(agent.Deps{
		LLM: llmClient, Tools: h.Engine, RAG: h.RAG, MaxIter: 20,
	})
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	a.Run(ctx, agent.RunInput{
		History:      history,
		Emit:         emit,
		ToolsEnabled: toolsEnabled,
		UseRAG:       useRAG,
		KBID:         sess.KBID,
		UserMessage:  req.Message,
	})

	// persist assistant message
	asstID := "msg_" + ulid.Make().String()
	_ = h.DB.AppendMessage(store.Message{
		ID: asstID, SessionID: id, Role: "assistant",
		Content: collected.text, ToolCalls: collected.toolCallsJSON(), CreatedAt: now,
	})
}

func storeMsgToLLM(m store.Message) llm.Message {
	return llm.Message{
		Role: llm.Role(m.Role), Content: m.Content, ToolCallID: m.ToolCallID,
	}
}

type streamCollector struct {
	text      string
	toolCalls []llm.ToolCall
}

func (c *streamCollector) appendText(s string) { c.text += s }

func (c *streamCollector) toolCallsJSON() string {
	if len(c.toolCalls) == 0 {
		return ""
	}
	b, _ := json.Marshal(c.toolCalls)
	return string(b)
}

// multiEmitter forwards events to the SSE client AND collects assistant output.
type multiEmitter struct {
	sse       *SSEWriter
	collector *streamCollector
}

func (m *multiEmitter) Emit(event string, data any) {
	if event == "delta" {
		if d, ok := data.(map[string]any); ok {
			if t, ok := d["text"].(string); ok {
				m.collector.appendText(t)
			}
		}
	}
	m.sse.Emit(event, data)
}
