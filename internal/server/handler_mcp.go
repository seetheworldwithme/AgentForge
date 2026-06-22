package server

import (
	"encoding/json"
	"net/http"

	"github.com/agent-rust/core/internal/mcp"
	"github.com/go-chi/chi/v5"
)

type MCPHandler struct {
	Manager *mcp.Manager
}

func (h *MCPHandler) Routes(r chi.Router) {
	r.Get("/mcp/servers", h.listServers)
	r.Put("/mcp/servers", h.saveServers)
}

func (h *MCPHandler) listServers(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeJSON(w, http.StatusOK, []mcp.ServerConfig{})
		return
	}
	servers, err := h.Manager.ListServers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

func (h *MCPHandler) saveServers(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "mcp manager unavailable")
		return
	}
	var servers []mcp.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&servers); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Manager.SaveServers(servers); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, servers)
}
