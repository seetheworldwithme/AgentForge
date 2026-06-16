package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/agent-rust/core/internal/tools"
)

type FileRead struct{}

func (FileRead) Spec() tools.Spec {
	return tools.Spec{
		Name:        "file_read",
		Description: "Read the contents of a file. Returns up to 2000 lines.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string","description":"absolute path to the file"},
			"offset":{"type":"integer","description":"1-based line to start at, optional"},
			"limit":{"type":"integer","description":"max lines to read, optional"}
		},"required":["path"]}`,
	}
}

func (FileRead) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	b, err := os.ReadFile(p.Path)
	if err != nil {
		return tools.Result{Content: "read error: " + err.Error(), IsError: true}, nil
	}
	lines := strings.Split(string(b), "\n")
	if p.Offset > 0 && p.Offset <= len(lines) {
		lines = lines[p.Offset-1:]
	}
	if p.Limit > 0 && p.Limit < len(lines) {
		lines = lines[:p.Limit]
	}
	var sb strings.Builder
	for i, ln := range lines {
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, ln)
	}
	return tools.Result{Content: sb.String()}, nil
}
