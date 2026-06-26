package server

import (
	"net/http"

	"github.com/agent-rust/core/internal/todo"
	"github.com/go-chi/chi/v5"
)

// TodoHandler 暴露 GET /api/sessions/{id}/todo，供前端切会话时拉取该会话的待办清单。
// 清单按会话隔离、存在内存里，所以这里直接读 Store.ListFor(id)。
type TodoHandler struct {
	Store *todo.Store
}

func (h *TodoHandler) Routes(r chi.Router) {
	r.Get("/sessions/{id}/todo", h.List)
}

func (h *TodoHandler) List(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Store.ListFor(id)})
}
