package builtin

import (
	"context"
	"os"
	"strings"

	"github.com/agent-rust/core/internal/tools"
)

type FileEdit struct{}

func (FileEdit) Spec() tools.Spec {
	return tools.Spec{
		Name:        "file_edit",
		Description: "Replace the first exact occurrence of old with new in a file.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string"},
			"old":{"type":"string"},
			"new":{"type":"string"}
		},"required":["path","old","new"]}`,
	}
}

func (FileEdit) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	d := gate.Request(ctx, tools.ConfirmRequest{ID: newID(), Tool: "file_edit", Args: args})
	if !d.Allow {
		return tools.Result{Content: "user denied file_edit", IsError: true}, nil
	}
	b, err := os.ReadFile(p.Path)
	if err != nil {
		return tools.Result{Content: "read: " + err.Error(), IsError: true}, nil
	}
	s := string(b)
	idx := strings.Index(s, p.Old)
	if idx < 0 {
		return tools.Result{Content: "old string not found", IsError: true}, nil
	}
	s = s[:idx] + p.New + s[idx+len(p.Old):]
	if err := os.WriteFile(p.Path, []byte(s), 0o644); err != nil {
		return tools.Result{Content: "write: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "edited " + p.Path}, nil
}
