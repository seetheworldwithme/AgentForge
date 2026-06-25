package agent

import (
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/llm"
)

// bigToolContent 生成一段明显超过 pruneHeadKeep 且明显超过 pruneMinimum
// 收益阈值的 tool 输出文本，确保裁剪算法能够切实触发并省下足够 token。
func bigToolContent(n int) string {
	// 每行约 50 个 ASCII 字符 ≈ 12 tokens，重复 n 行即可构造超长输出。
	line := "0123456789-abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJ\n"
	return strings.Repeat(line, n)
}

// buildToolHistory 构造 [system, user, assistant(带 ToolCalls), tool, ...] 交替的历史，
// 第 idx 条 tool 消息的 ToolCallID 与其前面 assistant 的 ToolCalls[0].ID 配对。
func buildToolHistory(pairs int, toolLines int) []llm.Message {
	msgs := make([]llm.Message, 0, pairs*3+2)
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: "你是一个助手"})
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: "请运行若干工具"})
	for i := range pairs {
		id := "call_" + itoa(i)
		msgs = append(msgs, llm.Message{
			Role: llm.RoleAssistant,
			Content: "调用工具中",
			ToolCalls: []llm.ToolCall{
				{ID: id, Name: "run", Args: `{"i":` + itoa(i) + `}`},
			},
		})
		msgs = append(msgs, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: id,
			Content:    bigToolContent(toolLines),
		})
	}
	return msgs
}

// itoa 是极简整数转字符串，避免在测试里引入 strconv 的细微风格差异。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func TestPruneHistory_NoOpWithinBudget(t *testing.T) {
	hist := []llm.Message{
		{Role: llm.RoleUser, Content: "短消息"},
		{Role: llm.RoleAssistant, Content: "短回复"},
	}
	// 给一个非常大的 context window，远超实际 token 用量。
	out, stats := PruneHistory(hist, 1_000_000)
	if stats.Pruned {
		t.Fatalf("预算充足不应裁剪，got Pruned=true, stats=%+v", stats)
	}
	if len(out) != len(hist) {
		t.Fatalf("长度应不变，got %d want %d", len(out), len(hist))
	}
	for i := range hist {
		if out[i].Content != hist[i].Content {
			t.Fatalf("预算充足时内容不应改变，index=%d", i)
		}
	}
}

func TestPruneHistory_NoOpWhenContextWindowZero(t *testing.T) {
	hist := buildToolHistory(3, 5000)
	out, stats := PruneHistory(hist, 0)
	if stats.Pruned {
		t.Fatalf("contextWindow=0 不应裁剪，got Pruned=true")
	}
	if len(out) != len(hist) {
		t.Fatalf("长度应不变，got %d want %d", len(out), len(hist))
	}
	for i := range hist {
		if out[i].Content != hist[i].Content {
			t.Fatalf("contextWindow=0 时内容不应改变，index=%d", i)
		}
	}
}

func TestPruneHistory_PrunesOnlyToolContent(t *testing.T) {
	// 10 个 tool 消息，每条都很大，整体远超预算，确保多数较早的会被裁。
	hist := buildToolHistory(10, 5000)
	// context window 设小，使可用预算紧张。
	out, stats := PruneHistory(hist, 60000)
	if !stats.Pruned {
		t.Fatalf("应触发裁剪，got Pruned=false, stats=%+v", stats)
	}
	if len(out) != len(hist) {
		t.Fatalf("不变量：长度必须恒等，got %d want %d", len(out), len(hist))
	}

	// 非 tool 消息 Content 一律不变。
	for i := range hist {
		if out[i].Role != llm.RoleTool {
			if out[i].Content != hist[i].Content {
				t.Fatalf("非 tool 消息 Content 不应改变，index=%d role=%s", i, out[i].Role)
			}
		}
	}

	// 每条 tool 消息的 ToolCallID 不变。
	for i := range hist {
		if out[i].Role == llm.RoleTool {
			if out[i].ToolCallID != hist[i].ToolCallID {
				t.Fatalf("ToolCallID 不应改变，index=%d got %q want %q",
					i, out[i].ToolCallID, hist[i].ToolCallID)
			}
		}
	}

	// 配对完整性：每个 assistant.ToolCalls[].ID 都能在 out 里找到匹配的 tool。
	for i := range out {
		if out[i].Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range out[i].ToolCalls {
			found := false
			for j := range out {
				if out[j].Role == llm.RoleTool && out[j].ToolCallID == tc.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("ToolCall ID=%q 找不到配对的 tool 消息", tc.ID)
			}
		}
	}

	// 被裁剪的较早 tool 消息 Content 应变短并带省略标记。
	trimmedCount := 0
	for i := range hist {
		if out[i].Role == llm.RoleTool && len(out[i].Content) < len(hist[i].Content) {
			trimmedCount++
			if !strings.Contains(out[i].Content, "...[已省略部分工具输出]...") {
				t.Fatalf("被截断的 tool 输出应包含省略标记，index=%d", i)
			}
		}
	}
	if trimmedCount == 0 {
		t.Fatalf("应至少裁剪一条 tool 消息")
	}
	if stats.ToolMsgsTrimmed != trimmedCount {
		t.Fatalf("ToolMsgsTrimmed 统计不一致，got %d want %d", stats.ToolMsgsTrimmed, trimmedCount)
	}
	if stats.AfterTokens >= stats.BeforeTokens {
		t.Fatalf("裁剪后 token 应减少，before=%d after=%d", stats.BeforeTokens, stats.AfterTokens)
	}
}

func TestPruneHistory_TailProtection(t *testing.T) {
	// 恰好 pruneTailSkip 条 tool 消息，全部很大，且总数不多以保证候选集为空。
	hist := buildToolHistory(pruneTailSkip, 5000)
	out, stats := PruneHistory(hist, 80000)
	// 候选被尾部硬保护 + 软保护全部覆盖，无法裁剪 → 应回滚。
	if stats.Pruned {
		t.Fatalf("尾部保护下不应裁剪，got Pruned=true, stats=%+v", stats)
	}
	for i := range hist {
		if out[i].Content != hist[i].Content {
			t.Fatalf("尾部保护下内容不应改变，index=%d", i)
		}
	}
}

func TestPruneHistory_MinimumRollback(t *testing.T) {
	// 构造一条较小的 tool 消息：能裁但收益不足 pruneMinimum。
	hist := []llm.Message{
		{Role: llm.RoleUser, Content: "q"},
		{
			Role:       llm.RoleTool,
			ToolCallID: "call_x",
			Content:    bigToolContent(200), // 约 2400 tokens，裁后 < pruneMinimum 收益
		},
	}
	// 设一个让它稍微超预算但远不至于榨干的 context window。
	out, stats := PruneHistory(hist, 4000)
	if stats.Pruned {
		t.Fatalf("收益不足应回滚，got Pruned=true, stats=%+v", stats)
	}
	for i := range hist {
		if out[i].Content != hist[i].Content {
			t.Fatalf("回滚后内容应与入参一致，index=%d", i)
		}
	}
}

func TestPruneHistory_Idempotent(t *testing.T) {
	hist := buildToolHistory(10, 5000)
	out1, stats1 := PruneHistory(hist, 60000)
	if !stats1.Pruned {
		t.Fatalf("首轮应裁剪，got Pruned=false")
	}
	// 对已裁剪结果再跑一次：已是稳定态，不应再裁。
	out2, stats2 := PruneHistory(out1, 60000)
	if stats2.Pruned {
		t.Fatalf("幂等：已裁剪结果再跑不应再次裁剪，got stats2=%+v", stats2)
	}
	if len(out2) != len(out1) {
		t.Fatalf("幂等：长度应不变，got %d want %d", len(out2), len(out1))
	}
	for i := range out1 {
		if out2[i].Content != out1[i].Content {
			t.Fatalf("幂等：内容应稳定，index=%d", i)
		}
	}
}
