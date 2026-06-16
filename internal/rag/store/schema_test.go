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
	for _, table := range []string{"knowledge_bases", "documents", "chunks", "eval_questions", "eval_expected", "eval_runs"} {
		if !strings.Contains(s, table) {
			t.Errorf("schema missing table: %s", table)
		}
	}
}
