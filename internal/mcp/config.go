package mcp

import (
	"encoding/json"
	"sort"
	"strings"
)

type fileConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

type rawFileConfig struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

func ParseConfigJSON(raw []byte) ([]ServerConfig, error) {
	var list []ServerConfig
	if err := json.Unmarshal(raw, &list); err == nil {
		return normalizeServers(list), nil
	}

	var rawCfg rawFileConfig
	if err := json.Unmarshal(raw, &rawCfg); err != nil {
		return nil, err
	}
	cfg := fileConfig{MCPServers: map[string]ServerConfig{}}
	for key, rawServer := range rawCfg.MCPServers {
		var server ServerConfig
		if err := json.Unmarshal(rawServer, &server); err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(rawServer, &fields)
		if _, ok := fields["enabled"]; !ok {
			server.Enabled = true
		}
		cfg.MCPServers[key] = server
	}
	keys := make([]string, 0, len(cfg.MCPServers))
	for key := range cfg.MCPServers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ServerConfig, 0, len(keys))
	for _, key := range keys {
		server := cfg.MCPServers[key]
		if server.ID == "" {
			server.ID = key
		}
		if server.Name == "" {
			server.Name = key
		}
		out = append(out, normalizeServer(server))
	}
	return out, nil
}

func FormatConfigJSON(servers []ServerConfig) ([]byte, error) {
	cfg := fileConfig{MCPServers: map[string]ServerConfig{}}
	for _, server := range normalizeServers(servers) {
		key := server.ID
		if key == "" {
			key = sanitize(server.Name)
		}
		cfg.MCPServers[key] = server
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func normalizeServers(servers []ServerConfig) []ServerConfig {
	out := make([]ServerConfig, len(servers))
	for i, server := range servers {
		out[i] = normalizeServer(server)
	}
	return out
}

func normalizeServer(server ServerConfig) ServerConfig {
	if server.Name == "" {
		server.Name = server.ID
	}
	if server.Transport == "" {
		if strings.TrimSpace(server.URL) != "" {
			server.Transport = TransportSSE
		} else {
			server.Transport = TransportStdio
		}
	}
	if server.Env == nil {
		server.Env = map[string]string{}
	}
	if server.Headers == nil {
		server.Headers = map[string]string{}
	}
	return server
}
