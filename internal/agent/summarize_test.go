package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/llm"
)

// toolAssistant 构造一条含单个 tool_call 的 assistant 消息。
func toolAssistant(id, text string) llm.Message {
	return llm.Message{
		Role:      llm.RoleAssistant,
		Content:   text,
		ToolCalls: []llm.ToolCall{{ID: id, Name: "bash", Args: "{}"}},
	}
}

// toolResult 构造一条对指定 call_id 的 tool 结果消息。
func toolResult(id, text string) llm.Message {
	return llm.Message{
		Role:       llm.RoleTool,
		Content:    text,
		ToolCallID: id,
	}
}

// TestSummarizeHistory_NoOpWithinBudget：history 总 token 在 keepTailTokens 内时，
// 不应调用 LLM（chatCalls=0），RemovedCount=0。
func TestSummarizeHistory_NoOpWithinBudget(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "你好"},
		{Role: llm.RoleAssistant, Content: "你好呀"},
	}
	m := &fakeLLM{chatReply: "（不应被使用的摘要）"}
	total := EstimateHistoryTokens(history)

	res, err := SummarizeHistory(context.Background(), m, history, total+10000)
	if err != nil {
		t.Fatalf("SummarizeHistory error: %v", err)
	}
	if m.chatCalls != 0 {
		t.Errorf("LLM Chat calls = %d, want 0 (history within budget must skip summarization)", m.chatCalls)
	}
	if res.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0", res.RemovedCount)
	}
}

// TestSummarizeHistory_TailPreservesToolPairing：构造超预算 history，
// 断言 tool 配对不变量：
//   - head 内带 ToolCalls 的 assistant，其 tool 结果不得落在 tail（不能切断配对）；
//   - head 内不得有孤儿 tool 消息（tool 在 head 但其 assistant 在 tail）；
//   - tail 内每个带 ToolCalls 的 assistant，其每个 ToolCallID 都能在 tail 找到配对 tool。
func TestSummarizeHistory_TailPreservesToolPairing(t *testing.T) {
	// 结构：system + 一段纯文本对话（user/assistant，作为安全的 head 结尾停留点）
	//   + 若干组 (assistant+tool_calls, tool) 配对 + 收尾 user。
	// head 必须能停在中间那段纯文本 assistant 上：前面进 head 被摘要、后面 tool 组留 tail。
	big := strings.Repeat("这是较早的一段较长对话内容。", 60) // 约 840 tokens，拉高总量
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: big},                  // 索引1：大 user（进 head）
		{Role: llm.RoleAssistant, Content: big},             // 索引2：纯文本 assistant（安全切分点）
		toolAssistant("call_0", big), toolResult("call_0", big), // 3-4：tool 组 0
		toolAssistant("call_1", big), toolResult("call_1", big), // 5-6：tool 组 1
		{Role: llm.RoleUser, Content: "继续吧"},             // 7
	}

	m := &fakeLLM{chatReply: "【目标】完成任务。【已完成】前述步骤。【待办】继续。"}
	total := EstimateHistoryTokens(history)
	// keepTailTokens 取较小值，使 tail 仅保留最后一两组配对 + user，head 停在索引2 的纯文本 assistant。
	res, err := SummarizeHistory(context.Background(), m, history, total/6)
	if err != nil {
		t.Fatalf("SummarizeHistory error: %v", err)
	}
	if m.chatCalls != 1 {
		t.Errorf("LLM Chat calls = %d, want 1 (should summarize once)", m.chatCalls)
	}
	if res.RemovedCount == 0 {
		t.Fatalf("RemovedCount = 0, want > 0 (history over budget)")
	}

	head := history[:res.TailStartIndex]
	tail := history[res.TailStartIndex:]

	// head 内带 ToolCalls 的 assistant，其 tool 结果不得落在 tail。
	for i := range head {
		if head[i].Role != llm.RoleAssistant || len(head[i].ToolCalls) == 0 {
			continue
		}
		tailToolIDs := map[string]bool{}
		for j := range tail {
			if tail[j].Role == llm.RoleTool {
				tailToolIDs[tail[j].ToolCallID] = true
			}
		}
		for _, tc := range head[i].ToolCalls {
			if tailToolIDs[tc.ID] {
				t.Errorf("head assistant[%d] ToolCallID %q has its tool result stranded in tail", i, tc.ID)
			}
		}
	}

	// head 内不得有孤儿 tool（tool 在 head 但其 assistant 在 tail）。
	tailAssistantToolIDs := map[string]bool{} // tail assistant 引用的 id（这些 tool 必须在 tail，不能在 head）
	for j := range tail {
		if tail[j].Role == llm.RoleAssistant {
			for _, tc := range tail[j].ToolCalls {
				tailAssistantToolIDs[tc.ID] = true
			}
		}
	}
	for i := range head {
		if head[i].Role != llm.RoleTool {
			continue
		}
		if tailAssistantToolIDs[head[i].ToolCallID] {
			t.Errorf("head tool[%d] ToolCallID %q is orphan: its assistant is in tail", i, head[i].ToolCallID)
		}
	}

	// tail 内每个带 ToolCalls 的 assistant，其每个 ToolCallID 都能在 tail 找到配对 tool。
	toolIDs := make(map[string]bool)
	for i := range tail {
		if tail[i].Role == llm.RoleTool {
			toolIDs[tail[i].ToolCallID] = true
		}
	}
	for i := range tail {
		if tail[i].Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range tail[i].ToolCalls {
			if !toolIDs[tc.ID] {
				t.Errorf("tail assistant ToolCallID %q has no matching tool in tail", tc.ID)
			}
		}
	}

	// tail 至少保留 system + 1 条（splitForSummary 的 headEnd<1 兜底）。
	if len(res.Tail) < 2 {
		t.Errorf("tail length = %d, want >= 2 (system + at least one turn)", len(res.Tail))
	}

	// 摘要文本应被保留。
	if res.Summary == "" {
		t.Errorf("Summary is empty")
	}
}

// TestSummarizeHistory_ChatErrorReturnsErr：LLM Chat 失败时返回 err，不产出结果。
func TestSummarizeHistory_ChatErrorReturnsErr(t *testing.T) {
	// head 内必须有纯文本 assistant 作为安全停留点，否则 splitForSummary 会一路前移到顶、
	// 兜底 NoOp，从而根本不会触发 Chat。
	big := strings.Repeat("x", 4000)
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: big},      // 大 user：进 head
		{Role: llm.RoleAssistant, Content: big}, // 纯文本 assistant：安全切分点
		{Role: llm.RoleUser, Content: "继续"},
	}
	m := &fakeLLM{chatReply: "", chatErr: context.DeadlineExceeded}
	_, err := SummarizeHistory(context.Background(), m, history, 100)
	if err == nil {
		t.Fatalf("expected error when LLM Chat fails, got nil")
	}
}

// TestSplitForSummary 覆盖 splitForSummary 的边界场景。
func TestSplitForSummary(t *testing.T) {
	t.Run("empty history", func(t *testing.T) {
		if got := splitForSummary(nil, 100); got != 0 {
			t.Errorf("empty history headEnd = %d, want 0", got)
		}
	})

	t.Run("within budget returns len(history)", func(t *testing.T) {
		h := []llm.Message{
			{Role: llm.RoleSystem, Content: "s"},
			{Role: llm.RoleUser, Content: "hi"},
		}
		total := EstimateHistoryTokens(h)
		if got := splitForSummary(h, total+1000); got != len(h) {
			t.Errorf("within-budget headEnd = %d, want %d (len)", got, len(h))
		}
	})

	t.Run("first message already over budget keeps system+1", func(t *testing.T) {
		// keepTailTokens 极小：即便从最末尾累加，第一条就超预算。
		// 切分点会回退到 < 1，触发兜底返回 len(history)（不压缩）。
		h := []llm.Message{
			{Role: llm.RoleSystem, Content: "s"},
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, Content: "hey"},
		}
		if got := splitForSummary(h, 0); got != len(h) {
			t.Errorf("headEnd < 1 fallback: got %d, want %d (len, no compression)", got, len(h))
		}
	})

	t.Run("tool-only tail does not strand orphan tool calls", func(t *testing.T) {
		// 末尾连续若干条 tool：splitForSummary 必须保证 tool 配对完整，
		// 不在 head 留下「tool 结果被切到 tail」的 assistant，也不留孤儿 tool。
		big := strings.Repeat("内容", 200)
		h := []llm.Message{
			{Role: llm.RoleSystem, Content: "s"},
			toolAssistant("c0", big), toolResult("c0", big), // 老一组：应进 head（被摘要）
			toolAssistant("c1", big), toolResult("c1", big), // 新一组：tail 候选边界
			toolAssistant("c2", big), toolResult("c2", big), // 最新一组：必然在 tail
			{Role: llm.RoleUser, Content: "继续"},
		}
		total := EstimateHistoryTokens(h)
		headEnd := splitForSummary(h, total/4)

		// 不变量：head 内带 ToolCalls 的 assistant，其 tool 结果不得在 tail。
		tailToolIDs := map[string]bool{}
		for i := headEnd; i < len(h); i++ {
			if h[i].Role == llm.RoleTool {
				tailToolIDs[h[i].ToolCallID] = true
			}
		}
		for i := range headEnd {
			if h[i].Role != llm.RoleAssistant || len(h[i].ToolCalls) == 0 {
				continue
			}
			for _, tc := range h[i].ToolCalls {
				if tailToolIDs[tc.ID] {
					t.Fatalf("head assistant[%d] ToolCallID %q tool result stranded in tail at headEnd=%d", i, tc.ID, headEnd)
				}
			}
		}
		// 不变量：tail 内每个 ToolCallID 都有配对 tool。
		toolIDs := map[string]bool{}
		for i := headEnd; i < len(h); i++ {
			if h[i].Role == llm.RoleTool {
				toolIDs[h[i].ToolCallID] = true
			}
		}
		for i := headEnd; i < len(h); i++ {
			if h[i].Role != llm.RoleAssistant {
				continue
			}
			for _, tc := range h[i].ToolCalls {
				if !toolIDs[tc.ID] {
					t.Errorf("tail assistant ToolCallID %q has no matching tool at headEnd=%d", tc.ID, headEnd)
				}
			}
		}
	})

	t.Run("assistant with tool calls at boundary moves back", func(t *testing.T) {
		// 构造：切分点 i 恰好落在一条「带 ToolCalls 的 assistant」上，
		// splitForSummary 应前移，使该 assistant 与其 tool 结果整体进入 tail。
		big := strings.Repeat("内容", 300)
		h := []llm.Message{
			{Role: llm.RoleSystem, Content: "s"},
			{Role: llm.RoleUser, Content: big}, // 大 user：确保 i 从这里开始累计
			toolAssistant("c0", "small"),       // 边界 assistant：含 ToolCalls
			toolResult("c0", "small"),          // 配对 tool
			{Role: llm.RoleUser, Content: "继续"},
		}
		// 取一个让 tail 仅能容纳最末 user + 0~1 条的 keepTailTokens，
		// 强制 i 落到前面的 assistant/tool 区。
		total := EstimateHistoryTokens(h)
		headEnd := splitForSummary(h, total/5)

		// 不变量：head 内带 ToolCalls 的 assistant，其 tool 结果不得在 tail
		// （边界 assistant 与其 tool 必须同侧）。
		tailToolIDs := map[string]bool{}
		for i := headEnd; i < len(h); i++ {
			if h[i].Role == llm.RoleTool {
				tailToolIDs[h[i].ToolCallID] = true
			}
		}
		for i := range headEnd {
			if h[i].Role != llm.RoleAssistant || len(h[i].ToolCalls) == 0 {
				continue
			}
			for _, tc := range h[i].ToolCalls {
				if tailToolIDs[tc.ID] {
					t.Fatalf("boundary assistant[%d] tool result stranded in tail at headEnd=%d", i, headEnd)
				}
			}
		}
	})
}
