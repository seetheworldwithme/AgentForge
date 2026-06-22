package server

import (
	"encoding/json"
	"io"
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
	r.Get("/mcp/config", h.getConfig)
	r.Put("/mcp/config", h.saveConfig)
	r.Get("/mcp/config-path", h.configPath)
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

func (h *MCPHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeJSON(w, http.StatusOK, map[string]any{"mcpServers": map[string]any{}})
		return
	}
	raw, err := h.Manager.ConfigJSON()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (h *MCPHandler) saveConfig(w http.ResponseWriter, r *http.Request) {
	if h.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "mcp manager unavailable")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.Manager.SaveConfigJSON(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func (h *MCPHandler) configPath(w http.ResponseWriter, r *http.Request) {
	path := ""
	if h.Manager != nil {
		path = h.Manager.ConfigPath()
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}
