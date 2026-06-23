package agent

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/tools"
)

// ragSimilarityThreshold drops retrieved chunks whose cosine similarity is below
// this value before injecting them into the prompt. Based on observation:
// relevant hits land ~0.45 while noise sits ~0.27-0.30, so 0.3 separates them.
// When everything falls below it, nothing is injected (the model answers from
// its own knowledge) instead of polluting the prompt with low-quality excerpts.
const ragSimilarityThreshold float32 = 0.3

// baseSystemPrompt 建立工具路由策略：明确告诉模型何时必须使用 MCP 工具。
// 没有它时，模型对图像理解、联网搜索等超出语言模型直接能力的请求，会默认
// 用 bash 等内置工具去绕，或干脆回答"我做不到"——即使对应的 mcp__ 工具
// 已经可用。这里把"先判断任务类型、超出自身能力就用 MCP"作为硬性规则注入。
const baseSystemPrompt = `你是一个配备了工具集的 AI 助手。你的工具分为两类：

1. 内置工具（bash、file_read、file_write、file_edit、grep）：用于在用户工作目录中执行命令、读写文件与检索内容。
2. MCP 扩展工具（工具名以 mcp__ 开头，形如 mcp__<服务>__<能力>）：这些是你自身语言模型能力之外的扩展能力，例如图像/视觉理解、联网搜索、网页阅读、链接深度阅读等。

工具使用原则（务必遵守）：
- 先判断任务类型，再选工具：文件与命令操作用内置工具；任何超出语言模型直接能力的请求，都必须调用对应的 MCP 工具来完成。
- 当用户要求识别或理解图片、搜索互联网上的最新信息、读取某个网页、深度阅读某个链接时，必须优先调用对应的 mcp__ 工具，不得用 bash 等内置工具变通，也不得直接回答“我无法做到 / 我没有这个能力”。
- 仅当确实没有任何合适的 MCP 工具可用时，才如实告知用户当前缺少哪一类能力。

记住：永远不要因为“我没有这个能力”就放弃——先检查可用的 mcp__ 工具，它们正是为你补充这些能力而存在的。

工作节奏（重要，避免中途停下）：
- 在用户的任务真正完成之前，每一轮都必须调用工具继续推进；不要只输出中间的思考、分析或计划文本就停下。
- 想清楚后直接用工具行动：例如要改文件就直接调用 file_edit，不要先输出一段“我打算这样改……”然后停下来等下一轮。
- 只有当任务已经完成、可以给用户最终答复时，才输出不含工具调用的总结性回答。`

type Agent struct {
	deps Deps
}

func New(deps Deps) *Agent {
	if deps.MaxIter <= 0 {
		deps.MaxIter = 20
	}
	if deps.LLMRetryWait <= 0 {
		deps.LLMRetryWait = 30 * time.Second
	}
	return &Agent{deps: deps}
}

// AgentBuilder is a convenience for handlers/tests to construct an Agent
// progressively. Production wiring uses New(Deps{...}) directly in main.
type AgentBuilder struct {
	LLM   llm.LLMClient
	Tools *tools.Engine
	RAG   RAGRetriever
}

func (b AgentBuilder) Build() *Agent {
	return New(Deps{LLM: b.LLM, Tools: b.Tools, RAG: b.RAG})
}

// RunInput is one invocation of the agent loop.
type RunInput struct {
	History      []llm.Message // current conversation (will be appended to)
	Emit         EventEmitter
	ToolsEnabled bool
	UseRAG       bool
	KBID         string // required when UseRAG
	UserMessage  string // the new user turn (appended to History if non-empty)
}

// Run executes the agent loop and emits events.
func (a *Agent) Run(ctx context.Context, in RunInput) {
	history := in.History
	if in.UserMessage != "" {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: in.UserMessage})
	}
	if a.deps.Skills != nil {
		if instructions, err := a.deps.Skills.EnabledInstructions(); err == nil && strings.TrimSpace(instructions) != "" {
			history = prependSystemContext(history, instructions)
		} else if err != nil {
			log.Printf("[Skills] load failed: %v", err)
		}
	}

	// 注入 base 系统提示词：建立工具路由策略，让模型在遇到图像理解、
	// 联网搜索等超出自身能力的请求时主动调用 mcp__ 工具，而非用 bash 绕过。
	// 在 skills 之后 prepend，使其排在最终 system 内容最前（最高优先级）。
	history = prependSystemContext(history, baseSystemPrompt)

	var toolSpecs []llm.ToolSpec
	if in.ToolsEnabled && a.deps.Tools != nil {
		for _, s := range a.deps.Tools.List() {
			toolSpecs = append(toolSpecs, llm.ToolSpec{
				Name: s.Name, Description: s.Description, Parameters: s.Parameters,
			})
		}
	}

	for iter := 0; ; iter++ {
		if iter > 0 && iter%a.deps.MaxIter == 0 {
			log.Printf("[Agent] checkpoint: reached %d tool iterations; continuing until task completes", iter)
			in.Emit.Emit("status", map[string]any{
				"kind":    "tool_iteration_checkpoint",
				"message": "工具调用已达到一个安全检查点，任务尚未完成，继续执行后续工具调用。",
				"iter":    iter,
			})
		}

		// Optionally inject RAG context before the model turn. Low-similarity
		// chunks are filtered out; if none clear the threshold, nothing is
		// injected so the model answers from its own knowledge.
		if in.UseRAG && a.deps.RAG != nil && in.KBID != "" {
			query := lastUserText(history)
			chunks, err := a.deps.RAG.Retrieve(ctx, in.KBID, query, 5)
			if err == nil {
				kept := filterRAGChunks(chunks, ragSimilarityThreshold)
				// log the retrieval once per turn (query is stable across tool
				// iterations) so the RAG pipeline can be debugged/tuned.
				if iter == 0 {
					logRAGRetrieval(query, chunks, kept, ragSimilarityThreshold)
				}
				if len(kept) > 0 {
					history = prependRAGContext(history, kept)
				}
			}
		}

		stream, streamStart, ok := a.openChatStreamWithRecovery(ctx, in.Emit, iter, history, toolSpecs)
		if !ok {
			return
		}

		var assistantText strings.Builder
		var toolCalls []llm.ToolCall
		var usage *llm.Usage
		chunks := 0
		sawDone := false
		for chunk := range stream {
			chunks++
			if chunk.Text != "" {
				assistantText.WriteString(chunk.Text)
				in.Emit.Emit("delta", map[string]any{"text": chunk.Text})
			}
			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, *chunk.ToolCall)
				in.Emit.Emit("tool_call", map[string]any{
					"call_id": chunk.ToolCall.ID,
					"tool":    chunk.ToolCall.Name,
					"input":   map[string]any{"raw": chunk.ToolCall.Args},
				})
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			if chunk.Done {
				sawDone = true
				break
			}
		}

		log.Printf("[Agent] iter=%d llm_done duration=%s chunks=%d saw_done=%t text_len=%d tool_calls=%d usage=%+v",
			iter, time.Since(streamStart).Round(time.Millisecond), chunks, sawDone, assistantText.Len(), len(toolCalls), usage)

		if !sawDone {
			if ctx.Err() != nil {
				log.Printf("[Agent] incomplete stream at iter=%d due to context error: %v", iter, ctx.Err())
				in.Emit.Emit("error", map[string]any{"message": "任务超时或被取消：" + ctx.Err().Error()})
				return
			}
			log.Printf("[Agent] incomplete stream at iter=%d; continuing original task, preview=%q", iter, truncate(assistantText.String(), 200))
			in.Emit.Emit("status", map[string]any{
				"kind":    "llm_incomplete_stream",
				"message": "模型响应流未完整结束，正在继续原任务。",
			})
			if len(toolCalls) == 0 {
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: incompleteStreamContinuationPrompt(),
				})
				continue
			}
		}

		if len(toolCalls) == 0 {
			if strings.TrimSpace(assistantText.String()) == "" {
				log.Printf("[Agent] blank assistant turn at iter=%d; continuing original task", iter)
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: blankAssistantContinuationPrompt(),
				})
				continue
			}
			history = append(history, llm.Message{
				Role: llm.RoleAssistant, Content: assistantText.String(),
			})
			// pure text answer; terminate. 记录预览，便于诊断“为什么停在这一轮”
			log.Printf("[Agent] stop: no tool call at iter=%d, preview=%q", iter, truncate(assistantText.String(), 200))
			in.Emit.Emit("done", map[string]any{"usage": usage})
			return
		}

		// record assistant tool-call turn before feeding tool results back.
		history = append(history, llm.Message{
			Role: llm.RoleAssistant, Content: assistantText.String(), ToolCalls: toolCalls,
		})

		// execute each tool, feed result back
		for i, tc := range toolCalls {
			toolStart := time.Now()
			log.Printf("[Agent] tool_start iter=%d index=%d id=%s name=%s args=%q",
				iter, i, tc.ID, tc.Name, truncate(tc.Args, 240))
			result := tools.Result{Content: "no tools available", IsError: true}
			var execErr error
			if a.deps.Tools != nil {
				r, err := a.deps.Tools.Execute(ctx, tc.Name, tc.Args)
				if err != nil {
					execErr = err
					result = tools.Result{Content: err.Error(), IsError: true}
				} else {
					result = r
				}
			}
			log.Printf("[Agent] tool_done iter=%d index=%d id=%s name=%s duration=%s is_error=%t content_len=%d exec_err=%v",
				iter, i, tc.ID, tc.Name, time.Since(toolStart).Round(time.Millisecond), result.IsError, len(result.Content), execErr)
			in.Emit.Emit("tool_result", map[string]any{
				"call_id": tc.ID, "content": result.Content, "is_error": result.IsError,
			})
			history = append(history, llm.Message{
				Role: llm.RoleTool, Content: result.Content, ToolCallID: tc.ID,
			})
		}
	}
}

func (a *Agent) openChatStreamWithRecovery(ctx context.Context, emit EventEmitter, iter int, history []llm.Message, toolSpecs []llm.ToolSpec) (<-chan llm.Chunk, time.Time, bool) {
	attempt := 0
	for {
		attempt++
		log.Printf("[Agent] iter=%d llm_start attempt=%d history=%d tools=%d last_user=%q",
			iter, attempt, len(history), len(toolSpecs), truncate(lastUserText(history), 160))
		streamStart := time.Now()
		stream, err := a.deps.LLM.ChatStream(ctx, history, toolSpecs)
		if err == nil {
			return stream, streamStart, true
		}
		if !isRecoverableLLMError(err) {
			log.Printf("[Agent] llm error at iter=%d attempt=%d: %v", iter, attempt, err)
			emit.Emit("error", map[string]any{"message": err.Error()})
			return nil, time.Time{}, false
		}
		wait := a.deps.LLMRetryWait
		log.Printf("[Agent] llm recoverable error at iter=%d attempt=%d; retrying in %s: %v",
			iter, attempt, wait, err)
		emit.Emit("status", map[string]any{
			"kind":    "llm_retry",
			"message": "模型触发限流，正在等待后自动重试。",
			"attempt": attempt,
			"wait_ms": wait.Milliseconds(),
			"error":   err.Error(),
		})
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			log.Printf("[Agent] llm retry canceled at iter=%d attempt=%d: %v", iter, attempt, ctx.Err())
			emit.Emit("error", map[string]any{"message": ctx.Err().Error()})
			return nil, time.Time{}, false
		}
	}
}

func isRecoverableLLMError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "rate limiting") ||
		strings.Contains(msg, "TPM limit")
}

func blankAssistantContinuationPrompt() string {
	return "上一轮助手没有给出有效内容。请继续完成用户的原始任务：如果还需要数据或文件，请继续调用工具；只有在任务真正完成并给出明确结果后，才输出最终答复。"
}

func incompleteStreamContinuationPrompt() string {
	return "上一轮模型响应流没有完整结束，不能把其中的中间说明当成最终结果。请继续完成用户的原始任务；如果刚才只是说“将要撰写/将要生成”，现在必须继续调用工具或直接生成完整交付物。"
}

func lastUserText(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleUser {
			return history[i].Content
		}
	}
	return ""
}

// filterRAGChunks keeps chunks at or above the similarity threshold, preserving
// the similarity-descending order returned by the retriever.
func filterRAGChunks(chunks []RetrievedChunk, threshold float32) []RetrievedChunk {
	kept := make([]RetrievedChunk, 0, len(chunks))
	for _, c := range chunks {
		if c.Similarity >= threshold {
			kept = append(kept, c)
		}
	}
	return kept
}

// logRAGRetrieval prints the retrieval pipeline for debugging: every candidate
// with its similarity and INJECT/DROP verdict. Injected chunks are printed in
// full (that is exactly what the model sees in the prompt); dropped ones get a
// short preview, enough to judge why they missed and to tune the threshold.
func logRAGRetrieval(query string, retrieved, kept []RetrievedChunk, threshold float32) {
	log.Printf("[RAG] query=%q threshold=%.2f candidates=%d injected=%d",
		query, threshold, len(retrieved), len(kept))
	keptSet := make(map[string]bool, len(kept))
	for _, c := range kept {
		keptSet[c.ID] = true
	}
	for i, c := range retrieved {
		flag := "DROP"
		text := c.Text
		if keptSet[c.ID] {
			flag = "INJECT"
		} else {
			text = truncate(strings.TrimSpace(c.Text), 100)
		}
		log.Printf("[RAG] #%d [%s] sim=%.3f %s\n%s", i+1, flag, c.Similarity, c.Filename, text)
	}
}

// truncate clips s to at most n runes (unicode-safe) with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func prependRAGContext(history []llm.Message, chunks []RetrievedChunk) []llm.Message {
	var sb strings.Builder
	sb.WriteString("Answer using the following knowledge base excerpts where relevant:\n\n")
	for i, c := range chunks {
		sb.WriteString("--- excerpt ")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(" (")
		sb.WriteString(c.Filename)
		sb.WriteString(", chunk ")
		sb.WriteString(c.ID)
		sb.WriteString(") ---\n")
		sb.WriteString(c.Text)
		sb.WriteString("\n\n")
	}
	system := llm.Message{Role: llm.RoleSystem, Content: sb.String()}
	return prependSystemMessage(history, system)
}

func prependSystemContext(history []llm.Message, content string) []llm.Message {
	return prependSystemMessage(history, llm.Message{Role: llm.RoleSystem, Content: content})
}

func prependSystemMessage(history []llm.Message, system llm.Message) []llm.Message {
	// merge with existing system message if present, else prepend
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		merged := history[0]
		merged.Content = system.Content + "\n" + history[0].Content
		out := make([]llm.Message, len(history))
		copy(out, history)
		out[0] = merged
		return out
	}
	return append([]llm.Message{system}, history...)
}
