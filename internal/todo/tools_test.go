package todo

import (
	"context"
	"strings"
	"testing"

	"github.com/agent-rust/core/internal/tools"
)

// runTool 调用一个 tool 的 Run（todo 工具不触碰 gate，传 nil）。
func runTool(t *testing.T, ts tools.Tool, args string) tools.Result {
	t.Helper()
	r, err := ts.Run(context.Background(), args, nil)
	if err != nil {
		t.Fatalf("tool Run error: %v", err)
	}
	return r
}

func TestToolCreateListUpdateDelete(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	var createT, listT, updateT, deleteT tools.Tool
	for _, x := range Tools(s) {
		switch x.Spec().Name {
		case "todo_create":
			createT = x
		case "todo_list":
			listT = x
		case "todo_update":
			updateT = x
		case "todo_delete":
			deleteT = x
		}
	}

	if r := runTool(t, createT, `{"subject":"任务一","description":"详情"}`); r.IsError {
		t.Errorf("create 失败: %v", r.Content)
	}
	if r := runTool(t, createT, `{"subject":""}`); !r.IsError {
		t.Errorf("空 subject 应报错")
	}
	if r := runTool(t, listT, `{}`); !strings.Contains(r.Content, "任务一") || !strings.Contains(r.Content, "共 1 项") {
		t.Errorf("list 应含任务一与计数，got: %q", r.Content)
	}
	if r := runTool(t, updateT, `{"id":1,"status":"in_progress"}`); r.IsError {
		t.Errorf("update in_progress 失败: %v", r.Content)
	}
	if r := runTool(t, listT, `{}`); !strings.Contains(r.Content, "[~]") {
		t.Errorf("list 应显示进行中标记 [~]，got: %q", r.Content)
	}
	if r := runTool(t, updateT, `{"id":1,"status":"bogus"}`); !r.IsError {
		t.Errorf("非法 status 应报错")
	}
	if r := runTool(t, updateT, `{"id":1,"status":"completed"}`); r.IsError {
		t.Errorf("update completed 失败: %v", r.Content)
	}
	if r := runTool(t, listT, `{}`); !strings.Contains(r.Content, "[x]") {
		t.Errorf("list 应显示完成标记 [x]，got: %q", r.Content)
	}
	if r := runTool(t, deleteT, `{"id":1}`); r.IsError {
		t.Errorf("delete 失败: %v", r.Content)
	}
	if r := runTool(t, listT, `{}`); !strings.Contains(r.Content, "暂无待办") {
		t.Errorf("删除后 list 应为空提示，got: %q", r.Content)
	}
}

// TestToolCreateWithDeps 验证通过工具创建带依赖的任务 + 依赖约束与渲染在工具层生效。
func TestToolCreateWithDeps(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	var createT, updateT, listT tools.Tool
	for _, x := range Tools(s) {
		switch x.Spec().Name {
		case "todo_create":
			createT = x
		case "todo_update":
			updateT = x
		case "todo_list":
			listT = x
		}
	}
	runTool(t, createT, `{"subject":"A"}`)                    // #1
	runTool(t, createT, `{"subject":"B","blocked_by":[1]}`)   // #2 被 #1 阻塞

	// #1 未完成，#2 不能开始
	if r := runTool(t, updateT, `{"id":2,"status":"in_progress"}`); !r.IsError || !strings.Contains(r.Content, "阻塞") {
		t.Errorf("依赖未完成时应报阻塞错误，got: %v", r.Content)
	}
	// 完成 #1 后 #2 可开始
	runTool(t, updateT, `{"id":1,"status":"completed"}`)
	if r := runTool(t, updateT, `{"id":2,"status":"in_progress"}`); r.IsError {
		t.Errorf("依赖完成后 #2 应可开始，got: %v", r.Content)
	}
	// list 显示依赖标记
	if r := runTool(t, listT, `{}`); !strings.Contains(r.Content, "被 #1 阻塞") {
		t.Errorf("list 应显示依赖标记，got: %q", r.Content)
	}
}

func TestRenderListEmpty(t *testing.T) {
	if got := renderList(nil); !strings.Contains(got, "暂无待办") {
		t.Errorf("空列表应有占位提示，got: %q", got)
	}
}
