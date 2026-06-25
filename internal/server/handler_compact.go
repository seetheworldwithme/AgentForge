package server

import (
	"context"
	"net/http"
	"time"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

// compactResponse 是手动压缩接口的返回体。compacted=false 时仅 usage_before 有值，
// 其余统计字段留空（带 omitempty 不下发）。
type compactResponse struct {
	Compacted      bool   `json:"compacted"`
	Reason         string `json:"reason,omitempty"`
	RemovedCount   int    `json:"removed_count,omitempty"`
	SummaryPreview string `json:"summary_preview,omitempty"`
	UsageBefore    int    `json:"usage_before,omitempty"`
	UsageAfter     int    `json:"usage_after,omitempty"`
}

// Compact 手动触发会话历史压缩：把较早的对话交给 LLM 摘要，并用单条 summary
// 消息替换前缀，下次加载历史变短、腾出 token 预算。
//
// 触发门槛：仅当历史估算 token 已达上下文窗口的 60% 时才真正压缩，否则原样返回。
// 摘要消息的 CreatedAt 设为保留尾部首条消息的前一纳秒，以保证它在 ListMessages
// 的时间顺序里排在尾部之前（见 store.CompactHistoryPrefix 的契约）。
func (h *SessionHandler) Compact(w http.ResponseWriter, r *http.Request) {
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
	// buildLLM 未注入（如测试或受限部署）时无法调用 LLM，直接 503。
	if h.buildLLM == nil {
		writeErr(w, http.StatusServiceUnavailable, "compact unavailable: llm builder not configured")
		return
	}

	storedMsgs, _ := h.DB.ListMessages(id)
	history := make([]llm.Message, 0, len(storedMsgs))
	for _, m := range storedMsgs {
		history = append(history, storeMsgToLLM(m, prov.Vision))
	}

	total := agent.EstimateHistoryTokens(history)
	window := effectiveContextWindow(prov)
	// 使用率未达 60%：无需压缩，直接回带 usage_before 供前端展示。
	if total*100 < window*60 {
		writeJSON(w, http.StatusOK, compactResponse{
			Compacted:   false,
			Reason:      "使用率未达 60%，无需压缩",
			UsageBefore: total,
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := agent.SummarizeHistory(ctx, h.buildLLM(prov), history, window/2)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 摘要要插在保留尾部之前：取 tail 首条消息的时间，往前 1 纳秒作为摘要时刻。
	tailFirstCreatedAt := storedMsgs[res.TailStartIndex].CreatedAt
	summaryCreatedAt := tailFirstCreatedAt
	if t, perr := time.Parse(time.RFC3339Nano, tailFirstCreatedAt); perr == nil {
		summaryCreatedAt = t.Add(-time.Nanosecond).Format(time.RFC3339Nano)
	}

	summary := store.Message{
		ID:        "msg_" + ulid.Make().String(),
		SessionID: id,
		Role:      "summary",
		Content:   res.Summary,
		CreatedAt: summaryCreatedAt,
	}
	if err := h.DB.CompactHistoryPrefix(id, tailFirstCreatedAt, summary); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	preview := res.Summary
	if runes := []rune(preview); len(runes) > 200 {
		preview = string(runes[:200])
	}
	writeJSON(w, http.StatusOK, compactResponse{
		Compacted:      true,
		RemovedCount:   res.RemovedCount,
		SummaryPreview: preview,
		UsageBefore:    total,
		UsageAfter:     total - res.HeadTokens,
	})
}
