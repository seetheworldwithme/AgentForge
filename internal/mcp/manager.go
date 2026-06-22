package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
)

const serversSettingKey = "mcp.servers"

type Manager struct {
	db *store.DB
}

func NewManager(db *store.DB) *Manager {
	return &Manager{db: db}
}

func AttachToEngine(base *tools.Engine, manager *Manager) *tools.Engine {
	if manager == nil {
		return base
	}
	return tools.NewEngineFromFunc(func() []tools.Spec {
		specs := append([]tools.Spec{}, base.List()...)
		specs = append(specs, manager.ToolSpecs()...)
		return specs
	}, func(ctx context.Context, name, args string) (tools.Result, error) {
		if IsToolName(name) {
			return manager.Execute(ctx, name, args)
		}
		return base.Execute(ctx, name, args)
	})
}

func (m *Manager) ListServers() ([]ServerConfig, error) {
	if m == nil || m.db == nil {
		return nil, nil
	}
	raw, err := m.db.GetSetting(serversSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return []ServerConfig{}, nil
	}
	var out []ServerConfig
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Manager) SaveServers(servers []ServerConfig) error {
	b, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	return m.db.SetSetting(serversSettingKey, string(b))
}

func (m *Manager) ToolSpecs() []tools.Spec {
	servers, err := m.ListServers()
	if err != nil {
		return nil
	}
	var out []tools.Spec
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		remoteTools, err := NewStdioClient(server).ListTools()
		if err != nil {
			continue
		}
		for _, remote := range remoteTools {
			schema, _ := json.Marshal(remote.InputSchema)
			out = append(out, tools.Spec{
				Name:        toolName(server, remote.Name),
				Description: "MCP " + server.Name + ": " + remote.Description,
				Parameters:  string(schema),
			})
		}
	}
	return out
}

func (m *Manager) Execute(ctx context.Context, name, args string) (tools.Result, error) {
	servers, err := m.ListServers()
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	var parsed map[string]any
	if strings.TrimSpace(args) != "" {
		if err := json.Unmarshal([]byte(args), &parsed); err != nil {
			return tools.Result{Content: "bad MCP args: " + err.Error(), IsError: true}, nil
		}
	}
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		remoteTools, err := NewStdioClient(server).ListTools()
		if err != nil {
			continue
		}
		for _, remote := range remoteTools {
			if toolName(server, remote.Name) != name {
				continue
			}
			text, err := NewStdioClient(server).CallTool(remote.Name, parsed)
			if err != nil {
				return tools.Result{Content: err.Error(), IsError: true}, nil
			}
			return tools.Result{Content: text}, nil
		}
	}
	return tools.Result{Content: "unknown MCP tool: " + name, IsError: true}, nil
}

func IsToolName(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}

func toolName(server ServerConfig, remote string) string {
	return "mcp__" + sanitize(server.ID) + "__" + sanitize(remote)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}
