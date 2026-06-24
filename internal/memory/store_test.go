package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*MemoryStore, string) {
	t.Helper()
	wd := t.TempDir()
	s := New(func() string { return wd }, "")
	return s, wd
}

func TestSaveAndList(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.Save(Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "正文"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "go-env" || got[0].Description != "Go 环境坑" ||
		got[0].Type != TypeProject || got[0].Body != "正文" {
		t.Fatalf("unexpected list: %+v", got)
	}
	if got[0].UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt should come from file mtime, got zero")
	}
}

func TestSaveRejectsBadName(t *testing.T) {
	s, _ := newTestStore(t)
	for _, bad := range []string{"", "UPPER", "has space", "../etc", "a/b"} {
		if err := s.Save(Entry{Name: bad, Description: "d", Type: TypeUser}); err == nil {
			t.Errorf("expected error for name %q", bad)
		}
	}
}

func TestSaveRejectsBadTypeAndOversize(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Save(Entry{Name: "ok", Description: "d", Type: "bogus"}); err == nil {
		t.Errorf("expected error for bad type")
	}
	longBody := make([]byte, MaxBodyBytes+1)
	for i := range longBody {
		longBody[i] = 'x'
	}
	if err := s.Save(Entry{Name: "ok", Description: "d", Type: TypeUser, Body: string(longBody)}); err == nil {
		t.Errorf("expected error for oversize body")
	}
}

func TestSaveUpdatesExisting(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "x", Description: "旧", Type: TypeUser, Body: "a"})
	time.Sleep(15 * time.Millisecond)
	_ = s.Save(Entry{Name: "x", Description: "新", Type: TypeUser, Body: "b"})
	got, _ := s.List()
	if len(got) != 1 || got[0].Description != "新" || got[0].Body != "b" {
		t.Fatalf("update failed: %+v", got)
	}
}

func TestGetAndDelete(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.Save(Entry{Name: "x", Description: "d", Type: TypeUser, Body: "正文"})
	e, err := s.Get("x")
	if err != nil || e.Body != "正文" {
		t.Fatalf("get: %v %+v", err, e)
	}
	if _, err := s.Get("missing"); err == nil {
		t.Errorf("expected error for missing get")
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := s.List()
	if len(got) != 0 {
		t.Fatalf("expected empty after delete, got %+v", got)
	}
}

func TestListSkipsIndexFile(t *testing.T) {
	s, wd := newTestStore(t)
	_ = s.Save(Entry{Name: "x", Description: "d", Type: TypeUser, Body: "b"})
	_ = os.WriteFile(filepath.Join(wd, DirName, IndexFile), []byte("# Memory Index"), 0o644)
	got, _ := s.List()
	for _, e := range got {
		if e.Name == "MEMORY" {
			t.Fatalf("index file leaked into list: %+v", got)
		}
	}
}

func TestListSkipsUnparseable(t *testing.T) {
	s, wd := newTestStore(t)
	_ = s.Save(Entry{Name: "good", Description: "d", Type: TypeUser, Body: "b"})
	_ = os.WriteFile(filepath.Join(wd, DirName, "bad.md"), []byte("\x00\x01garbage"), 0o644)
	got, _ := s.List()
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("expected only good entry, got %+v", got)
	}
}
