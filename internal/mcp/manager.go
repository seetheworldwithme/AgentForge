package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
)

const serversSettingKey = "mcp.servers"

type Manager struct {
	db         *store.DB
	configPath string
}

func NewManager(db *store.DB) *Manager {
	return NewManagerWithPath(db, defaultConfigPath())
}

func NewManagerWithPath(db *store.DB, configPath string) *Manager {
	return &Manager{db: db, configPath: configPath}
}

func (m *Manager) ConfigPath() string {
	if m == nil {
		return ""
	}
	return m.configPath
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
	if m.configPath != "" {
		raw, err := os.ReadFile(m.configPath)
		if err == nil && strings.TrimSpace(string(raw)) != "" {
			return ParseConfigJSON(raw)
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	raw, err := m.db.GetSetting(serversSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return []ServerConfig{}, nil
	}
	var out []ServerConfig
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return normalizeServers(out), nil
}

func (m *Manager) SaveServers(servers []ServerConfig) error {
	servers = normalizeServers(servers)
	if err := m.ValidateServers(servers); err != nil {
		return err
	}
	return m.writeServers(servers)
}

func (m *Manager) SaveConfigJSON(raw []byte) ([]byte, error) {
	servers, err := ParseConfigJSON(raw)
	if err != nil {
		return nil, err
	}
	if err := m.ValidateServers(servers); err != nil {
		return nil, err
	}
	if err := m.writeServers(servers); err != nil {
		return nil, err
	}
	return FormatConfigJSON(servers)
}

func (m *Manager) ConfigJSON() ([]byte, error) {
	servers, err := m.ListServers()
	if err != nil {
		return nil, err
	}
	return FormatConfigJSON(servers)
}

func (m *Manager) ValidateServers(servers []ServerConfig) error {
	for _, server := range normalizeServers(servers) {
		if !server.Enabled {
			continue
		}
		if _, err := NewClient(server).ListTools(); err != nil {
			return fmt.Errorf("MCP %s unavailable: %w", server.Name, err)
		}
	}
	return nil
}

func (m *Manager) writeServers(servers []ServerConfig) error {
	b, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	if m.configPath != "" {
		if err := os.MkdirAll(filepath.Dir(m.configPath), 0o755); err != nil {
			return err
		}
		raw, err := FormatConfigJSON(servers)
		if err != nil {
			return err
		}
		if err := os.WriteFile(m.configPath, raw, 0o644); err != nil {
			return err
		}
	}
	if m.db != nil {
		return m.db.SetSetting(serversSettingKey, string(b))
	}
	return nil
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
		client := NewClient(server)
		remoteTools, err := client.ListTools()
		if err != nil {
			// 暴露加载失败：否则某个 MCP 没起来时，模型和用户都无从知晓
			// 它的工具为何不可用（识图 MCP 没拉起就会导致"不调用"的假象）。
			log.Printf("[MCP] server %q tools/list failed: %v", server.Name, err)
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

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".agent", "mcp.json")
	}
	return filepath.Join(home, ".agent", "mcp.json")
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
		client := NewClient(server)
		remoteTools, err := client.ListTools()
		if err != nil {
			continue
		}
		for _, remote := range remoteTools {
			if toolName(server, remote.Name) != name {
				continue
			}
			text, err := client.CallTool(remote.Name, parsed)
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
