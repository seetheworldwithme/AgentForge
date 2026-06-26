// Package todo 提供进程级、按会话隔离的待办任务存储与工具。
//
// 设计对标 Claude Code 的 TodoWrite：让 agent 在复杂多步任务中显式跟踪进度
// （pending → in_progress → completed），并支持任务间依赖（blocks/blockedBy）。
// 存储是内存的、任务级的——同一会话的多次请求共享同一份清单（通过 SetCurrent
// 切换当前会话），重启后清空。状态变化通过 onChange 回调通知调用方（handler 注入
// SSE 推送，让前端实时看到进度）。
package todo

import (
	"fmt"
	"sync"
)

// Status 是任务状态。
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Task 是一条待办任务。
type Task struct {
	ID          int    `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	ActiveForm  string `json:"active_form,omitempty"`
	Status      Status `json:"status"`
	Blocks      []int  `json:"blocks,omitempty"`     // 本任务阻塞哪些任务 id
	BlockedBy   []int  `json:"blocked_by,omitempty"` // 本任务被哪些任务 id 阻塞
}

// Store 是进程级、按会话隔离的内存待办存储。零值不可用，请用 New。
type Store struct {
	mu       sync.RWMutex
	current  string             // 当前会话 id，Create/Update/List/Delete 都作用于它
	items    map[string][]Task  // sessionID -> 该会话的任务（按 id 升序）
	seq      map[string]int     // sessionID -> 自增 id 计数（删除后不复用，避免 id 冲突）
	onChange func([]Task)       // 状态变化回调（handler 注入 sse emit）；nil 不通知
}

// New 创建一个空的待办存储。
func New() *Store {
	return &Store{items: map[string][]Task{}, seq: map[string]int{}}
}

// SetCurrent 切换当前会话；后续所有写/读操作都作用于它。
func (s *Store) SetCurrent(sessionID string) {
	s.mu.Lock()
	s.current = sessionID
	s.mu.Unlock()
}

// SetOnChange 设置状态变化回调：Create/Update/Delete 后会用当前会话的清单副本调用它。
// handler 在 Chat 开头注入 sse 推送闭包，defer 清理为 nil（防 stale emitter 泄漏到下一会话）。
func (s *Store) SetOnChange(cb func([]Task)) {
	s.mu.Lock()
	s.onChange = cb
	s.mu.Unlock()
}

// notifyLocked 在持锁状态下推送当前会话清单副本给 onChange。todo 操作低频，
// 持锁调 sse emit 可接受（与 Gate 持锁 emit confirm_req 同款）。
func (s *Store) notifyLocked() {
	if s.onChange == nil {
		return
	}
	s.onChange(snapshot(s.items[s.current]))
}

// Create 新建一条 pending 任务，返回带分配 id 的任务。blocks/blockedBy 声明依赖。
func (s *Store) Create(subject, description, activeForm string, blocks, blockedBy []int) Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq[s.current]++
	t := Task{
		ID:          s.seq[s.current],
		Subject:     subject,
		Description: description,
		ActiveForm:  activeForm,
		Status:      StatusPending,
		Blocks:      append([]int(nil), blocks...),
		BlockedBy:   append([]int(nil), blockedBy...),
	}
	s.items[s.current] = append(s.items[s.current], t)
	s.notifyLocked()
	return t
}

// Update 局部更新一条任务：传 nil 的字段表示不改，addBlocks/addBlockedBy 追加依赖。
//
// 约束：
//   - 设为 in_progress 时，若 BlockedBy 含未完成任务，返回 error（被依赖阻塞）；
//   - 设为 in_progress 时，自动把同会话其它 in_progress 任务置回 pending（一次只一个进行中）。
func (s *Store) Update(id int, status *Status, subject, description, activeForm *string, addBlocks, addBlockedBy []int) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.items[s.current]
	idx := -1
	for i, t := range list {
		if t.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Task{}, fmt.Errorf("任务 %d 不存在", id)
	}
	t := list[idx]
	if status != nil {
		if *status == StatusInProgress {
			for _, depID := range t.BlockedBy {
				if !isCompleted(list, depID) {
					return Task{}, fmt.Errorf("任务 #%d 被依赖任务 #%d 阻塞，后者尚未完成", id, depID)
				}
			}
			for j := range list {
				if list[j].Status == StatusInProgress && list[j].ID != id {
					list[j].Status = StatusPending
				}
			}
		}
		t.Status = *status
	}
	if subject != nil {
		t.Subject = *subject
	}
	if description != nil {
		t.Description = *description
	}
	if activeForm != nil {
		t.ActiveForm = *activeForm
	}
	if addBlocks != nil {
		t.Blocks = mergeUnique(t.Blocks, addBlocks)
	}
	if addBlockedBy != nil {
		t.BlockedBy = mergeUnique(t.BlockedBy, addBlockedBy)
	}
	list[idx] = t
	s.notifyLocked()
	return t, nil
}

// Delete 删除一条任务。
func (s *Store) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.items[s.current]
	for i, t := range list {
		if t.ID == id {
			s.items[s.current] = append(list[:i], list[i+1:]...)
			s.notifyLocked()
			return nil
		}
	}
	return fmt.Errorf("任务 %d 不存在", id)
}

// List 返回当前会话的全部任务副本（按 id 升序）。
func (s *Store) List() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return snapshot(s.items[s.current])
}

// ListFor 返回指定会话的清单副本（GET 端点用，不依赖 current）。
func (s *Store) ListFor(sid string) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return snapshot(s.items[sid])
}

func snapshot(src []Task) []Task {
	out := make([]Task, len(src))
	copy(out, src)
	return out
}

func isCompleted(list []Task, id int) bool {
	for _, t := range list {
		if t.ID == id {
			return t.Status == StatusCompleted
		}
	}
	return false
}

// mergeUnique 合并两个 id 切片去重。
func mergeUnique(a, b []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(a)+len(b))
	for _, x := range a {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	for _, x := range b {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// validStatus 判断是否为合法状态值。
func validStatus(s Status) bool {
	return s == StatusPending || s == StatusInProgress || s == StatusCompleted
}
