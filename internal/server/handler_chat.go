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

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/mcp"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

type ChatHandler struct {
	DB            *store.DB
	Gate          *tools.Gate // wires tool confirmations onto this chat's SSE stream
	Engine        *tools.Engine     // 纯内置工具引擎（未 attach mcp）
	MCP           *mcp.Manager      // 按请求 attach mcp；nil 则不挂载 MCP 工具
	RAG           agent.RAGRetriever  // optional; nil disables RAG
	Skills        agent.SkillProvider // optional; nil disables skills
	MCPConfigPath string
	WorkDir       *tools.WorkDir // optional; nil disables @ file-attachment injection
	Memory        agent.MemoryProvider // optional; nil disables memory injection
}

func (h *ChatHandler) Routes(r chi.Router) {
	r.Post("/sessions/{id}/chat", h.Chat)
}

type chatRequest struct {
	Message      string   `json:"message"`
	ToolsEnabled *bool    `json:"tools_enabled"`
	UseRAG       *bool    `json:"use_rag"`
	ProviderID   *string  `json:"provider_id"`    // optional override; falls back to session's provider
	PlanMode    *bool    `json:"plan_mode"`   // 本次会话临时开启「计划模式」（只读 + 产出计划）
	SkillIDs    []string `json:"skill_ids"`   // 本次临时勾选的 skill id（替代全局 enabled）
	Attachments []string `json:"attachments"` // @ 选中的文件/文件夹相对路径(workdir 下)
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
	log.Printf("[Chat] start session=%s provider=%s tools_enabled_req=%v use_rag_req=%v msg_len=%d",
		id, prov.ID, boolPtrForLog(req.ToolsEnabled), boolPtrForLog(req.UseRAG), len(req.Message))

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
		// 按确认规则设置 Gate：自动模式直接放行所有工具调用，手动模式逐次确认。
		h.Gate.SetAutoAllow(confirmModeSetting(h.DB) == confirmModeAuto)
		h.Gate.SetEmitter(func(req tools.ConfirmRequest) {
			log.Printf("[Chat] confirm_emit session=%s request_id=%s tool=%s args=%q",
				id, req.ID, req.Tool, truncateForLog(req.Args, 240))
			sse.Emit("confirm_req", map[string]any{
				"request_id":     req.ID,
				"tool":           req.Tool,
				"input":          map[string]any{"raw": req.Args},
				"match_key_hint": req.MatchKeyHint,
			})
			log.Printf("[Chat] confirm_emitted session=%s request_id=%s tool=%s", id, req.ID, req.Tool)
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
	if h.MCPConfigPath != "" {
		history = prependSystemMessage(history, "MCP server configuration is stored at "+h.MCPConfigPath+". When the user asks which MCP servers are configured, read that file directly instead of searching the filesystem.")
	}

	// 将 @ 选中的文件/文件夹注入到用户消息前部:文件读取内容(超限截断),
	// 文件夹展开为目录树。随用户消息持久化,下一轮 history 自动带上。
	if h.WorkDir != nil {
		if wd := h.WorkDir.Get(); wd != "" {
			if block := buildAttachments(wd, req.Attachments); block != "" {
				req.Message = block + req.Message
			}
		}
	}

	// persist user message
	now := time.Now().UTC().Format(time.RFC3339Nano)
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
	planMode := false
	if req.PlanMode != nil {
		planMode = *req.PlanMode
	}

	// 把全部已启用 MCP 挂到内置工具引擎之上。MCP 由设置页全局配置，配置后自动可用、
	// 模型自动调用——不再支持按对话勾选（对齐 Cursor/Windsurf/Trae 的主流设计）。
	chatEngine := mcp.AttachToEngine(h.Engine, h.MCP, nil)
	a := agent.New(agent.Deps{
		LLM: llmClient, Tools: chatEngine, RAG: h.RAG, Skills: h.Skills, Memory: h.Memory,
		MaxToolCalls: toolLimitSetting(h.DB),
	})
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Minute)
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

	runStart := time.Now()
	a.Run(ctx, agent.RunInput{
		History:      history,
		Emit:         emit,
		ToolsEnabled: toolsEnabled,
		UseRAG:       useRAG,
		KBID:         sess.KBID,
		UserMessage:  req.Message,
		PlanMode:     planMode,
		SkillIDs:     req.SkillIDs,
	})
	log.Printf("[Chat] agent_return session=%s duration=%s", id, time.Since(runStart).Round(time.Millisecond))

	persisted := 0
	for _, msg := range collected.finish() {
		_ = h.DB.AppendMessage(llmMsgToStore(id, msg))
		persisted++
	}
	log.Printf("[Chat] persisted_assistant_turns session=%s count=%d", id, persisted)

	// Wait for the concurrent title generation to settle (LLM or fallback)
	// before the stream closes, so the client always receives a title event.
	titleWg.Wait()
	log.Printf("[Chat] done session=%s", id)
}

func prependSystemMessage(history []llm.Message, content string) []llm.Message {
	system := llm.Message{Role: llm.RoleSystem, Content: content}
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		out := append([]llm.Message(nil), history...)
		out[0] = llm.Message{Role: llm.RoleSystem, Content: content + "\n" + history[0].Content}
		return out
	}
	return append([]llm.Message{system}, history...)
}

func boolPtrForLog(v *bool) string {
	if v == nil {
		return "<nil>"
	}
	if *v {
		return "true"
	}
	return "false"
}

func truncateForLog(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// titleClient returns the provider used for conversation-title generation.
// Titles reuse the default chat model (the one flagged "默认" on the settings
// page), so the call runs on its own connection in parallel with the reply.
// Falls back to the given chat client when no default chat model is configured.
func (h *ChatHandler) titleClient(fallback llm.LLMClient) llm.LLMClient {
	if def, err := h.DB.GetDefaultProviderByKind("chat"); err == nil && def.ChatModel != "" {
		return llm.NewOpenAIClient(llm.Config{
			BaseURL: def.BaseURL, APIKey: def.APIKey, Model: def.ChatModel,
		})
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
	// 反序列化 assistant 的 tool_calls（存储为 JSON 字符串）。
	// 之前这里漏掉了 ToolCalls，导致跨请求续聊时模型看不到自己上一轮调过
	// 哪些工具，上下文断裂、多步任务容易卡住。
	var toolCalls []llm.ToolCall
	if s := strings.TrimSpace(m.ToolCalls); s != "" {
		if err := json.Unmarshal([]byte(s), &toolCalls); err != nil {
			log.Printf("[Chat] decode tool_calls failed (msg=%s): %v", m.ID, err)
		}
	}
	return llm.Message{
		Role: llm.Role(m.Role), Content: m.Content,
		ToolCalls: toolCalls, ToolCallID: m.ToolCallID,
	}
}

type streamCollector struct {
	pendingText      strings.Builder
	pendingToolCalls []llm.ToolCall
	messages         []llm.Message
}

func (c *streamCollector) appendText(s string) {
	c.pendingText.WriteString(s)
}

func (c *streamCollector) appendToolCall(tc llm.ToolCall) {
	c.pendingToolCalls = append(c.pendingToolCalls, tc)
}

func (c *streamCollector) appendToolResult(callID, content string) {
	c.flushAssistantTurn()
	c.messages = append(c.messages, llm.Message{
		Role: llm.RoleTool, Content: content, ToolCallID: callID,
	})
}

func (c *streamCollector) finish() []llm.Message {
	c.flushAssistantTurn()
	return append([]llm.Message(nil), c.messages...)
}

func (c *streamCollector) flushAssistantTurn() {
	if c.pendingText.Len() == 0 && len(c.pendingToolCalls) == 0 {
		return
	}
	c.messages = append(c.messages, llm.Message{
		Role: llm.RoleAssistant, Content: c.pendingText.String(),
		ToolCalls: append([]llm.ToolCall(nil), c.pendingToolCalls...),
	})
	c.pendingText.Reset()
	c.pendingToolCalls = nil
}

// multiEmitter forwards events to the SSE client AND collects assistant output.
type multiEmitter struct {
	sse       *SSEWriter
	collector *streamCollector
}

func (m *multiEmitter) Emit(event string, data any) {
	switch event {
	case "delta":
		if d, ok := data.(map[string]any); ok {
			if t, ok := d["text"].(string); ok {
				m.collector.appendText(t)
			}
		}
	case "tool_call":
		if tc, ok := toolCallFromEvent(data); ok {
			m.collector.appendToolCall(tc)
		}
	case "tool_result":
		if callID, content, ok := toolResultFromEvent(data); ok {
			m.collector.appendToolResult(callID, content)
		}
	}
	m.sse.Emit(event, data)
}

func toolCallFromEvent(data any) (llm.ToolCall, bool) {
	d, ok := data.(map[string]any)
	if !ok {
		return llm.ToolCall{}, false
	}
	id, _ := d["call_id"].(string)
	name, _ := d["tool"].(string)
	input, _ := d["input"].(map[string]any)
	args, _ := input["raw"].(string)
	if id == "" || name == "" {
		return llm.ToolCall{}, false
	}
	return llm.ToolCall{ID: id, Name: name, Args: args}, true
}

func toolResultFromEvent(data any) (string, string, bool) {
	d, ok := data.(map[string]any)
	if !ok {
		return "", "", false
	}
	callID, _ := d["call_id"].(string)
	content, _ := d["content"].(string)
	if callID == "" {
		return "", "", false
	}
	return callID, content, true
}

func llmMsgToStore(sessionID string, msg llm.Message) store.Message {
	toolCalls := ""
	if len(msg.ToolCalls) > 0 {
		if b, err := json.Marshal(msg.ToolCalls); err == nil {
			toolCalls = string(b)
		}
	}
	return store.Message{
		ID:         "msg_" + ulid.Make().String(),
		SessionID:  sessionID,
		Role:       string(msg.Role),
		Content:    msg.Content,
		ToolCalls:  toolCalls,
		ToolCallID: msg.ToolCallID,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}
