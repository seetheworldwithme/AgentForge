package todo

import (
	"reflect"
	"testing"
)

func ptrStatus(s Status) *Status { return &s }
func ptrString(s string) *string { return &s }

func TestCreateAndList(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	a := s.Create("任务A", "详情A", "", nil, nil)
	b := s.Create("任务B", "", "", nil, nil)
	if a.ID != 1 || b.ID != 2 {
		t.Errorf("id 自增：got %d,%d want 1,2", a.ID, b.ID)
	}
	list := s.List()
	if len(list) != 2 || list[0].Status != StatusPending {
		t.Errorf("List/状态错误：got %+v", list)
	}
}

func TestUpdate_Status(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	s.Create("A", "", "", nil, nil)
	task, err := s.Update(1, ptrStatus(StatusInProgress), nil, nil, nil, nil, nil)
	if err != nil || task.Status != StatusInProgress {
		t.Errorf("update in_progress 失败: err=%v status=%s", err, task.Status)
	}
}

// TestUpdate_OnlyOneInProgress 验证状态机核心约束：同一时刻只允许一条 in_progress。
func TestUpdate_OnlyOneInProgress(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	s.Create("A", "", "", nil, nil)
	s.Create("B", "", "", nil, nil)
	s.Update(1, ptrStatus(StatusInProgress), nil, nil, nil, nil, nil)
	s.Update(2, ptrStatus(StatusInProgress), nil, nil, nil, nil, nil)
	list := s.List()
	inProg := 0
	for _, x := range list {
		if x.Status == StatusInProgress {
			inProg++
		}
	}
	if inProg != 1 || list[0].Status != StatusPending || list[1].Status != StatusInProgress {
		t.Errorf("一次只一个进行中约束失败: inProg=%d %+v", inProg, list)
	}
}

// TestUpdate_BlockedByConstraint 验证依赖约束：blockedBy 未完成时禁止 in_progress。
func TestUpdate_BlockedByConstraint(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	s.Create("A", "", "", nil, nil)      // #1
	s.Create("B", "", "", nil, []int{1}) // #2 被 #1 阻塞
	if _, err := s.Update(2, ptrStatus(StatusInProgress), nil, nil, nil, nil, nil); err == nil {
		t.Errorf("被未完成依赖阻塞时应报错")
	}
	s.Update(1, ptrStatus(StatusCompleted), nil, nil, nil, nil, nil)
	if _, err := s.Update(2, ptrStatus(StatusInProgress), nil, nil, nil, nil, nil); err != nil {
		t.Errorf("依赖完成后应可开始，got err=%v", err)
	}
}

// TestUpdate_AddDeps 验证追加依赖（addBlocks/addBlockedBy 去重）。
func TestUpdate_AddDeps(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	s.Create("A", "", "", nil, nil)
	s.Create("B", "", "", nil, nil)
	task, _ := s.Update(1, nil, nil, nil, nil, []int{2}, nil)
	if !reflect.DeepEqual(task.Blocks, []int{2}) {
		t.Errorf("add_blocks 失败: %+v", task.Blocks)
	}
	task, _ = s.Update(1, nil, nil, nil, nil, []int{2}, nil) // 重复加 2，应去重
	if len(task.Blocks) != 1 || task.Blocks[0] != 2 {
		t.Errorf("add_blocks 去重失败: %+v", task.Blocks)
	}
}

func TestUpdate_PartialFields(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	s.Create("A", "oldDesc", "oldForm", nil, nil)
	task, _ := s.Update(1, nil, ptrString("新标题"), nil, nil, nil, nil)
	if task.Subject != "新标题" || task.Description != "oldDesc" || task.ActiveForm != "oldForm" {
		t.Errorf("部分更新错误（未传字段不应被改）: %+v", task)
	}
}

func TestDelete(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	s.Create("A", "", "", nil, nil)
	if err := s.Delete(1); err != nil {
		t.Errorf("Delete 失败: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("删除后应空")
	}
	if err := s.Delete(1); err == nil {
		t.Errorf("重复删除应报错")
	}
}

func TestUpdateNotFound(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	if _, err := s.Update(99, ptrStatus(StatusCompleted), nil, nil, nil, nil, nil); err == nil {
		t.Errorf("更新不存在的 id 应报错")
	}
}

func TestSessionIsolation(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	s.Create("A-s1", "", "", nil, nil)
	s.SetCurrent("s2")
	s.Create("B-s2", "", "", nil, nil)
	if len(s.List()) != 1 {
		t.Errorf("s2 应只有 1 项")
	}
	if got := s.ListFor("s1"); len(got) != 1 || got[0].Subject != "A-s1" {
		t.Errorf("ListFor(s1) 应只有 A-s1: %+v", got)
	}
}

// TestOnChange 验证 Create/Update/Delete 触发 onChange 回调（带当前会话清单副本）。
func TestOnChange(t *testing.T) {
	s := New()
	s.SetCurrent("s1")
	var got []Task
	s.SetOnChange(func(list []Task) { got = list })

	s.Create("A", "", "", nil, nil)
	if len(got) != 1 || got[0].Subject != "A" {
		t.Errorf("Create 后 onChange 应收到 1 项，got %+v", got)
	}
	s.Update(1, ptrStatus(StatusInProgress), nil, nil, nil, nil, nil)
	if len(got) != 1 || got[0].Status != StatusInProgress {
		t.Errorf("Update 后 onChange 应反映状态，got %+v", got)
	}
	s.Delete(1)
	if len(got) != 0 {
		t.Errorf("Delete 后 onChange 应收到空清单，got %+v", got)
	}
}
