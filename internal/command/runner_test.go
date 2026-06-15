package command

import (
	"context"
	"runtime"
	"testing"

	"github.com/agentforge/agentforge/internal/tool"
)

func TestRunEchoCommand(t *testing.T) {
	var spec CommandSpec
	if runtime.GOOS == "windows" {
		spec = CommandSpec{Title: "echo", Binary: "cmd", Args: []string{"/c", "echo hello"}}
	} else {
		spec = CommandSpec{Title: "echo", Binary: "echo", Args: []string{"hello"}}
	}

	runner := NewRunner()
	var deltas []string
	var resultContent string
	var resultIsError bool

	events, err := runner.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	for ev := range events {
		switch ev.Kind {
		case tool.EventDelta:
			deltas = append(deltas, ev.Text)
		case tool.EventResult:
			if ev.Result != nil {
				resultContent = ev.Result.Content
				resultIsError = ev.Result.IsError
			}
		}
	}

	if len(deltas) == 0 {
		t.Fatal("expected at least one delta line")
	}
	if resultContent == "" {
		t.Fatal("expected non-empty result content")
	}
	if resultIsError {
		t.Error("expected successful result, got error")
	}
}

func TestRunFailingCommand(t *testing.T) {
	spec := CommandSpec{Title: "bad", Binary: "this-binary-does-not-exist-xyz", Args: nil}
	runner := NewRunner()
	var resultContent string
	var resultIsError bool

	events, err := runner.Run(context.Background(), spec)
	if err == nil {
		for ev := range events {
			if ev.Kind == tool.EventResult && ev.Result != nil {
				resultContent = ev.Result.Content
				resultIsError = ev.Result.IsError
			}
		}
	}

	if !resultIsError {
		t.Error("expected error result for nonexistent binary")
	}
	if resultContent == "" {
		t.Error("expected non-empty error content")
	}
}
