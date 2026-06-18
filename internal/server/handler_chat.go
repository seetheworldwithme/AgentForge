package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
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
	Message      string  `json:"message"`
	ToolsEnabled *bool   `json:"tools_enabled"`
	UseRAG       *bool   `json:"use_rag"`
	ProviderID   *string `json:"provider_id"` // optional override; falls back to session's provider
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
	// Allow per-message provider override so the user can switch models
	// directly from the chat input without changing the session binding.
	if req.ProviderID != nil && *req.ProviderID != "" && *req.ProviderID != sess.ProviderID {
		override, err := h.DB.GetProvider(*req.ProviderID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "selected provider not found")
			return
		}
		prov = override
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
	firstMessage := len(storedMsgs) == 0
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

	// Generate the title concurrently with the reply using a SEPARATE provider
	// (the configured title provider, falling back to this chat's provider).
	// Two different providers are two independent connections, so neither call
	// queues behind the other — the title no longer starves on a single-stream
	// chat provider. Best-effort: on failure/timeout it degrades to a local
	// excerpt so the title is never missing.
	wantTitle := firstMessage && strings.TrimSpace(req.Message) != ""
	var titleWg sync.WaitGroup
	if wantTitle {
		titleWg.Add(1)
		go func() {
			defer titleWg.Done()
			title := generateTitleBestEffort(ctx, h.titleClient(llmClient), req.Message)
			_ = h.DB.RenameSession(id, title, time.Now().UTC().Format(time.RFC3339))
			sse.Emit("title", map[string]any{"session_id": id, "title": title})
			log.Printf("title-gen: session %s -> %q", id, title)
		}()
	} else if firstMessage {
		log.Printf("title-gen: session %s skipped (msgLen=%d)", id, len(req.Message))
	}

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

	// Wait for the concurrent title generation to settle (LLM or fallback)
	// before the stream closes, so the client always receives a title event.
	titleWg.Wait()
}

// titleClient returns the provider dedicated to title generation, falling back
// to the given chat client when none is configured. Using a separate provider
// lets the title call run on its own connection in parallel with the reply.
func (h *ChatHandler) titleClient(fallback llm.LLMClient) llm.LLMClient {
	if id, err := h.DB.GetSetting("title_provider_id"); err == nil && id != "" {
		if p, err := h.DB.GetProvider(id); err == nil && p.ChatModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.ChatModel,
			})
		}
	}
	return fallback
}

// generateTitle asks the model for a very short, plain summary of the user's
// first message to use as a conversation title. Returns the title (possibly ""),
// the number of stream chunks received (diagnostic: >0 with empty text usually
// means a reasoning model), and an error when the request itself failed or
// timed out.
func generateTitle(ctx context.Context, client llm.LLMClient, userMessage string) (string, int, error) {
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	stream, err := client.ChatStream(tctx, []llm.Message{
		{Role: llm.RoleSystem, Content: "你是一个标题生成器。根据用户的问题生成一个简洁的对话标题。" +
			"要求：不超过15个字；只输出标题文字；不要加引号、书名号或末尾标点；不要解释。"},
		{Role: llm.RoleUser, Content: userMessage},
	}, nil)
	if err != nil {
		return "", 0, fmt.Errorf("chat stream: %w", err)
	}
	var sb strings.Builder
	chunks := 0
	for chunk := range stream {
		chunks++
		if chunk.Text != "" {
			sb.WriteString(chunk.Text)
		}
	}
	if tctx.Err() == context.DeadlineExceeded {
		return "", chunks, fmt.Errorf("timeout after 15s (received %d chunks)", chunks)
	}
	return sanitizeTitle(sb.String()), chunks, nil
}

// generateTitleBestEffort asks the LLM for a title; on any failure, empty
// result, or cancellation it falls back to a local excerpt of the user's
// message so the sidebar always gets a title instead of staying "新对话".
func generateTitleBestEffort(ctx context.Context, client llm.LLMClient, userMessage string) string {
	if title, chunks, err := generateTitle(ctx, client, userMessage); err == nil && title != "" {
		return title
	} else if err != nil {
		log.Printf("title-gen: LLM failed (%d chunks, %v); using fallback", chunks, err)
	} else {
		log.Printf("title-gen: LLM returned empty (%d chunks); using fallback", chunks)
	}
	return fallbackTitle(userMessage)
}

// fallbackTitle derives a title locally from the user's first message (first
// line, cleaned and capped): instant, no LLM — used when the title call fails
// or times out.
func fallbackTitle(userMessage string) string {
	s := strings.TrimSpace(userMessage)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	return sanitizeTitle(s)
}

// sanitizeTitle trims surrounding whitespace/quotes/punctuation and collapses
// the title to a single line, capping its length.
func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, trimCutset)
	// collapse newlines/tabs to spaces and trim again
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 20 {
		s = string([]rune(s)[:20])
	}
	return s
}

// trimCutset lists characters stripped from the title's edges: ASCII and
// CJK quotes/punctuation that models often wrap around a title.
var trimCutset = "\"'`.,;:!?" +
	".,、；；：：!？" +
	"「」『』《》【】（）()" +
	"“”‘’"

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
