package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/tools"
)

func autoAllowGate() tools.GateInterface {
	return tools.NewAutoAllowGate()
}

func TestFileRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("line1\nline2\n"), 0o644)

	r, err := (FileRead{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(p)+`"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Content, "line1") {
		t.Errorf("content=%q", r.Content)
	}
}

func TestFileWriteRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")
	g := autoAllowGate()
	_, err := (FileWrite{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(p)+`","content":"hi"}`, g)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "hi" {
		t.Errorf("file=%q", b)
	}
}

func TestFileEditReplacesFirstMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "e.txt")
	os.WriteFile(p, []byte("foo bar foo"), 0o644)
	_, err := (FileEdit{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(p)+`","old":"foo","new":"qux"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "qux bar foo" {
		t.Errorf("got=%q want 'qux bar foo'", b)
	}
}

func TestGrepFindsMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nworld\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello again\n"), 0o644)
	r, err := (Grep{}).Run(context.Background(),
		`{"path":"`+filepath.ToSlash(dir)+`","pattern":"hello"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Content, "a.txt") || !strings.Contains(r.Content, "b.txt") {
		t.Errorf("content=%q", r.Content)
	}
}

func TestBashRunsCommand(t *testing.T) {
	// `go env GOOS` is safe everywhere Go is installed.
	r, err := (Bash{}).Run(context.Background(),
		`{"command":"go env GOOS"}`, autoAllowGate())
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError {
		t.Errorf("bash failed: %s", r.Content)
	}
	if !strings.Contains(r.Content, "windows") && !strings.Contains(r.Content, "linux") &&
		!strings.Contains(r.Content, "darwin") {
		t.Errorf("unexpected output: %s", r.Content)
	}
}
