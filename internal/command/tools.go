package command

import (
	"context"
	"runtime"

	"github.com/agentforge/agentforge/internal/tool"
)

// SystemInfoTool 获取系统信息，实现 tool.Tool。
type SystemInfoTool struct {
	runner *Runner
}

func NewSystemInfoTool() *SystemInfoTool {
	return &SystemInfoTool{runner: NewRunner()}
}

func (t *SystemInfoTool) Name() string        { return "get_system_info" }
func (t *SystemInfoTool) Description() string { return "获取系统信息（OS、运行时）" }

func (t *SystemInfoTool) Schema() []byte {
	return []byte(`{"type":"object","properties":{}}`)
}

func (t *SystemInfoTool) Execute(ctx context.Context, args []byte) (<-chan tool.Event, error) {
	spec := t.platformSpec()
	return t.runner.Run(ctx, spec)
}

func (t *SystemInfoTool) platformSpec() CommandSpec {
	if runtime.GOOS == "windows" {
		return CommandSpec{
			Title:  "系统信息",
			Binary: "powershell.exe",
			Args:   []string{"-NoProfile", "-Command", "Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,OSArchitecture | Format-List"},
		}
	}
	return CommandSpec{
		Title:  "系统信息",
		Binary: "uname",
		Args:   []string{"-a"},
	}
}
