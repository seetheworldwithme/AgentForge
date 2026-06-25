package agent

import (
	"context"
	"strings"

	"github.com/agent-rust/core/internal/llm"
)

// SummarizeResult 描述一次历史摘要的结果。
type SummarizeResult struct {
	// Summary 是 LLM 生成的早期对话摘要文本。
	Summary string `json:"summary"`
	// Tail 是保留下来不参与摘要的尾部消息（即原 history[headEnd:]）。
	Tail []llm.Message `json:"tail"`
	// TailStartIndex 是 Tail 在原 history 中的起始下标。
	TailStartIndex int `json:"tail_start_index"`
	// RemovedCount 是被摘要替换掉的消息条数，等于 len(head)。
	RemovedCount int `json:"removed_count"`
	// HeadTokens 是被摘要部分（head）的估算 token 数。
	HeadTokens int `json:"head_tokens"`
	// TailTokens 是保留部分（tail）的估算 token 数。
	TailTokens int `json:"tail_tokens"`
}

// SummarizeHistory 把超预算的早期对话（head）压缩成一段摘要，保留尾部（tail）原文。
//
// 流程：
//   - 先用 splitForSummary 找到切分点 headEnd；
//   - 若 headEnd >= len(history)，说明整体在预算内无需压缩，直接返回 RemovedCount=0；
//   - 否则把 head 各消息拼成对话文本交给 LLM 生成摘要，tail 原样保留。
//
// 绝对不变量（由 splitForSummary 保证）：
//   - head 内不残留「有 ToolCalls 但其 tool 结果被切到 tail」的孤儿 assistant；
//   - tail 内每个 ToolCallID 都有配对的 tool 消息。
//
// Chat 失败时返回 err，调用方应据此降级（不替换 history）。
func SummarizeHistory(ctx context.Context, client llm.LLMClient, history []llm.Message, keepTailTokens int) (SummarizeResult, error) {
	headEnd := splitForSummary(history, keepTailTokens)
	// 整体在预算内：无需调用 LLM。
	if headEnd >= len(history) {
		return SummarizeResult{
			Tail:           history,
			TailStartIndex: len(history),
			TailTokens:     EstimateHistoryTokens(history),
		}, nil
	}

	head := history[:headEnd]
	tail := history[headEnd:]

	// 把 head 各消息按「角色: 内容」拼接成对话文本，作为摘要输入。
	var sb strings.Builder
	for i := range head {
		sb.WriteString(string(head[i].Role))
		sb.WriteString(": ")
		sb.WriteString(head[i].Content)
		sb.WriteString("\n")
	}

	summary, err := client.Chat(ctx, []llm.Message{
		{Role: llm.RoleSystem, Content: summaryPrompt()},
		{Role: llm.RoleUser, Content: sb.String()},
	})
	if err != nil {
		return SummarizeResult{}, err
	}

	return SummarizeResult{
		Summary:        summary,
		Tail:           tail,
		TailStartIndex: headEnd,
		RemovedCount:   len(head),
		HeadTokens:     EstimateHistoryTokens(head),
		TailTokens:     EstimateHistoryTokens(tail),
	}, nil
}

// splitForSummary 在 history 中找到切分点 headEnd：head=history[:headEnd] 将被压缩为摘要，
// tail=history[headEnd:] 保留原文。纯函数，不依赖外部状态。
//
// 算法（必须保证 tool 配对完整）：
//  1. 从末尾往前累加 EstimateMessageTokens，找到使累计 token 首次超过 keepTailTokens 的下标 i
//     （即 history[i:] 是 tail 候选）。
//  2. 反复前移 i，直到它落在安全的 head 结尾：i 处为 tool（孤儿 tool）或带 ToolCalls 的
//     assistant（其 tool 结果会落到 tail）都必须前移；前移后可能落到上一条 tool，故用 while。
//  3. headEnd = i；若 headEnd < 1（至少保留 system+1 条），则 headEnd = len(history)（不压缩）。
//
// 绝对不变量：
//   - head 内不得存在「有 ToolCalls 但其 tool 结果被切到 tail」的 assistant；
//   - tail 内每个 ToolCallID 都有配对 tool 消息。
func splitForSummary(history []llm.Message, keepTailTokens int) int {
	n := len(history)
	if n == 0 {
		return 0
	}

	// 1. 从末尾往前累加 token，找到使累计首次超过 keepTailTokens 的下标 i。
	cumulative := 0
	i := n
	for j := n - 1; j >= 0; j-- {
		cumulative += EstimateMessageTokens(history[j])
		if cumulative > keepTailTokens {
			i = j // history[i:] 是 tail 候选
			break
		}
	}
	// 整体都在预算内：i 仍为 n，表示无需压缩。
	if i == n {
		return n
	}

	// 2. 把切分点 i 调整到一个安全的 head 结尾，保证 tool 配对完整。
	//    反复前移直到 i 处既不是孤儿 tool，也不是「带 ToolCalls 但其 tool 结果会落到
	//    tail」的 assistant：i 处为 tool 则必须连同其 assistant 一起进 tail；i 处为
	//    带 ToolCalls 的 assistant 则要把它和它的 tool 结果整体留进 tail。前移一位后
	//    可能落到上一条 tool，需继续循环，故用 while 而非单步。
	for i > 0 {
		if history[i].Role == llm.RoleTool {
			// tool 消息必须跟随其前面的 assistant，整体留在 tail。
			i--
			continue
		}
		if history[i].Role == llm.RoleAssistant && len(history[i].ToolCalls) > 0 {
			// 带 ToolCalls 的 assistant：把它和它的 tool 结果整体留在 tail。
			i--
			continue
		}
		break
	}

	// 3. headEnd < 1（切到首条之前）则放弃压缩：保留至少 system+1 条。
	if i < 1 {
		return n
	}

	return i
}

// summaryPrompt 返回中文结构化摘要指令，要求 LLM 把给定对话历史浓缩为：
// 目标 / 关键发现 / 已完成 / 相关文件路径 / 待办，保留所有关键事实、文件路径、决策；
// 输出纯文本，不要寒暄。
func summaryPrompt() string {
	return `你是一个对话历史压缩器。请把下面给出的较早的对话历史浓缩为一份结构化摘要，供后续对话继续参考。

输出格式（中文，用以下小标题，缺项可写“无”）：

【目标】用户本次任务想达成什么。
【关键发现】调研、命令执行、代码阅读中得到的关键事实与结论。
【已完成】已经做了哪些操作 / 改动 / 产出。
【相关文件路径】涉及的文件、目录、URL 等路径，逐行列出。
【待办】尚未完成、需要继续推进的事项。

要求：
- 保留所有关键事实、文件路径、技术决策、参数取值，不要丢失上下文。
- 摘要要紧凑、可执行，避免无关细节与寒暄。
- 只输出摘要正文，不要加“好的”“以下是摘要”之类的开场白。`
}
