package store

import (
	"path/filepath"
	"testing"
)

// 插入 5 条时间递增的消息（t0<t1<t2<t3<t4），调 CompactHistoryPrefix 后应保留
// [summary, t3, t4]，summary 排首位且 t0/t1/t2 已删。
func TestCompactHistoryPrefix_DeletesHeadAndInsertsSummary(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateSession(Session{ID: "sess_1", Title: "t", CreatedAt: now(), UpdatedAt: now()}); err != nil {
		t.Fatal(err)
	}

	times := []string{"2026-06-25T00:00:00Z", "2026-06-25T00:00:01Z",
		"2026-06-25T00:00:02Z", "2026-06-25T00:00:03Z", "2026-06-25T00:00:04Z"}
	for i, ts := range times {
		if err := db.AppendMessage(Message{
			ID: "m" + string(rune('0'+i)), SessionID: "sess_1", Role: "user",
			Content: "old" + string(rune('0'+i)), CreatedAt: ts,
		}); err != nil {
			t.Fatalf("append m%d: %v", i, err)
		}
	}

	// 摘要排在 beforeCreatedAt（=times[3]）前一刻，保证列于 tail 之前
	summary := Message{
		ID: "msum", SessionID: "sess_1", Role: "summary",
		Content: "压缩摘要", CreatedAt: "2026-06-25T00:00:02.999999999Z",
	}
	if err := db.CompactHistoryPrefix("sess_1", times[3], summary); err != nil {
		t.Fatalf("compact: %v", err)
	}

	got, err := db.ListMessages("sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3: %+v", len(got), got)
	}
	if got[0].ID != "msum" || got[0].Role != "summary" {
		t.Errorf("first=%+v, want summary", got[0])
	}
	if got[1].ID != "m3" || got[2].ID != "m4" {
		t.Errorf("tail order = %s,%s, want m3,m4", got[1].ID, got[2].ID)
	}
}

// summary.ID 与已存在消息 ID 重复导致 INSERT 主键冲突时，DELETE 也应回滚：
// 原 5 条仍在，事务不留下半成品状态。
func TestCompactHistoryPrefix_RollbackOnError(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "sessions_rb.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateSession(Session{ID: "sess_2", Title: "t", CreatedAt: now(), UpdatedAt: now()}); err != nil {
		t.Fatal(err)
	}

	times := []string{"2026-06-25T00:00:00Z", "2026-06-25T00:00:01Z",
		"2026-06-25T00:00:02Z", "2026-06-25T00:00:03Z", "2026-06-25T00:00:04Z"}
	for i, ts := range times {
		if err := db.AppendMessage(Message{
			ID: "m" + string(rune('0'+i)), SessionID: "sess_2", Role: "user",
			Content: "old" + string(rune('0'+i)), CreatedAt: ts,
		}); err != nil {
			t.Fatalf("append m%d: %v", i, err)
		}
	}

	// 故意复用 tail 中仍存在的 m3 作为摘要 ID：DELETE 只删 created_at < times[3]
	// 的行（t0/t1/t2），m3 保留，故 INSERT 触发主键冲突。
	dupSummary := Message{
		ID: "m3", SessionID: "sess_2", Role: "summary",
		Content: "dup", CreatedAt: "2026-06-25T00:00:02.999999999Z",
	}
	if err := db.CompactHistoryPrefix("sess_2", times[3], dupSummary); err == nil {
		t.Fatal("compact: expected primary-key conflict error, got nil")
	}

	// 事务回滚：5 条原消息均应保留
	got, err := db.ListMessages("sess_2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("after rollback len=%d, want 5 (DELETE should have rolled back too): %+v",
			len(got), got)
	}
}
