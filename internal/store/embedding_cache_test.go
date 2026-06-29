package store

import (
	"path/filepath"
	"testing"
)

func TestEmbeddingCache(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "ec.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.PutCachedEmbedding("m1", "h1", []float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	// 命中 h1，未命中 h2
	got, err := db.GetCachedEmbeddings("m1", []string{"h1", "h2"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got["h1"]; !ok || len(v) != 3 || v[0] != 1 {
		t.Errorf("h1 not cached correctly: %+v", got)
	}
	if _, ok := got["h2"]; ok {
		t.Errorf("h2 should not be cached")
	}
	// 不同 model 不命中（维度/模型隔离）
	got2, _ := db.GetCachedEmbeddings("m2", []string{"h1"})
	if len(got2) != 0 {
		t.Errorf("different model should miss: %+v", got2)
	}
	// 幂等：重复写不报错，且 INSERT OR IGNORE 不覆盖原值
	if err := db.PutCachedEmbedding("m1", "h1", []float32{9, 9, 9}); err != nil {
		t.Fatal(err)
	}
	got3, _ := db.GetCachedEmbeddings("m1", []string{"h1"})
	if got3["h1"][0] != 1 {
		t.Errorf("INSERT OR IGNORE should not overwrite: %+v", got3)
	}
}
