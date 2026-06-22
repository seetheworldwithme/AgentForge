package skills

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agent-rust/core/internal/store"
)

const disabledSettingKey = "skills.disabled"

type Source string

const (
	SourceGlobal    Source = "global"
	SourceProject   Source = "project"
	SourceWorkspace Source = "workspace"
)

type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      Source `json:"source"`
	Path        string `json:"path"`
	Enabled     bool   `json:"enabled"`
}

type Options struct {
	DB          *store.DB
	GlobalRoot  string
	ProjectRoot string
	WorkDir     func() string
}

type Manager struct {
	db          *store.DB
	globalRoot  string
	projectRoot string
	workDir     func() string
}

func NewManager(opts Options) *Manager {
	if opts.GlobalRoot == "" {
		if home, err := os.UserHomeDir(); err == nil {
			opts.GlobalRoot = home
		}
	}
	if opts.ProjectRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			opts.ProjectRoot = wd
		}
	}
	return &Manager{
		db:          opts.DB,
		globalRoot:  opts.GlobalRoot,
		projectRoot: opts.ProjectRoot,
		workDir:     opts.WorkDir,
	}
}

func (m *Manager) List() ([]Skill, error) {
	disabled, err := m.disabled()
	if err != nil {
		return nil, err
	}

	var out []Skill
	seen := map[string]bool{}
	for _, src := range m.sources() {
		for _, dir := range skillDirs(src.root) {
			entries, err := os.ReadDir(dir)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				path := filepath.Join(dir, entry.Name(), "SKILL.md")
				if _, err := os.Stat(path); err != nil {
					continue
				}
				id := string(src.kind) + ":" + entry.Name()
				if seen[id] {
					continue
				}
				seen[id] = true
				name, desc := readMetadata(path, entry.Name())
				out = append(out, Skill{
					ID: id, Name: name, Description: desc, Source: src.kind,
					Path: path, Enabled: !disabled[id],
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (m *Manager) SetEnabled(id string, enabled bool) error {
	disabled, err := m.disabled()
	if err != nil {
		return err
	}
	if enabled {
		delete(disabled, id)
	} else {
		disabled[id] = true
	}
	b, err := json.Marshal(disabled)
	if err != nil {
		return err
	}
	return m.db.SetSetting(disabledSettingKey, string(b))
}

func (m *Manager) EnabledInstructions() (string, error) {
	items, err := m.List()
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		b, err := os.ReadFile(item.Path)
		if err != nil {
			return "", err
		}
		if sb.Len() == 0 {
			sb.WriteString("Available enabled skills. Follow a skill when it applies to the user's request.\n")
		}
		sb.WriteString("\n<skill id=\"")
		sb.WriteString(item.ID)
		sb.WriteString("\" name=\"")
		sb.WriteString(item.Name)
		sb.WriteString("\" source=\"")
		sb.WriteString(string(item.Source))
		sb.WriteString("\">\n")
		sb.Write(b)
		sb.WriteString("\n</skill>\n")
	}
	return sb.String(), nil
}

func (m *Manager) disabled() (map[string]bool, error) {
	out := map[string]bool{}
	if m.db == nil {
		return out, nil
	}
	raw, err := m.db.GetSetting(disabledSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

type sourceRoot struct {
	kind Source
	root string
}

func (m *Manager) sources() []sourceRoot {
	var out []sourceRoot
	if m.globalRoot != "" {
		out = append(out, sourceRoot{kind: SourceGlobal, root: m.globalRoot})
	}
	if m.projectRoot != "" {
		out = append(out, sourceRoot{kind: SourceProject, root: m.projectRoot})
	}
	if m.workDir != nil {
		if wd := m.workDir(); wd != "" && wd != m.projectRoot {
			out = append(out, sourceRoot{kind: SourceWorkspace, root: wd})
		}
	}
	return out
}

func skillDirs(root string) []string {
	return []string{
		filepath.Join(root, ".agent", "skills"),
		filepath.Join(root, ".agents", "skills"),
	}
}

func readMetadata(path, fallback string) (string, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return fallback, ""
	}
	name := fallback
	desc := ""
	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return name, desc
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			break
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			if val != "" {
				name = val
			}
		case "description":
			desc = val
		}
	}
	return name, desc
}
