package builtin

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agent-rust/core/internal/tools"
)

type Grep struct{}

func (Grep) Spec() tools.Spec {
	return tools.Spec{
		Name:        "grep",
		Description: "Recursively search for a substring in files under a path.",
		Parameters: `{"type":"object","properties":{
			"path":{"type":"string"},
			"pattern":{"type":"string"}
		},"required":["path","pattern"]}`,
	}
}

func (Grep) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	var sb strings.Builder
	count := 0
	err := filepath.Walk(p.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			if strings.Contains(sc.Text(), p.Pattern) {
				fmt.Fprintf(&sb, "%s:%d: %s\n", path, lineNo, sc.Text())
				count++
			}
		}
		return nil
	})
	if err != nil {
		return tools.Result{Content: "walk: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: fmt.Sprintf("%d matches:\n%s", count, sb.String())}, nil
}
