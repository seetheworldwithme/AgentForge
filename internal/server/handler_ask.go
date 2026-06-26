package server

import (
	"encoding/json"
	"net/http"

	"github.com/agent-rust/core/internal/tools"
	"github.com/go-chi/chi/v5"
)

// AskHandler 接收前端对 ask_user_req 的回答，投递回对应阻塞中的 Asker.Ask 调用。
// 与 ToolsHandler.confirm 对称：confirm 解除 Gate 的工具确认阻塞，这里解除 Asker 的提问阻塞。
type AskHandler struct {
	Asker *tools.Asker
}

func (h *AskHandler) Routes(r chi.Router) {
	r.Post("/agent/ask", h.ask)
}

type askResolveRequest struct {
	RequestID string `json:"request_id"`
	Selection string `json:"selection"` // 选中的 option label
	Other     string `json:"other"`      // 「其他」输入框文本
	Canceled  bool   `json:"canceled"`   // 用户取消/关闭了本次提问
}

func (h *AskHandler) ask(w http.ResponseWriter, r *http.Request) {
	var req askResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ans := tools.Answer{Selection: req.Selection, Other: req.Other, Canceled: req.Canceled}
	if !h.Asker.Resolve(req.RequestID, ans) {
		writeErr(w, http.StatusNotFound, "no pending question "+req.RequestID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": true})
}
