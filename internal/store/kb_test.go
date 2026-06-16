package store

import (
	"path/filepath"
	"testing"
)

func TestKBDocChunkCRUD(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()

	kb := KnowledgeBase{ID: "kb_1", Name: "docs", ChunkSize: 800, ChunkOverlap: 100,
		CreatedAt: now()}
	if err := db.CreateKB(kb); err != nil {
		t.Fatal(err)
	}

	doc := Document{ID: "doc_1", KBID: "kb_1", Filename: "a.txt", FileSize: 5,
		MimeType: "text/plain", Status: "processing", CreatedAt: now()}
	if err := db.CreateDocument(doc); err != nil {
		t.Fatal(err)
	}

	chunks := []Chunk{
		{ID: "c1", DocID: "doc_1", KBID: "kb_1", Ordinal: 0, Text: "hello"},
		{ID: "c2", DocID: "doc_1", KBID: "kb_1", Ordinal: 1, Text: "world"},
	}
	for _, c := range chunks {
		if err := db.CreateChunk(c); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.SetDocumentStatus("doc_1", "ready", 2, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetDocument("doc_1")
	if got.Status != "ready" || got.ChunkCount != 2 {
		t.Errorf("got=%+v", got)
	}

	list, _ := db.ListDocuments("kb_1")
	if len(list) != 1 {
		t.Errorf("len=%d", len(list))
	}

	// GetChunksByIDs round-trips chunk text
	byIDs, _ := db.GetChunksByIDs([]string{"c1", "c2"})
	if len(byIDs) != 2 {
		t.Errorf("GetChunksByIDs len=%d", len(byIDs))
	}

	// delete document cascades chunks
	if err := db.DeleteDocument("doc_1"); err != nil {
		t.Fatal(err)
	}
	cs, _ := db.ListChunksByDoc("doc_1")
	if len(cs) != 0 {
		t.Errorf("chunks not cascaded, len=%d", len(cs))
	}
}
