package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agent-rust/core/internal/tools"
)

// createTool 实现 todo_create：新建一条 pending 任务，可声明依赖。
type createTool struct{ store *Store }

func (createTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "todo_create",
		Description: "新建一条待办任务（pending）。用于复杂多步任务（≥3 步）显式跟踪进度：" +
			"动手前先把步骤列出来，每完成一步用 todo_update 标记。简单单步任务不必用。",
		Parameters: `{"type":"object","properties":{
			"subject":{"type":"string","description":"任务标题，祈使句，如「修复登录 bug」"},
			"description":{"type":"string","description":"可选：详情/验收标准"},
			"active_form":{"type":"string","description":"可选：进行时形式，如「修复登录 bug 中」"},
			"blocks":{"type":"array","items":{"type":"integer"},"description":"可选：本任务阻塞哪些任务 id"},
			"blocked_by":{"type":"array","items":{"type":"integer"},"description":"可选：本任务被哪些任务 id 阻塞（须先完成）"}
		},"required":["subject"]}`,
	}
}

func (t *createTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		Subject     string `json:"subject"`
		Description string `json:"description"`
		ActiveForm  string `json:"active_form"`
		Blocks      []int  `json:"blocks"`
		BlockedBy   []int  `json:"blocked_by"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(p.Subject) == "" {
		return tools.Result{Content: "todo_create 失败：subject 不能为空", IsError: true}, nil
	}
	task := t.store.Create(p.Subject, p.Description, p.ActiveForm, p.Blocks, p.BlockedBy)
	return tools.Result{Content: fmt.Sprintf("已新建待办 #%d「%s」。用 todo_list 查看全部。", task.ID, task.Subject)}, nil
}

// updateTool 实现 todo_update：局部更新一条任务，可追加依赖。
type updateTool struct{ store *Store }

func (updateTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "todo_update",
		Description: "更新一条待办。状态机：pending → in_progress → completed。" +
			"开始做某任务前先标 in_progress（若被未完成的依赖阻塞会报错）；彻底完成（含验证）才标 completed。" +
			"同一时刻只允许一条 in_progress——设新的会自动把旧的置回 pending。",
		Parameters: `{"type":"object","properties":{
			"id":{"type":"integer","description":"任务 id"},
			"status":{"type":"string","enum":["pending","in_progress","completed"]},
			"subject":{"type":"string"},
			"description":{"type":"string"},
			"active_form":{"type":"string"},
			"add_blocks":{"type":"array","items":{"type":"integer"},"description":"追加：本任务阻塞的任务 id"},
			"add_blocked_by":{"type":"array","items":{"type":"integer"},"description":"追加：阻塞本任务的任务 id"}
		},"required":["id"]}`,
	}
}

func (t *updateTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		ID           int     `json:"id"`
		Status       *Status `json:"status"`
		Subject      *string `json:"subject"`
		Description  *string `json:"description"`
		ActiveForm   *string `json:"active_form"`
		AddBlocks    []int   `json:"add_blocks"`
		AddBlockedBy []int   `json:"add_blocked_by"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if p.Status != nil && !validStatus(*p.Status) {
		return tools.Result{Content: "todo_update 失败：非法 status（可选 pending/in_progress/completed）", IsError: true}, nil
	}
	task, err := t.store.Update(p.ID, p.Status, p.Subject, p.Description, p.ActiveForm, p.AddBlocks, p.AddBlockedBy)
	if err != nil {
		return tools.Result{Content: "todo_update 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: fmt.Sprintf("已更新待办 #%d「%s」→ %s。", task.ID, task.Subject, task.Status)}, nil
}

// listTool 实现 todo_list：列出当前会话全部待办（按状态分组，含依赖标记）。
type listTool struct{ store *Store }

func (listTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "todo_list",
		Description: "列出当前会话的全部待办（按 进行中/待办/已完成 分组，含依赖标记）。做下一步前查看进度，避免重复或遗漏。",
		Parameters:  `{}`,
	}
}

func (t *listTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	return tools.Result{Content: renderList(t.store.List())}, nil
}

// deleteTool 实现 todo_delete：删除一条任务。
type deleteTool struct{ store *Store }

func (deleteTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "todo_delete",
		Description: "删除一条待办（仅在任务作废/建错时使用；完成的任务用 todo_update 标 completed，不要删除）。",
		Parameters:  `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`,
	}
}

func (t *deleteTool) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct{ ID int `json:"id"` }
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if err := t.store.Delete(p.ID); err != nil {
		return tools.Result{Content: "todo_delete 失败: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: fmt.Sprintf("已删除待办 #%d。", p.ID)}, nil
}

// Tools 返回全部 todo 工具，供 main.go 注册进 tools.Registry。
func Tools(s *Store) []tools.Tool {
	return []tools.Tool{&createTool{store: s}, &updateTool{store: s}, &listTool{store: s}, &deleteTool{store: s}}
}

// renderList 把任务列表渲染成给模型/用户读的文本（ASCII 标记，不用 emoji）：
//
//	[~] 进行中 / [ ] 待办 / [x] 已完成；依赖用（阻塞 #3）/（被 #1 阻塞）标注。
func renderList(list []Task) string {
	if len(list) == 0 {
		return "（当前会话暂无待办）"
	}
	var inProg, pending, done []Task
	for _, t := range list {
		switch t.Status {
		case StatusInProgress:
			inProg = append(inProg, t)
		case StatusCompleted:
			done = append(done, t)
		default:
			pending = append(pending, t)
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "待办清单（共 %d 项，进行中 %d，待办 %d，已完成 %d）：\n",
		len(list), len(inProg), len(pending), len(done))
	for _, t := range inProg {
		fmt.Fprintf(&sb, "  [~] #%d %s%s\n", t.ID, t.Subject, depSuffix(t))
	}
	for _, t := range pending {
		fmt.Fprintf(&sb, "  [ ] #%d %s%s\n", t.ID, t.Subject, depSuffix(t))
	}
	for _, t := range done {
		fmt.Fprintf(&sb, "  [x] #%d %s%s\n", t.ID, t.Subject, depSuffix(t))
	}
	return sb.String()
}

// depSuffix 渲染依赖标记：（阻塞 #3）；（被 #1 阻塞）；无依赖返回空串。
func depSuffix(t Task) string {
	var parts []string
	if len(t.Blocks) > 0 {
		parts = append(parts, "阻塞 "+joinIDs(t.Blocks))
	}
	if len(t.BlockedBy) > 0 {
		parts = append(parts, "被 "+joinIDs(t.BlockedBy)+" 阻塞")
	}
	if len(parts) == 0 {
		return ""
	}
	return "（" + strings.Join(parts, "；") + "）"
}

func joinIDs(ids []int) string {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = fmt.Sprintf("#%d", id)
	}
	return strings.Join(strs, ",")
}
