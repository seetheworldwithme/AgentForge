package agent

import (
	"context"
	"fmt"
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

// planSystemPrompt 用于「计划模式」：在动手改任何东西之前先产出可执行计划。
// 该模式下 Agent 只做只读调研（读文件、grep、只读 bash），严禁修改文件或执行有
// 副作用的命令，最终交付物是一份结构化的实施计划而非代码改动。
const planSystemPrompt = `你当前处于「计划模式」。用户希望：在动手改任何东西之前，先给出一份可执行的实施计划。

执行要求：
1. 充分调研：用 file_read、grep、bash（仅限只读命令：ls/cat/git status/git log 等）通读相关代码、配置与目录结构，真正理解现状、依赖与约束，不要凭空假设。
2. 严禁修改：绝不调用 file_write/file_edit，也不执行任何有写副作用或破坏性的命令（rm、写入、安装等）。本模式只读。
3. 产出结构化计划，至少包含：
   - 目标：一句话说清要达成什么。
   - 现状分析：基于调研指出相关文件、现有实现、关键约束。
   - 改动清单：逐项列出要改的文件与具体改动点（新建/修改/删除）。
   - 实施步骤：可分步执行的顺序，标注每步的验证方式。
   - 风险与取舍：可能出错处、为何选此方案。
4. 调研可多轮工具调用推进；调研充分后一次性输出完整计划，不要中途停下。
本模式下唯一交付物是计划文本，不是代码改动。`

type Agent struct {
	deps Deps
}

func New(deps Deps) *Agent {
	if deps.MaxIter <= 0 {
		deps.MaxIter = 20
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
	// PlanMode 开启「计划模式」：注入只读调研 + 产出结构化计划的系统提示词，
	// 并从暴露给模型的工具中过滤掉 file_write/file_edit。本模式只读，不改文件。
	PlanMode bool
	// SkillIDs 非空时，仅注入这些 id 的 skill 指令（替代全局 enabled）；为空时
	// 维持全局 enabled 行为。
	SkillIDs []string
}

// Run executes the agent loop and emits events.
func (a *Agent) Run(ctx context.Context, in RunInput) {
	history := in.History
	if in.UserMessage != "" {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: in.UserMessage})
	}
	if a.deps.Skills != nil {
		// 临时勾选优先：本次指定了 SkillIDs 时只注入这些 skill（替代全局 enabled）。
		var instructions string
		var err error
		if len(in.SkillIDs) > 0 {
			instructions, err = a.deps.Skills.InstructionsFor(in.SkillIDs)
		} else {
			instructions, err = a.deps.Skills.EnabledInstructions()
		}
		if err == nil && strings.TrimSpace(instructions) != "" {
			history = prependSystemContext(history, instructions)
		} else if err != nil {
			log.Printf("[Skills] load failed: %v", err)
		}
	}

	// 注入 base 系统提示词：建立工具路由策略，让模型在遇到图像理解、
	// 联网搜索等超出自身能力的请求时主动调用 mcp__ 工具，而非用 bash 绕过。
	// 在 skills 之后 prepend，使其排在最终 system 内容最前（最高优先级）。
	history = prependSystemContext(history, baseSystemPrompt)

	// 计划模式：在 base 之后 prepend，使计划约束排在 system 内容最前（最高优先级）。
	if in.PlanMode {
		history = prependSystemContext(history, planSystemPrompt)
	}

	// 注入 RAG 上下文（一次性）：用户问题在整个工具迭代过程中稳定，因此检索
	// 与注入只在循环前做一次，避免每轮重复检索、以及 system prompt 随迭代次数
	// 累积膨胀。低相似度片段被过滤；若全部低于阈值则不注入，由模型凭自身知识作答。
	if in.UseRAG && a.deps.RAG != nil && in.KBID != "" {
		query := lastUserText(history)
		chunks, err := a.deps.RAG.Retrieve(ctx, in.KBID, query, 5)
		if err == nil {
			kept := filterRAGChunks(chunks, ragSimilarityThreshold)
			logRAGRetrieval(query, chunks, kept, ragSimilarityThreshold)
			if len(kept) > 0 {
				history = prependRAGContext(history, kept)
			}
		} else {
			log.Printf("[RAG] retrieve failed: %v", err)
		}
	}

	var toolSpecs []llm.ToolSpec
	if in.ToolsEnabled && a.deps.Tools != nil {
		for _, s := range a.deps.Tools.List() {
			// 计划模式只读：屏蔽写文件工具，防止 Agent 借机修改项目。
			if in.PlanMode && (s.Name == "file_write" || s.Name == "file_edit") {
				continue
			}
			toolSpecs = append(toolSpecs, llm.ToolSpec{
				Name: s.Name, Description: s.Description, Parameters: s.Parameters,
			})
		}
	}

	// toolCallCount 累计本轮 Run 中已执行的工具调用次数，用于 MaxToolCalls 硬上限判定。
	toolCallCount := 0
	for iter := 0; ; iter++ {
		// 显式响应取消：避免在 context 已取消时仍发起新一轮 LLM 调用。
		if err := ctx.Err(); err != nil {
			log.Printf("[Agent] abort at iter=%d due to context error: %v", iter, err)
			in.Emit.Emit("error", map[string]any{"message": "任务超时或被取消：" + err.Error()})
			return
		}

		if iter > 0 && iter%a.deps.MaxIter == 0 {
			log.Printf("[Agent] checkpoint: reached %d tool iterations; continuing until task completes", iter)
			in.Emit.Emit("status", map[string]any{
				"kind":    "tool_iteration_checkpoint",
				"message": "工具调用已达到一个安全检查点，任务尚未完成，继续执行后续工具调用。",
				"iter":    iter,
			})
		}

		stream, streamStart, ok := a.openChatStream(ctx, in.Emit, iter, history, toolSpecs)
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

		// 工具调用硬上限：已达上限时不再执行新的工具调用。把模型本轮的
		// tool-call turn 记入历史，并为每个调用补一条"未执行"结果（保持消息
		// 序列合法），发一条警告 status，然后继续——模型下一轮读到"未执行"
		// 提示后会基于已有结果给出最终文本回答。
		if a.deps.MaxToolCalls > 0 && toolCallCount >= a.deps.MaxToolCalls {
			log.Printf("[Agent] tool_limit_reached at iter=%d count=%d limit=%d", iter, toolCallCount, a.deps.MaxToolCalls)
			in.Emit.Emit("status", map[string]any{
				"kind":    "tool_limit_reached",
				"message": fmt.Sprintf("已达到工具调用上限（%d 次），不再执行新的工具调用，模型将基于已有结果给出最终回答。", a.deps.MaxToolCalls),
				"limit":   a.deps.MaxToolCalls,
				"count":   toolCallCount,
			})
			history = append(history, llm.Message{
				Role: llm.RoleAssistant, Content: assistantText.String(), ToolCalls: toolCalls,
			})
			skipped := "工具调用已达上限，本次未执行。请基于已有结果直接给出最终回答，不要再调用工具。"
			for _, tc := range toolCalls {
				in.Emit.Emit("tool_result", map[string]any{
					"call_id": tc.ID, "content": skipped, "is_error": true,
				})
				history = append(history, llm.Message{
					Role: llm.RoleTool, Content: skipped, ToolCallID: tc.ID,
				})
			}
			continue
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
		toolCallCount += len(toolCalls)
	}
}

// openChatStream 发起一轮模型流式调用。瞬态错误（429/5xx）的重试已由
// llm.Retry 在更底层统一处理，这里不再做二次重试——避免与底层重试叠加，
// 在 provider 持续限流时陷入无限等待。
func (a *Agent) openChatStream(ctx context.Context, emit EventEmitter, iter int, history []llm.Message, toolSpecs []llm.ToolSpec) (<-chan llm.Chunk, time.Time, bool) {
	log.Printf("[Agent] iter=%d llm_start history=%d tools=%d last_user=%q",
		iter, len(history), len(toolSpecs), truncate(lastUserText(history), 160))
	streamStart := time.Now()
	stream, err := a.deps.LLM.ChatStream(ctx, history, toolSpecs)
	if err != nil {
		log.Printf("[Agent] llm error at iter=%d: %v", iter, err)
		emit.Emit("error", map[string]any{"message": err.Error()})
		return nil, time.Time{}, false
	}
	return stream, streamStart, true
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
	if n <= 0 {
		return s
	}
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
