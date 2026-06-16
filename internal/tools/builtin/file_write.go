package builtin

import (
	"context"
	"os"
	"path/filepath"

	"github.com/agent-rust/core/internal/tools"
)

type FileWrite struct{}

func (FileWrite) Spec() tools.Spec {
	return tools.Spec{
		Name:        "file_write",
		Description: "Write content to a file, overwriting if it exists.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string"},
			"content":{"type":"string"}
		},"required":["path","content"]}`,
	}
}

func (FileWrite) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	// dangerous: require confirmation
	d := gate.Request(ctx, tools.ConfirmRequest{ID: newID(), Tool: "file_write", Args: args})
	if !d.Allow {
		return tools.Result{Content: "user denied file_write", IsError: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return tools.Result{Content: "mkdir: " + err.Error(), IsError: true}, nil
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return tools.Result{Content: "write: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "wrote " + p.Path}, nil
}
