package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"github.com/agent-rust/core/internal/tools"
)

type Bash struct {
	WorkDir *tools.WorkDir // optional; when set, commands run in this directory
}

func (Bash) Spec() tools.Spec {
	return tools.Spec{
		Name:        "bash",
		Description: "Run a shell command. Each call requires user confirmation.",
		Parameters: `{"type":"object","properties":{
			"command":{"type":"string"},
			"timeout":{"type":"integer","description":"seconds, default 30"}
		},"required":["command"]}`,
	}
}

func (b Bash) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}

	// dangerous: require confirmation
	d := gate.Request(ctx, tools.ConfirmRequest{ID: newID(), Tool: "bash", Args: args})
	if !d.Allow {
		return tools.Result{Content: "user denied bash command", IsError: true}, nil
	}

	timeout := 30 * time.Second
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", p.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", p.Command)
	}
	// Run in the user-selected working directory if one is set.
	if b.WorkDir != nil {
		if dir := b.WorkDir.Get(); dir != "" {
			cmd.Dir = dir
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	// On Windows, cmd.exe outputs in the system OEM code page (e.g. GBK/CP936
	// on Chinese Windows). Convert to UTF-8 when the raw bytes aren't valid UTF-8.
	if runtime.GOOS == "windows" {
		out = decodeWinOutput(out)
	}
	if stderr.Len() > 0 {
		errStr := stderr.String()
		if runtime.GOOS == "windows" {
			errStr = decodeWinOutput(errStr)
		}
		out += "\n[stderr]\n" + errStr
	}
	if err != nil {
		out += fmt.Sprintf("\n[error] %v", err)
		return tools.Result{Content: out, IsError: true}, nil
	}
	return tools.Result{Content: out}, nil
}

// decodeWinOutput attempts to convert non-UTF-8 command output (typically GBK
// on Simplified Chinese Windows) to valid UTF-8.
func decodeWinOutput(s string) string {
	if s == "" || utf8.ValidString(s) {
		return s
	}
	dec := simplifiedchinese.GBK.NewDecoder()
	result, _, err := transform.Bytes(dec, []byte(s))
	if err != nil {
		return s
	}
	return string(result)
}
