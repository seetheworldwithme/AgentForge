package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resolveUnder 是 @ 注入的安全核心:相对路径必须落在工作目录内,
// 任何形式的 ../ 逃逸都要被拒绝。
func TestResolveUnder(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		rel  string
		want bool
	}{
		{"empty", "", true},
		{"subdir", "sub", true},
		{"nested", "a/b/c", true},
		{"dot", ".", true},
		{"parent", "..", false},
		{"escape", "../escape", false},
		{"sneaky", "a/../../../escape", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := resolveUnder(root, c.rel); ok != c.want {
				t.Errorf("resolveUnder(%q) ok=%v, want %v", c.rel, ok, c.want)
			}
		})
	}
}

// 单文件:内容与文件名都应出现在注入块中。
func TestBuildAttachmentsFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "main.go"), "package main\n")
	got := buildAttachments(root, []string{"main.go"})
	for _, want := range []string{"<attachments>", "main.go", "package main", "```go"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in block, got:\n%s", want, got)
		}
	}
}

// 超过上限的单文件应截断并标注。
func TestBuildAttachmentsFileTruncation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "big.txt"), strings.Repeat("x", maxFileBytes+1024))
	got := buildAttachments(root, []string{"big.txt"})
	if !strings.Contains(got, "已截断") {
		t.Errorf("expected truncation marker, got prefix:\n%s", got[:min(200, len(got))])
	}
}

// 文件夹:展开为目录树,排除 node_modules 等噪音目录。
func TestBuildAttachmentsDirTree(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "pkg"))
	mustWrite(t, filepath.Join(root, "pkg", "a.go"), "package pkg\n")
	mustMkdir(t, filepath.Join(root, "pkg", "node_modules"))
	mustWrite(t, filepath.Join(root, "pkg", "node_modules", "dep.js"), "x")

	got := buildAttachments(root, []string{"pkg"})
	if !strings.Contains(got, "pkg/a.go") {
		t.Errorf("expected pkg/a.go in tree, got:\n%s", got)
	}
	if strings.Contains(got, "node_modules") {
		t.Errorf("node_modules should be excluded, got:\n%s", got)
	}
}

// 越界路径应标注跳过,不读取工作目录之外的文件。
func TestBuildAttachmentsEscape(t *testing.T) {
	root := t.TempDir()
	got := buildAttachments(root, []string{"../../../etc/passwd"})
	if !strings.Contains(got, "已跳过") {
		t.Errorf("expected skip marker for escape, got:\n%s", got)
	}
}

// 二进制文件应跳过内容(避免把不可读字节塞进上下文)。
func TestBuildAttachmentsBinarySkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte{0x00, 0x01, 0xFF, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	got := buildAttachments(root, []string{"bin"})
	if !strings.Contains(got, "二进制") {
		t.Errorf("expected binary skip marker, got:\n%s", got)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
