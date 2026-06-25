package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/store"
)

func TestManagerDiscoversGlobalAndWorkspaceSkills(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 仅区分「全局」与「当前工作目录」两类来源；项目根（启动目录）不再作为独立来源。
	globalRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	writeSkill(t, filepath.Join(globalRoot, ".agentforge", "skills", "global-skill", "SKILL.md"), "global-skill", "Global skill")
	writeSkill(t, filepath.Join(workspaceRoot, ".agentforge", "skills", "workspace-skill", "SKILL.md"), "workspace-skill", "Workspace skill")

	m := NewManager(Options{
		DB:         db,
		GlobalRoot: globalRoot,
		WorkDir:    func() string { return workspaceRoot },
	})

	items, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 skills, got %d: %#v", len(items), items)
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
	writeSkill(t, filepath.Join(root, ".agentforge", "skills", "enabled", "SKILL.md"), "enabled", "Use this enabled skill")
	writeSkill(t, filepath.Join(root, ".agentforge", "skills", "disabled", "SKILL.md"), "disabled", "Do not include this skill")

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

// TestManagerListReflectsWorkDirChanges 验证 List 动态反映工作目录变化：workDir
// 切换后，workspace 来源的 skills 随之改变。这是「切换工作目录后斜杠菜单应刷新」
// 的后端前提——若不成立，前端重载也无济于事。
func TestManagerListReflectsWorkDirChanges(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	globalRoot := t.TempDir()
	writeSkill(t, filepath.Join(globalRoot, ".agentforge", "skills", "g-skill", "SKILL.md"), "g-skill", "Global")
	dirA := t.TempDir()
	writeSkill(t, filepath.Join(dirA, ".agentforge", "skills", "a-skill", "SKILL.md"), "a-skill", "A")
	dirB := t.TempDir()
	writeSkill(t, filepath.Join(dirB, ".agentforge", "skills", "b-skill", "SKILL.md"), "b-skill", "B")

	current := dirA
	m := NewManager(Options{DB: db, GlobalRoot: globalRoot, WorkDir: func() string { return current }})

	hasName := func(items []Skill, name string) bool {
		for _, it := range items {
			if it.Name == name {
				return true
			}
		}
		return false
	}

	list1, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if !hasName(list1, "a-skill") || hasName(list1, "b-skill") {
		t.Fatalf("workDir=dirA: expected a-skill present, b-skill absent; got %+v", list1)
	}

	current = dirB
	list2, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if !hasName(list2, "b-skill") || hasName(list2, "a-skill") {
		t.Fatalf("workDir=dirB: expected b-skill present, a-skill absent; got %+v", list2)
	}
}

// TestManagerIndexInstructionsAndContent 验证按需加载的两个新方法：
// IndexInstructions 只输出 enabled skill 的 id+name+description 一行（不含 SKILL.md 全文、
// 不含 disabled），GetSkillContent 按 id 返回全文、未知 id 报错。
func TestManagerIndexInstructionsAndContent(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, ".agentforge", "skills", "alpha", "SKILL.md"), "alpha", "Alpha skill for X")
	writeSkill(t, filepath.Join(root, ".agentforge", "skills", "beta", "SKILL.md"), "beta", "Beta skill for Y")

	m := NewManager(Options{DB: db, GlobalRoot: root})

	items, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	var alphaID, betaID string
	for _, it := range items {
		switch it.Name {
		case "alpha":
			alphaID = it.ID
		case "beta":
			betaID = it.ID
		}
	}
	if err := m.SetEnabled(betaID, false); err != nil {
		t.Fatal(err)
	}

	// IndexInstructions：只含 enabled skill 的 id+description，不含全文、不含 disabled
	idx, err := m.IndexInstructions()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(idx, alphaID) || !strings.Contains(idx, "Alpha skill for X") {
		t.Fatalf("index missing enabled skill: %s", idx)
	}
	if strings.Contains(idx, "Beta skill") {
		t.Fatalf("index includes disabled skill: %s", idx)
	}
	if strings.Contains(idx, "# alpha") { // SKILL.md 正文标题，索引里不该出现
		t.Fatalf("index leaked full SKILL.md: %s", idx)
	}

	// GetSkillContent：按 id 返回全文
	content, err := m.GetSkillContent(alphaID)
	if err != nil {
		t.Fatalf("GetSkillContent: %v", err)
	}
	if !strings.Contains(content, "# alpha") {
		t.Fatalf("GetSkillContent missing full body: %s", content)
	}
	if _, err := m.GetSkillContent("global:does-not-exist"); err == nil {
		t.Fatalf("GetSkillContent should error on unknown id")
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
