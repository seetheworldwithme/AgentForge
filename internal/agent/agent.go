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

// defaultContextWindow 是 provider 未配置 context_window 时的全局兜底，当前主流模型普遍 >=200K。
const defaultContextWindow = 200000

// baseSystemPrompt 建立工具路由策略：明确告诉模型何时必须使用 MCP 工具。
// 没有它时，模型对图像理解、联网搜索等超出语言模型直接能力的请求，会默认
// 用 bash 等内置工具去绕，或干脆回答"我做不到"——即使对应的 mcp__ 工具
// 已经可用。这里把"先判断任务类型、超出自身能力就用 MCP"作为硬性规则注入。
const baseSystemPrompt = `你是一个配备了工具集的 AI 助手。你的工具分为两类：

1. 内置工具（bash、file_read、file_write、file_edit、grep、read_skill）：用于在用户工作目录中执行命令、读写文件与检索内容；read_skill 用于按需加载某个 skill 的完整指令——系统已注入精简的 skills 索引，需要某个 skill 时按其 id 调用 read_skill 取全文再执行。
2. MCP 扩展工具（工具名以 mcp__ 开头，形如 mcp__<服务>__<能力>）：这些是你自身语言模型能力之外的扩展能力，例如图像/视觉理解、联网搜索、网页阅读、链接深度阅读等。

工具使用原则（务必遵守）：
- 先判断任务类型，再选工具：文件与命令操作用内置工具；任何超出语言模型直接能力的请求，都必须调用对应的 MCP 工具来完成。
- 当用户要求识别或理解图片时：如果你已在消息中直接收到图片内容（多模态），请直接基于看到的图片回答，不要调用图片识别类 mcp__ 工具；只有当你没有直接收到图片时，才调用对应的 mcp__ 图片识别工具。
- 当用户要求搜索互联网上的最新信息、读取某个网页、深度阅读某个链接时，必须优先调用对应的 mcp__ 工具，不得用 bash 等内置工具变通，也不得直接回答“我无法做到 / 我没有这个能力”。
- 仅当确实没有任何合适的 MCP 工具可用时，才如实告知用户当前缺少哪一类能力。

记住：永远不要因为“我没有这个能力”就放弃——先检查可用的 mcp__ 工具，它们正是为你补充这些能力而存在的。

工作目录与路径定位（务必遵守）：
- bash、grep、file_* 等命令默认在用户选定的「当前工作目录」下运行，该目录即用户打开的工作空间根。「工作目录 / 当前工作目录 / 项目根目录 / 当前工作空间 / 当前目录」都是同一个意思，均指当前工作目录，绝不要把它理解成用户家目录（如 /Users/xxx）或某个全局路径。
- 因此涉及“当前工作目录”的请求直接用相对路径操作即可（如 ls .agentforge/skills、grep -r "x" .），不要从家目录起 find 全盘搜索；需要确认路径时可先执行 pwd。
- skills 只存在于当前工作目录的 .agentforge/skills 与全局 ~/.agentforge/skills 下，不要去 .agents/skills、.claude/skills 等其它工具的目录里找——那些不是本工具的 skills。
- 关于“当前工作目录有几个 / 列表是什么 / 内容是否一致”的结论，必须基于实际命令输出，未看到输出不得臆断。

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

// visionSystemPrompt 用于本身具备视觉能力的多模态模型：明确告诉它图片已直接发来，
// 直接看图回答即可，不要调用图片识别类 MCP——避免与 baseSystemPrompt 中"图片用 MCP"
// 的兜底策略、"工作节奏"中"每轮都调工具"的指令冲突。
const visionSystemPrompt = `【视觉能力优先——本段优先级最高，覆盖任何要求你调用工具的其它指示】
你本身是一个具备视觉（多模态）能力的模型。当用户在消息中附带图片时，图片已作为图像内容直接发送给你，你能直接、完整地"看到"它。

涉及图片的任务（识别 / 提取图片文字、描述图片内容、分析截图 / 图表 / 界面、回答关于图片的提问等）都是一步即可完成的：
- 直接基于你看到的图片给出文字回答——给出回答即视为任务完成，必须立即停止：本轮不要再调用任何工具，也不要再继续下一轮。
- 绝不要调用 mcp__ 开头的图片识别 / OCR / 截图解析工具（它们是给纯文本模型补充视觉能力用的，对你完全多余，且因为没有真实文件路径必然失败），也不要向用户索要图片的文件路径或 URL——你已直接收到图片本身。
- 不要因为"工作节奏 / 每轮都要调用工具推进"之类的通用指示而对图片任务反复调用工具：图片理解看一眼即完成，无需任何工具。

mcp__ 工具仅用于视觉之外的能力（如联网搜索、网页阅读等）。仅当你在消息中确实没有看到任何图片时，才考虑其它方式。`

type Agent struct {
	deps Deps
	// noDispatch 为 true 时本 agent 是子 agent：不向模型暴露 dispatch_agent 工具，
	// 从工具可见性层面杜绝递归套娃。主 agent（New 构造）为零值 false，可派生子 agent。
	noDispatch bool
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
	// UserImages 是本轮用户消息附带的多模态图片（dataURL），随 UserMessage 一起
	// append 到 history，由 openai.go 转成 image_url 发给视觉模型。
	UserImages []llm.ImageRef
	// PlanMode 开启「计划模式」：注入只读调研 + 产出结构化计划的系统提示词，
	// 并从暴露给模型的工具中过滤掉 file_write/file_edit。本模式只读，不改文件。
	PlanMode bool
	// SkillIDs 非空时，仅注入这些 id 的 skill 指令（替代全局 enabled）；为空时
	// 维持全局 enabled 行为。
	SkillIDs []string
	// Vision 表示当前模型本身具备视觉（多模态）能力。为 true 时注入 visionSystemPrompt，
	// 让模型直接看用户附带的图片，而非调用图片识别类 MCP。
	Vision bool
}

// Run executes the agent loop and emits events.
func (a *Agent) Run(ctx context.Context, in RunInput) {
	history := in.History
	if in.UserMessage != "" {
		history = append(history, llm.Message{
			Role: llm.RoleUser, Content: in.UserMessage, Images: in.UserImages,
		})
	}
	if a.deps.Skills != nil {
		// 勾选优先：本次指定 SkillIDs 时直接全文注入这些 skill；否则只注入精简索引，
		// 模型按需用 read_skill 工具加载全文——避免每轮都把所有 SKILL.md 全文塞进 prompt。
		var instructions string
		var err error
		if len(in.SkillIDs) > 0 {
			instructions, err = a.deps.Skills.InstructionsFor(in.SkillIDs)
		} else {
			instructions, err = a.deps.Skills.IndexInstructions()
		}
		if err == nil && strings.TrimSpace(instructions) != "" {
			history = prependSystemContext(history, instructions)
		} else if err != nil {
			log.Printf("[Skills] load failed: %v", err)
		}
	}

	// 注入记忆索引：跨会话事实背景。放在 skills 之后、base 之前，
	// 使其在最终 system 内容中位于 base（工具路由策略）与 skills 之间。
	if a.deps.Memory != nil {
		if idx := a.deps.Memory.IndexContext(); strings.TrimSpace(idx) != "" {
			history = prependSystemContext(history, idx)
		}
	}

	// 注入项目规则：全局+项目 AGENTFORGE.md（始终）+ 兼容导入 CLAUDE.md/AGENTS.md（开关）。
	// 放在 memory 之后、base 之前 prepend，使其在最终 system 内容中位于 base 与 memory 之间。
	if a.deps.Rules != nil {
		if ctx := a.deps.Rules.RulesContext(); strings.TrimSpace(ctx) != "" {
			history = prependSystemContext(history, ctx)
		}
	}

	// 注入 base 系统提示词：建立工具路由策略，让模型在遇到图像理解、
	// 联网搜索等超出自身能力的请求时主动调用 mcp__ 工具，而非用 bash 绕过。
	// 在 skills 之后 prepend，使其排在最终 system 内容最前（最高优先级）。
	history = prependSystemContext(history, baseSystemPrompt)

	// 视觉模型：注入“直接看图”提示（排在 base 之前，最高优先级，覆盖 base 中“图片用 MCP”的兜底）。
	if in.Vision {
		history = prependSystemContext(history, visionSystemPrompt)
	}

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
			// 视觉模型 + 本轮含图片：屏蔽图片识别 / OCR 类 MCP——模型自己能看图，
			// 这类工具对它多余，且因没有真实文件路径必然失败。
			if in.Vision && len(in.UserImages) > 0 && isImageOCRTool(s.Name) {
				continue
			}
			toolSpecs = append(toolSpecs, llm.ToolSpec{
				Name: s.Name, Description: s.Description, Parameters: s.Parameters,
			})
		}
		// 主 agent 额外暴露 dispatch_agent（派生独立上下文的子 agent）；子 agent
		// （noDispatch=true）不暴露，从工具可见性层面杜绝递归套娃。
		if !a.noDispatch {
			toolSpecs = append(toolSpecs, dispatchToolSpec())
		}
		// ask_user：模型拿不准、需用户拍板时调用。Asker 为 nil（如子 agent）时不暴露，
		// 既控制了「仅主 agent 可问」，也免去额外标志位。
		if a.deps.Asker != nil {
			toolSpecs = append(toolSpecs, askUserToolSpec())
		}
	}

	// toolCallCount 累计本轮 Run 中已执行的工具调用次数，用于 MaxToolCalls 硬上限判定。
	toolCallCount := 0
	// summarized 标记本轮 Run 是否已自动摘要过一次。每次 Run 至多摘要 1 次：
	// 摘要把早期对话压成文本注入 system，已不可逆，避免在循环里反复触发。
	summarized := false
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

		// 上下文窗口压缩：在发起新一轮 LLM 调用前，按当前模型的 context window
		// 估算历史 token，超出预算则倒序截断较早的 tool 输出。Pruned=true 时才真正
		// 替换 history 并发一条 status 气泡告知用户。
		cw := a.deps.ContextWindow
		if cw <= 0 {
			cw = defaultContextWindow
		}
		prunedHistory, stats := PruneHistory(history, cw)
		if stats.Pruned {
			history = prunedHistory
			log.Printf("[Agent] context_pruned iter=%d before=%d after=%d saved=%d trimmed=%d", iter, stats.BeforeTokens, stats.AfterTokens, stats.SavedTokens, stats.ToolMsgsTrimmed)
			in.Emit.Emit("status", map[string]any{"kind": "context_pruned", "message": fmt.Sprintf("已自动压缩较早的工具输出以适应上下文窗口，省下约 %d tokens。", stats.SavedTokens), "stats": stats})
		}

		// 自动历史摘要：在裁剪 tool 输出之后，若历史仍超预算，则把更早的整段对话压成
		// 一段文本摘要并注入 system，tail（含最近 tool 配对）原文保留。每次 Run 至多摘要
		// 1 次（summarized 标记）。需要 a.deps.LLM 才能调用 Chat 生成摘要。
		if !summarized {
			sumUsable := cw - pruneOutputReserve - pruneBuffer
			if sumUsable <= 0 {
				sumUsable = defaultContextWindow - pruneOutputReserve - pruneBuffer
			}
			if a.deps.LLM != nil && EstimateHistoryTokens(history) > sumUsable {
				sumCtx, sumCancel := context.WithTimeout(ctx, 60*time.Second)
				res, err := SummarizeHistory(sumCtx, a.deps.LLM, history, sumUsable)
				sumCancel()
				if err != nil {
					log.Printf("[Agent] auto_summarize_failed iter=%d err=%v", iter, err)
				} else if res.RemovedCount > 0 {
					// 把摘要注入 system（prependSystemContext 会合并进已有 system，避免双 system），
					// tail 原样作为新 history。
					history = prependSystemContext(res.Tail, res.Summary)
					summarized = true
					log.Printf("[Agent] context_summarized iter=%d removed=%d head_tokens=%d tail_tokens=%d summary_len=%d",
						iter, res.RemovedCount, res.HeadTokens, res.TailTokens, len(res.Summary))
					in.Emit.Emit("status", map[string]any{
						"kind":          "context_summarized",
						"message":       fmt.Sprintf("已自动总结较早的 %d 条对话以适应上下文窗口。", res.RemovedCount),
						"removed_count": res.RemovedCount,
					})
				}
			}
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
		// firstAt 记录本轮首个生成 token（正文或推理）到达时刻，用于结束时计算
		// 精确生成速率 completion_tokens / (firstAt→done)，排除首 token 前的网络时延。
		var firstAt time.Time
		for chunk := range stream {
			chunks++
			if (chunk.Text != "" || chunk.Reasoning != "") && firstAt.IsZero() {
				firstAt = time.Now()
			}
			if chunk.Text != "" {
				assistantText.WriteString(chunk.Text)
				in.Emit.Emit("delta", map[string]any{"text": chunk.Text})
			}
			// 推理模型的思考过程：转发给前端展示，由 handler 层 collector 持久化；
			// 不写入 history（reasoning 不回传模型），也不计入正文 assistantText。
			if chunk.Reasoning != "" {
				in.Emit.Emit("thinking", map[string]any{"text": chunk.Reasoning})
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
			// 精确平均生成速率：服务器返回的 completion_tokens / 真实生成时长（首 token→结束）。
			// 仅当 provider 返回 usage 且可计时时才有值；否则缺省，前端回退到实时估算。
			var tokensPerSec float64
			if usage != nil && usage.OutputTokens > 0 && !firstAt.IsZero() {
				if dur := time.Since(firstAt).Seconds(); dur > 0 {
					tokensPerSec = float64(usage.OutputTokens) / dur
				}
			}
			done := map[string]any{"usage": usage}
			if tokensPerSec > 0 {
				done["tokens_per_sec"] = tokensPerSec
			}
			in.Emit.Emit("done", done)
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
			// 特判 ask_user：agent 原生能力，需 a.deps.Asker（普通工具在 Engine 构造时拿不到）。
			// 用 tc.ID 作 Question.ID，前端据此把用户的回答投递回本次阻塞调用。
			if tc.Name == askUserToolName {
				if a.deps.Asker == nil {
					result = tools.Result{Content: "ask_user 未启用", IsError: true}
				} else {
					result = a.handleAskUser(ctx, tc.ID, tc.Args)
				}
			} else if tc.Name == dispatchToolName {
				// 特判 dispatch_agent：agent 原生能力，不走 tools.Engine（它拿不到 LLM）。
				// 透传 in.PlanMode 给子 agent：plan mode 下子 agent 也只读，防止借派生绕过。
				if a.noDispatch {
					result = tools.Result{Content: "子 agent 不可派生子 agent（防递归）", IsError: true}
				} else {
					result = a.handleDispatch(ctx, tc.Args, in.PlanMode)
				}
			} else if a.deps.Tools != nil {
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

// isImageOCRTool 粗略判定一个工具是否属于"图片识别 / OCR / 截图解析"类。视觉模型本身
// 能看图，这类 MCP 工具对它多余（且因没有真实文件路径必然失败），故在"视觉模型 + 本轮
// 含图片"时过滤掉。关键词保守，避免误伤联网搜图、生成图等工具。
func isImageOCRTool(name string) bool {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "screenshot"),
		strings.Contains(n, "ocr"),
		strings.Contains(n, "extract_text"),
		strings.Contains(n, "image_to_text"),
		strings.Contains(n, "describe_image"):
		return true
	}
	return false
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
