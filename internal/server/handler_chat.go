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
	Gate          *tools.Gate         // wires tool confirmations onto this chat's SSE stream
	Engine        *tools.Engine       // 纯内置工具引擎（未 attach mcp）
	MCP           *mcp.Manager        // 按请求 attach mcp；nil 则不挂载 MCP 工具
	RAG           agent.RAGRetriever  // optional; nil disables RAG
	Skills        agent.SkillProvider // optional; nil disables skills
	MCPConfigPath string
	WorkDir       *tools.WorkDir       // optional; nil disables @ file-attachment injection
	Memory        agent.MemoryProvider // optional; nil disables memory injection
}

func (h *ChatHandler) Routes(r chi.Router) {
	r.Post("/sessions/{id}/chat", h.Chat)
}

type chatRequest struct {
	Message       string   `json:"message"`
	ToolsEnabled  *bool    `json:"tools_enabled"`
	UseRAG        *bool    `json:"use_rag"`
	ProviderID    *string  `json:"provider_id"`               // optional override; falls back to session's provider
	PlanMode      *bool    `json:"plan_mode"`                 // 本次会话临时开启「计划模式」（只读 + 产出计划）
	SkillIDs      []string `json:"skill_ids"`                 // 本次临时勾选的 skill id（替代全局 enabled）
	Attachments   []string `json:"attachments"`               // @ 选中的文件/文件夹相对路径(workdir 下)
	Regenerate    bool     `json:"regenerate"`                // 重新回答：截断末尾 user 之后的内容并重新生成
	EditMessageID string   `json:"edit_message_id,omitempty"` // 编辑重发：定位指定 user，截断其后、改写其文本后重新生成
	Images        []string `json:"images,omitempty"`          // 用户粘贴的图片 dataURL（多模态，仅视觉模型）
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
		history = append(history, storeMsgToLLM(m, prov.Vision))
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
	if req.Regenerate {
		// 重新回答：不重新持久化 user 消息，删除末尾 user 之后的所有旧回答后重新生成。
		// 这要求历史里至少有一条 user；若没有则降级为普通发送。
		if lu := lastUserMessage(storedMsgs); lu.ID != "" {
			if err := h.DB.DeleteMessagesFrom(id, lu.CreatedAt); err != nil {
				log.Printf("[Chat] regenerate truncate failed session=%s: %v", id, err)
			}
			history = history[:0]
			kept, _ := h.DB.ListMessages(id)
			for _, m := range kept {
				history = append(history, storeMsgToLLM(m, prov.Vision))
			}
			// 重新回答时不重复注入标题生成（首条消息已存在）
			req.Message = ""
		}
	}

	// 编辑重发：定位指定 user 消息，删除其后所有内容并改写其正文/图片，再重新生成。
	// 与 regenerate 对称——本轮 user 由重建后的 history 末尾承担，故 req.Message 置空避免重复持久化。
	if req.EditMessageID != "" {
		if em := findMessageByID(storedMsgs, req.EditMessageID); em.ID != "" && em.Role == "user" {
			if err := h.DB.DeleteMessagesFrom(id, em.CreatedAt); err != nil {
				log.Printf("[Chat] edit truncate failed session=%s: %v", id, err)
			}
			userImages := ""
			if len(req.Images) > 0 {
				if b, err := json.Marshal(req.Images); err == nil {
					userImages = string(b)
				}
			}
			if err := h.DB.UpdateMessageContent(id, req.EditMessageID, req.Message, userImages); err != nil {
				log.Printf("[Chat] edit update failed session=%s: %v", id, err)
			}
			history = history[:0]
			kept, _ := h.DB.ListMessages(id)
			for _, m := range kept {
				history = append(history, storeMsgToLLM(m, prov.Vision))
			}
			req.Message = ""
		} else {
			log.Printf("[Chat] edit target not found or not user session=%s msg=%s", id, req.EditMessageID)
		}
	}

	if req.Message != "" {
		userMsgID := "msg_" + ulid.Make().String()
		userImages := ""
		if len(req.Images) > 0 {
			if b, err := json.Marshal(req.Images); err == nil {
				userImages = string(b)
			}
		}
		_ = h.DB.AppendMessage(store.Message{
			ID: userMsgID, SessionID: id, Role: "user", Content: req.Message,
			Images: userImages, CreatedAt: now,
		})
		// 回传真实 user 消息 id：前端乐观消息用的是 pending- 临时 id，收到后替换为
		// 真实 id，才能在「编辑重发」时按 id 精确定位（见 chatRequest.EditMessageID）。
		sse.Emit("user_saved", map[string]any{"user_msg_id": userMsgID})
		// 注意：此处只持久化 user 消息，不把它 append 进 history——本轮 user 由
		// agent.Run 的 UserMessage 统一追加（见 agent.go），否则模型会收到两条重复
		// 的 user 消息。firstMessage 已由 len(storedMsgs)==0 判定，不依赖 history 长度。
	}

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
		MaxToolCalls:  toolLimitSetting(h.DB),
		ContextWindow: effectiveContextWindow(prov),
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
		UserImages:   toImageRefs(req.Images),
		Vision:       prov.Vision,
		PlanMode:     planMode,
		SkillIDs:     req.SkillIDs,
	})
	log.Printf("[Chat] agent_return session=%s duration=%s", id, time.Since(runStart).Round(time.Millisecond))

	persisted := 0
	finished := collected.finish()
	// 把 provider 上报的真实 usage 回填到最后一条 assistant 消息；
	// 这是持久化用量，仅作用于「无工具调用」收尾的那条纯文本回答。
	for i, msg := range finished {
		isLastAssistant := i == len(finished)-1 && msg.Role == llm.RoleAssistant
		storeMsg := llmMsgToStore(id, msg)
		if isLastAssistant && collected.lastUsage != nil {
			storeMsg.TokensIn = collected.lastUsage.InputTokens
			storeMsg.TokensOut = collected.lastUsage.OutputTokens
		}
		_ = h.DB.AppendMessage(storeMsg)
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

// lastUserMessage 返回消息列表中最后一条 user 消息；没有则返回空 Message。
// 用于「重新回答」定位截断点：删除该 user 消息之后的所有内容。
func lastUserMessage(msgs []store.Message) store.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i]
		}
	}
	return store.Message{}
}

// findMessageByID 在消息列表中按 id 查找；找不到返回空 Message。用于「编辑重发」定位目标 user。
func findMessageByID(msgs []store.Message, id string) store.Message {
	for _, m := range msgs {
		if m.ID == id {
			return m
		}
	}
	return store.Message{}
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

// effectiveContextWindow 返回 provider 的上下文窗口大小；未配置（<=0）时回退全局默认 200000。
func effectiveContextWindow(p store.Provider) int {
	if p.ContextWindow > 0 {
		return p.ContextWindow
	}
	return 200000
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

func storeMsgToLLM(m store.Message, vision bool) llm.Message {
	// 反序列化 assistant 的 tool_calls（存储为 JSON 字符串）。
	// 之前这里漏掉了 ToolCalls，导致跨请求续聊时模型看不到自己上一轮调过
	// 哪些工具，上下文断裂、多步任务容易卡住。
	var toolCalls []llm.ToolCall
	if s := strings.TrimSpace(m.ToolCalls); s != "" {
		if err := json.Unmarshal([]byte(s), &toolCalls); err != nil {
			log.Printf("[Chat] decode tool_calls failed (msg=%s): %v", m.ID, err)
		}
	}
	role := llm.Role(m.Role)
	// 历史压缩产生的摘要以 summary 角色持久化；回放给 LLM 时改写为 system，
	// 让模型把它当作背景上下文，而非一条非法的对话角色。
	if role == "summary" {
		role = llm.RoleSystem
	}
	msg := llm.Message{
		Role: role, Content: m.Content,
		ToolCalls: toolCalls, ToolCallID: m.ToolCallID,
	}
	// 仅当当前模型支持视觉时，才把历史用户图片回填为多模态 content；
	// 否则纯文本模型续聊带图历史会被 provider 以 400 拒绝。
	if vision && m.Role == string(llm.RoleUser) {
		if s := strings.TrimSpace(m.Images); s != "" {
			var urls []string
			if err := json.Unmarshal([]byte(s), &urls); err == nil {
				msg.Images = toImageRefs(urls)
			}
		}
	}
	return msg
}

// toImageRefs 把 dataURL 字符串切片转为多模态 ImageRef；空输入返回 nil。
func toImageRefs(urls []string) []llm.ImageRef {
	if len(urls) == 0 {
		return nil
	}
	out := make([]llm.ImageRef, 0, len(urls))
	for _, u := range urls {
		if u != "" {
			out = append(out, llm.ImageRef{DataURL: u})
		}
	}
	return out
}

type streamCollector struct {
	pendingText      strings.Builder
	pendingThinking  strings.Builder
	pendingToolCalls []llm.ToolCall
	messages         []llm.Message
	lastUsage        *llm.Usage // provider 在 done 事件里上报的真实用量；nil 表示未上报
}

func (c *streamCollector) appendText(s string) {
	c.pendingText.WriteString(s)
}

func (c *streamCollector) appendThinking(s string) {
	c.pendingThinking.WriteString(s)
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
	// 守卫须包含 pendingThinking：否则「纯思考、无正文也无工具调用」的轮次会被吞掉，思考丢失。
	if c.pendingText.Len() == 0 && c.pendingThinking.Len() == 0 && len(c.pendingToolCalls) == 0 {
		return
	}
	c.messages = append(c.messages, llm.Message{
		Role: llm.RoleAssistant, Content: c.pendingText.String(),
		Thinking:  c.pendingThinking.String(),
		ToolCalls: append([]llm.ToolCall(nil), c.pendingToolCalls...),
	})
	c.pendingText.Reset()
	c.pendingThinking.Reset()
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
	case "thinking":
		if d, ok := data.(map[string]any); ok {
			if t, ok := d["text"].(string); ok {
				m.collector.appendThinking(t)
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
	case "done":
		// done 事件携带 provider 上报的真实 usage；缓存下来，持久化时回填到最后一条 assistant。
		if d, ok := data.(map[string]any); ok {
			if u, ok := d["usage"].(*llm.Usage); ok {
				m.collector.lastUsage = u
			}
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
		Thinking:   msg.Thinking,
		ToolCalls:  toolCalls,
		ToolCallID: msg.ToolCallID,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}
