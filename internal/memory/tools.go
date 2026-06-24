package memory

import (
	"context"
	"encoding/json"

	"github.com/agent-rust/core/internal/tools"
)

// saveTool 实现 memory_save：校验并写入记忆条目，自动维护索引。
type saveTool struct{ store *MemoryStore }

func (t *saveTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "memory_save",
		Description: "记录一条跨会话记忆（仅记「长期有用、代码/git 查不到」的事实：用户偏好、" +
			"工作约定、环境坑、外部资源）。同名 name 会覆盖更新，不要为重复事实新建。",
		Parameters: `{"type":"object","properties":{
			"name":{"type":"string","description":"kebab-case 唯一标识，如 go-env"},
			"description":{"type":"string","description":"一行摘要，召回时判断相关性用"},
			"type":{"type":"string","enum":["user","feedback","project","reference"],
				"description":"user=用户偏好; feedback=工作指导; project=进行中的工作/约束; reference=外部资源"},
			"body":{"type":"string","description":"markdown 正文；feedback/project 类末尾带 **Why:** 与 **How to apply:**"}
		},"required":["name","description","type","body"]}`,
	}
}

func (t *saveTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Name, Description, Type, Body string
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if err := t.store.Save(Entry{
		Name: p.Name, Description: p.Description, Type: Type(p.Type), Body: p.Body,
	}); err != nil {
		return tools.Result{Content: "memory_save 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "已记忆 " + p.Name}, nil
}

// readTool 实现 memory_read：读取单条记忆全文。
type readTool struct{ store *MemoryStore }

func (t *readTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "memory_read",
		Description: "读取一条记忆的完整内容（frontmatter + 正文）。",
		Parameters:  `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
	}
}

func (t *readTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct{ Name string }
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	e, err := t.store.Get(p.Name)
	if err != nil {
		return tools.Result{Content: "memory_read 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: formatEntry(e)}, nil
}

// deleteTool 实现 memory_delete：删除一条记忆。
type deleteTool struct{ store *MemoryStore }

func (t *deleteTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "memory_delete",
		Description: "删除一条记忆（仅在确认其已过时/有误时使用）。",
		Parameters:  `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`,
	}
}

func (t *deleteTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct{ Name string }
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if err := t.store.Delete(p.Name); err != nil {
		return tools.Result{Content: "memory_delete 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "已删除记忆 " + p.Name}, nil
}

// Tools 返回全部 memory 工具，供 main.go 注册进 tools.Registry。
func Tools(s *MemoryStore) []tools.Tool {
	return []tools.Tool{&saveTool{store: s}, &readTool{store: s}, &deleteTool{store: s}}
}
