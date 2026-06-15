package command

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"

	"github.com/agentforge/agentforge/internal/tool"
)

// Runner 执行白名单命令并把输出转为 tool.Event 流。
type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

// Run 执行一个命令规格，返回事件流。不经 shell，结构化传参。
func (r *Runner) Run(ctx context.Context, spec CommandSpec) (<-chan tool.Event, error) {
	ch := make(chan tool.Event)

	go func() {
		defer close(ch)

		cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)

		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "启动失败(stdout pipe): " + err.Error(),
				IsError: true,
			}}
			return
		}
		stderrBuf := &bytes.Buffer{}
		cmd.Stderr = stderrBuf

		if err := cmd.Start(); err != nil {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "启动失败: " + err.Error(),
				IsError: true,
			}}
			return
		}

		scanner := bufio.NewScanner(stdoutPipe)
		// 默认 64KB 行长上限对 V1 的系统信息命令足够；超大行会被截断。
		// 若未来白名单命令产出超长行，调大 scanner.Buffer 并检查 scanner.Err()。
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var collected bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			ch <- tool.Event{Kind: tool.EventDelta, Text: line}
			collected.WriteString(line + "\n")
		}
		if scanErr := scanner.Err(); scanErr != nil {
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: "读取输出失败: " + scanErr.Error(),
				IsError: true,
			}}
			return
		}

		if err := cmd.Wait(); err != nil {
			errText := err.Error()
			if stderrBuf.Len() > 0 {
				errText += "\nstderr: " + stderrBuf.String()
			}
			ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
				Content: errText,
				IsError: true,
			}}
			return
		}

		ch <- tool.Event{Kind: tool.EventResult, Result: &tool.Result{
			Content: collected.String(),
		}}
	}()

	return ch, nil
}
