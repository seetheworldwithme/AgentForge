package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
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
