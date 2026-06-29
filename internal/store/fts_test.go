package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func newFTSDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "fts.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.EnsureFTSTable("test_fts"); err != nil {
		t.Fatalf("EnsureFTSTable (fts5 not compiled? need -tags fts5): %v", err)
	}
	return db
}

// TestFTSTrigramChinese 验证 trigram 对中文子串的匹配（3 字、4 字 phrase、英文）。
func TestFTSTrigramChinese(t *testing.T) {
	db := newFTSDB(t)
	docs := map[string]string{
		"c1": "代理模型在消费场景下的召回率优化方案",
		"c2": "向量数据库的索引构建与查询性能",
		"c3": "hello world from agentforge",
	}
	for id, text := range docs {
		if err := db.InsertFTS("test_fts", id, text); err != nil {
			t.Fatalf("InsertFTS %s: %v", id, err)
		}
	}

	if hits, _ := db.SearchFTS("test_fts", `"召回率"`, 5); len(hits) != 1 || hits[0].ChunkID != "c1" {
		t.Errorf("3-char phrase match: got %+v, want only c1", hits)
	}
	if hits, _ := db.SearchFTS("test_fts", `"召回率优"`, 5); len(hits) != 1 || hits[0].ChunkID != "c1" {
		t.Errorf("4-char phrase match: got %+v, want c1", hits)
	}
	if hits, _ := db.SearchFTS("test_fts", `"agentforge"`, 5); len(hits) != 1 || hits[0].ChunkID != "c3" {
		t.Errorf("english match: got %+v, want c3", hits)
	}
}

// TestFTSShortQueryNoHit 验证 < 3 个 unicode 字符的 query 不命中（trigram 限制，
// 调用方应据此回退向量路）。
func TestFTSShortQueryNoHit(t *testing.T) {
	db := newFTSDB(t)
	if err := db.InsertFTS("test_fts", "c1", "代理模型召回率"); err != nil {
		t.Fatal(err)
	}
	hits, err := db.SearchFTS("test_fts", `"召回"`, 5) // 2 字 < 3
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("2-char query should not match (trigram), got %+v", hits)
	}
}

func TestFTSDelete(t *testing.T) {
	db := newFTSDB(t)
	db.InsertFTS("test_fts", "c1", "删除测试召回率内容")
	db.InsertFTS("test_fts", "c2", "保留召回率内容")

	if err := db.DeleteFTSByChunkIDs("test_fts", []string{"c1"}); err != nil {
		t.Fatalf("DeleteFTSByChunkIDs: %v", err)
	}
	hits, _ := db.SearchFTS("test_fts", `"召回率"`, 5)
	for _, h := range hits {
		if h.ChunkID == "c1" {
			t.Errorf("c1 should be deleted, still matched")
		}
	}
}

// TestFTSTableNotExists 验证查不存在的 FTS 表返回 error（retrieve 据此降级纯向量路）。
func TestFTSTableNotExists(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "fts2.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := db.SearchFTS("no_such_table", `"召回率"`, 5); err == nil {
		t.Errorf("SearchFTS on missing table should error")
	}
}

func TestFTSTableName(t *testing.T) {
	got := FTSTableName("KB-abc 123")
	if !strings.HasPrefix(got, "fts_") {
		t.Errorf("FTSTableName = %q, want fts_ prefix", got)
	}
	if strings.Contains(got, "vec_") {
		t.Errorf("FTSTableName = %q, should not contain vec_", got)
	}
}
