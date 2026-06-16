// internal/rag/store/schema_test.go
package store

import (
	"strings"
	"testing"
)

func TestSchemaUsesDynamicDimension(t *testing.T) {
	s768 := Schema(768)
	s1024 := Schema(1024)
	if !strings.Contains(s768, "float[768]") {
		t.Errorf("768 schema should contain float[768]")
	}
	if !strings.Contains(s1024, "float[1024]") {
		t.Errorf("1024 schema should contain float[1024]")
	}
}

func TestSchemaCreatesAllTables(t *testing.T) {
	s := Schema(768)
	// vec_chunks 是 sqlite-vec 虚拟表，与普通表 chunks 分开守护，
	// 避免 strings.Contains("chunks") 同时命中 vec_chunks 而漏报回归。
	for _, table := range []string{"knowledge_bases", "documents", "chunks", "eval_questions", "eval_expected", "eval_runs"} {
		if !strings.Contains(s, "CREATE TABLE IF NOT EXISTS "+table+" (") {
			t.Errorf("schema missing table: %s", table)
		}
	}
	if !strings.Contains(s, "CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks") {
		t.Errorf("schema missing virtual table: vec_chunks")
	}
}
