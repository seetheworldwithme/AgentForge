package server

import (
	"encoding/json"
	"net/http"

	"github.com/agent-rust/core/internal/rules"
	"github.com/agent-rust/core/internal/store"
	"github.com/go-chi/chi/v5"
)

// RulesHandler 暴露规则文件（AGENTFORGE.md）的 CRUD 与兼容导入开关给前端 UI。
// 规则注入走 agent.RulesProvider（运行时由 RulesStore.RulesContext 拼装），不经此处。
type RulesHandler struct {
	Store *rules.RulesStore
	DB    *store.DB // 读写兼容导入开关
}

func (h *RulesHandler) Routes(r chi.Router) {
	r.Get("/rules/content", h.getContent)
	r.Put("/rules/content", h.putContent)
	r.Delete("/rules/content", h.clearContent)
	r.Get("/rules/imports", h.getImports)
	r.Put("/rules/imports", h.putImports)
}

// validScope 校验 scope 取值合法。
func validScope(s string) (rules.Scope, bool) {
	switch s {
	case "global":
		return rules.ScopeGlobal, true
	case "project":
		return rules.ScopeProject, true
	}
	return "", false
}

// getContent 读取某 scope 的规则内容；不存在返回空串 + exists=false（前端显示空编辑器）。
func (h *RulesHandler) getContent(w http.ResponseWriter, r *http.Request) {
	scope, ok := validScope(r.URL.Query().Get("scope"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "scope 必须是 global 或 project")
		return
	}
	body, err := h.Store.Get(scope)
	if err != nil {
		if err == rules.ErrNotFound {
			writeJSON(w, http.StatusOK, map[string]any{"body": "", "exists": false})
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"body": body, "exists": true})
}

type rulesContentBody struct {
	Scope string `json:"scope"`
	Body  string `json:"body"`
}

func (h *RulesHandler) putContent(w http.ResponseWriter, r *http.Request) {
	var body rulesContentBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body: "+err.Error())
		return
	}
	scope, ok := validScope(body.Scope)
	if !ok {
		writeErr(w, http.StatusBadRequest, "scope 必须是 global 或 project")
		return
	}
	if err := h.Store.Save(scope, body.Body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope": body.Scope, "ok": true})
}

// clearContent 删除某 scope 的规则文件；不存在视为已清空（幂等返回 ok）。
func (h *RulesHandler) clearContent(w http.ResponseWriter, r *http.Request) {
	scope, ok := validScope(r.URL.Query().Get("scope"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "scope 必须是 global 或 project")
		return
	}
	if err := h.Store.Clear(scope); err != nil && err != rules.ErrNotFound {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope": scope, "ok": true})
}

// getImports 读取两个兼容导入开关（CLAUDE.md / AGENTS.md）。
func (h *RulesHandler) getImports(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"claude": h.importOn(rules.SettingImportClaude),
		"agents": h.importOn(rules.SettingImportAgents),
	})
}

type rulesImportsBody struct {
	Claude bool `json:"claude"`
	Agents bool `json:"agents"`
}

func (h *RulesHandler) putImports(w http.ResponseWriter, r *http.Request) {
	var body rulesImportsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad body: "+err.Error())
		return
	}
	h.setImport(rules.SettingImportClaude, body.Claude)
	h.setImport(rules.SettingImportAgents, body.Agents)
	writeJSON(w, http.StatusOK, map[string]any{"claude": body.Claude, "agents": body.Agents})
}

func (h *RulesHandler) importOn(key string) bool {
	if h.DB == nil {
		return false
	}
	v, _ := h.DB.GetSetting(key)
	return v == "1"
}

func (h *RulesHandler) setImport(key string, on bool) {
	if h.DB == nil {
		return
	}
	v := ""
	if on {
		v = "1"
	}
	_ = h.DB.SetSetting(key, v)
}
