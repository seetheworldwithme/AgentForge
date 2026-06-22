package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/store"
)

func TestManagerDiscoversGlobalProjectAndWorkspaceSkills(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	globalRoot := t.TempDir()
	projectRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	writeSkill(t, filepath.Join(globalRoot, ".agent", "skills", "global-skill", "SKILL.md"), "global-skill", "Global skill")
	writeSkill(t, filepath.Join(projectRoot, ".agents", "skills", "project-skill", "SKILL.md"), "project-skill", "Project skill")
	writeSkill(t, filepath.Join(workspaceRoot, ".agent", "skills", "workspace-skill", "SKILL.md"), "workspace-skill", "Workspace skill")

	m := NewManager(Options{
		DB:          db,
		GlobalRoot:  globalRoot,
		ProjectRoot: projectRoot,
		WorkDir:     func() string { return workspaceRoot },
	})

	items, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 skills, got %d: %#v", len(items), items)
	}

	byName := map[string]Skill{}
	for _, item := range items {
		byName[item.Name] = item
		if !item.Enabled {
			t.Fatalf("newly discovered skill %s should be enabled by default", item.Name)
		}
	}
	if byName["global-skill"].Source != SourceGlobal {
		t.Fatalf("global skill source = %q", byName["global-skill"].Source)
	}
	if byName["project-skill"].Source != SourceProject {
		t.Fatalf("project skill source = %q", byName["project-skill"].Source)
	}
	if byName["workspace-skill"].Source != SourceWorkspace {
		t.Fatalf("workspace skill source = %q", byName["workspace-skill"].Source)
	}
}

func TestManagerPersistsDisabledSkillAndBuildsPrompt(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, ".agent", "skills", "enabled", "SKILL.md"), "enabled", "Use this enabled skill")
	writeSkill(t, filepath.Join(root, ".agent", "skills", "disabled", "SKILL.md"), "disabled", "Do not include this skill")

	m := NewManager(Options{DB: db, GlobalRoot: root})
	items, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	var disabledID string
	for _, item := range items {
		if item.Name == "disabled" {
			disabledID = item.ID
		}
	}
	if disabledID == "" {
		t.Fatal("disabled test skill not discovered")
	}
	if err := m.SetEnabled(disabledID, false); err != nil {
		t.Fatal(err)
	}

	prompt, err := m.EnabledInstructions()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Use this enabled skill") {
		t.Fatalf("prompt missing enabled skill: %s", prompt)
	}
	if strings.Contains(prompt, "Do not include this skill") {
		t.Fatalf("prompt includes disabled skill: %s", prompt)
	}
}

func writeSkill(t *testing.T, path, name, description string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\n" + description + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
