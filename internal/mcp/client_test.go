package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/store"
)

func TestStdioClientListsAndCallsTools(t *testing.T) {
	if os.Getenv("AGENTFORGE_MCP_HELPER") == "1" {
		runMCPHelper()
		return
	}

	client := NewStdioClient(ServerConfig{
		ID:      "srv_test",
		Name:    "test",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestStdioClientListsAndCallsTools"},
		Env:     map[string]string{"AGENTFORGE_MCP_HELPER": "1"},
		Enabled: true,
	})

	tools, err := client.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}

	result, err := client.CallTool("echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "hello") {
		t.Fatalf("unexpected call result: %q", result)
	}
}

func TestHTTPClientListsAndCallsTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/json") || !strings.Contains(got, "text/event-stream") {
			t.Errorf("missing MCP accept header: %q", got)
		}
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		switch req.Method {
		case "initialize":
			writeHTTPRPC(w, req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
			})
		case "tools/list":
			writeHTTPRPC(w, req.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo text",
					"inputSchema": map[string]any{"type": "object"},
				}},
			})
		case "tools/call":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}\n\n", req.ID)
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}
	}))
	defer srv.Close()

	client := NewClient(ServerConfig{
		ID: "srv_http", Name: "http", Transport: TransportSSE, URL: srv.URL, Enabled: true,
	})
	tools, err := client.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	result, err := client.CallTool("echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "hello") {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestParseMCPConfigAcceptsMCPServersObject(t *testing.T) {
	raw := `{"mcpServers":{"files":{"command":"npx","args":["-y","server"],"env":{"A":"B"}},"remote":{"url":"http://localhost:3000/mcp","headers":{"Authorization":"Bearer token"}}}}`
	servers, err := ParseConfigJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %#v", servers)
	}
	if servers[0].ID != "files" || servers[0].Transport != TransportStdio || servers[0].Command != "npx" {
		t.Fatalf("stdio server not normalized: %#v", servers[0])
	}
	if servers[1].ID != "remote" || servers[1].Transport != TransportSSE || servers[1].URL == "" {
		t.Fatalf("remote server not normalized: %#v", servers[1])
	}
}

func TestManagerSavesConfigToAgentMCPJSON(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	path := filepath.Join(t.TempDir(), ".agent", "mcp.json")
	manager := NewManagerWithPath(db, path)

	err = manager.SaveServers([]ServerConfig{{
		ID: "disabled", Name: "Disabled", Transport: TransportStdio, Command: "missing", Enabled: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mcpServers"`) || !strings.Contains(string(raw), `"disabled"`) {
		t.Fatalf("unexpected mcp.json: %s", string(raw))
	}
	servers, err := manager.ListServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].ID != "disabled" {
		t.Fatalf("unexpected servers from file: %#v", servers)
	}
}

func TestManagerRejectsInvalidEnabledServerWithoutWritingConfig(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	path := filepath.Join(t.TempDir(), ".agent", "mcp.json")
	manager := NewManagerWithPath(db, path)

	err = manager.SaveServers([]ServerConfig{{
		ID: "bad", Name: "Bad", Transport: TransportStdio, Command: "definitely-not-a-real-command", Enabled: true,
	}})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid config should not be written, stat err=%v", statErr)
	}
}

func runMCPHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			writeRPC(req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "test", "version": "0.0.1"},
			})
		case "tools/list":
			writeRPC(req.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "Echo text",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"text": map[string]any{"type": "string"}},
					},
				}},
			})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			writeRPC(req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": fmt.Sprint(params.Arguments["text"])}},
			})
		}
	}
}

func writeRPC(id int, result any) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	fmt.Println(string(body))
}

func writeHTTPRPC(w http.ResponseWriter, id int, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}
