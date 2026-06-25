package agent

import (
	"unicode/utf8"

	"github.com/agent-rust/core/internal/llm"
)

// 裁剪相关常量。均为经验值，用于在超 context window 时安全地压缩历史。
const (
	// pruneOutputReserve 为模型本轮输出预留的 token 预算。
	pruneOutputReserve = 8000
	// pruneBuffer 用于吸收 token 估算误差，避免裁剪后仍超限。
	pruneBuffer = 2000
	// pruneProtectRecent 使最近这么多 token 的 tool 输出受软保护，不会被截断。
	pruneProtectRecent = 40000
	// pruneMinimum 为最小有效收益：只有能省下 >= 此值才真正执行裁剪，否则回滚。
	pruneMinimum = 8000
	// pruneTailSkip 硬保护最近 N 条 tool 消息，绝不截断。
	pruneTailSkip = 6
	// pruneHeadKeep 截断 tool 输出时保留前 head 的 rune 数。
	pruneHeadKeep = 1500
)

// PruneStats 描述一次历史裁剪的统计信息。
type PruneStats struct {
	Pruned          bool `json:"pruned"`
	BeforeTokens    int  `json:"before_tokens"`
	AfterTokens     int  `json:"after_tokens"`
	SavedTokens     int  `json:"saved_tokens"`
	ToolMsgsTrimmed int  `json:"tool_msgs_trimmed"`
	ContextWindow   int  `json:"context_window"`
}

// EstimateHistoryTokens 估算一整段历史消息占用的 token 数，逐条求和。
func EstimateHistoryTokens(msgs []llm.Message) int {
	total := 0
	for i := range msgs {
		total += EstimateMessageTokens(msgs[i])
	}
	return total
}

// PruneHistory 在历史 token 超出可用预算时，倒序贪心地截断较早的 tool 消息正文。
//
// 绝对不变量：
//   - 返回 slice 长度恒等于 len(history)，绝不 append 或 delete 任何消息；
//   - 仅修改 Role == RoleTool 消息的 Content 字段，system/user/assistant 消息
//     及其它字段（含 ToolCallID、ToolCalls[].ID）一律不动，配对关系完整保留。
func PruneHistory(history []llm.Message, contextWindow int) ([]llm.Message, PruneStats) {
	var stats PruneStats

	// a. 复制一份，避免改到入参底层数组。
	out := make([]llm.Message, len(history))
	copy(out, history)
	stats.ContextWindow = contextWindow

	// b. contextWindow 非正：不裁剪。
	if contextWindow <= 0 {
		return out, stats
	}

	// c. 计算可用预算与当前总量，未超则直接返回。
	usable := contextWindow - pruneOutputReserve - pruneBuffer
	total := EstimateHistoryTokens(out)
	stats.BeforeTokens = total
	if total <= usable {
		return out, stats
	}

	// d. 收集所有 tool 消息下标（升序）。
	toolIdxs := make([]int, 0, len(out))
	for i := range out {
		if out[i].Role == llm.RoleTool {
			toolIdxs = append(toolIdxs, i)
		}
	}
	if len(toolIdxs) == 0 {
		// 没有 tool 消息可裁，直接返回。
		return out, stats
	}

	// e. 保护尾部：先硬保护末尾 pruneTailSkip 条；再对剩余从近到远
	// 累计 token，累计 < pruneProtectRecent 的也软保护。剩下更早的为候选。
	protectCount := min(pruneTailSkip, len(toolIdxs))
	// 末尾 protectCount 条硬保护，不参与截断；仅前面的作为软保护候选池。
	candidatesTail := toolIdxs[:len(toolIdxs)-protectCount]

	// 从最近往远累计软保护，确定首个可截断候选位置。
	recentSum := 0
	firstCutIdx := len(candidatesTail)
	for i := len(candidatesTail) - 1; i >= 0; i-- {
		if recentSum >= pruneProtectRecent {
			break
		}
		recentSum += EstimateMessageTokens(out[candidatesTail[i]])
		// 仍处于保护范围内，则不把它列为候选。
		firstCutIdx = i
	}
	candidates := candidatesTail[:firstCutIdx]

	// f. 从最远（最老）向近处遍历候选，逐个截断直到预算达标。
	saved := 0
	trimmed := 0
	for _, idx := range candidates {
		orig := EstimateMessageTokens(out[idx])
		out[idx].Content = trimToolContent(out[idx].Content)
		newT := EstimateMessageTokens(out[idx])
		saved += orig - newT
		trimmed++
		if total-saved <= usable {
			break
		}
	}

	// g. 收益不足则回滚：返回 history 的干净副本，Pruned=false。
	if saved < pruneMinimum {
		rollback := make([]llm.Message, len(history))
		copy(rollback, history)
		return rollback, stats
	}

	stats.Pruned = true
	stats.SavedTokens = saved
	stats.ToolMsgsTrimmed = trimmed
	stats.AfterTokens = EstimateHistoryTokens(out)
	return out, stats
}

// trimToolContent 把 tool 输出截断到前 pruneHeadKeep 个 rune，并追加省略标记。
// 若原始长度不超过 pruneHeadKeep，则原样返回（不值得截断）。
func trimToolContent(content string) string {
	if utf8.RuneCountInString(content) <= pruneHeadKeep {
		return content
	}
	runes := []rune(content)
	head := string(runes[:pruneHeadKeep])
	return head + "\n...[已省略部分工具输出]..."
}
