package builtin

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
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

	log.Printf("[Bash] request command=%q timeout=%d", previewCommand(p.Command, 240), p.Timeout)
	// dangerous: require confirmation
	matchKey, matchHint := bashRememberMatch(p.Command)
	d := gate.Request(ctx, tools.ConfirmRequest{
		ID: newID(), Tool: "bash", Args: args, MatchKey: matchKey, MatchKeyHint: matchHint,
	})
	if !d.Allow {
		log.Printf("[Bash] denied command=%q", previewCommand(p.Command, 240))
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
	workdir := ""
	if b.WorkDir != nil {
		if dir := b.WorkDir.Get(); dir != "" {
			cmd.Dir = dir
			workdir = dir
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	log.Printf("[Bash] start command=%q dir=%q timeout=%s", previewCommand(p.Command, 240), workdir, timeout)
	err := cmd.Run()
	duration := time.Since(start).Round(time.Millisecond)
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
		log.Printf("[Bash] done command=%q duration=%s stdout_bytes=%d stderr_bytes=%d err=%v ctx_err=%v",
			previewCommand(p.Command, 240), duration, stdout.Len(), stderr.Len(), err, ctx.Err())
		out += fmt.Sprintf("\n[error] %v", err)
		return tools.Result{Content: out, IsError: true}, nil
	}
	log.Printf("[Bash] done command=%q duration=%s stdout_bytes=%d stderr_bytes=%d err=<nil> ctx_err=%v",
		previewCommand(p.Command, 240), duration, stdout.Len(), stderr.Len(), ctx.Err())
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

func previewCommand(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func bashRememberMatch(command string) (string, string) {
	fields := strings.Fields(command)
	if len(fields) < 2 {
		return "", ""
	}
	family := fields[0] + " " + fields[1]
	return "bash:" + family, family
}
