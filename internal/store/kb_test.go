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

func TestKBWorkbenchStoreOperations(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "workbench.db"))
	defer db.Close()

	if err := db.CreateProvider(Provider{ID: "prov_old", Name: "old", CreatedAt: now(), UpdatedAt: now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateProvider(Provider{ID: "prov_new", Name: "new", CreatedAt: now(), UpdatedAt: now()}); err != nil {
		t.Fatal(err)
	}

	kb := KnowledgeBase{ID: "kb_1", Name: "docs", Description: "old",
		EmbedProviderID: "prov_old", ChunkSize: 800, ChunkOverlap: 100,
		CreatedAt: now()}
	if err := db.CreateKB(kb); err != nil {
		t.Fatal(err)
	}

	kb.Name = "Product Docs"
	kb.Description = "updated"
	kb.EmbedProviderID = "prov_new"
	kb.ChunkSize = 1200
	kb.ChunkOverlap = 160
	if err := db.UpdateKB(kb); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetKB("kb_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Product Docs" || got.Description != "updated" ||
		got.EmbedProviderID != "prov_new" || got.ChunkSize != 1200 ||
		got.ChunkOverlap != 160 {
		t.Fatalf("updated kb = %+v", got)
	}

	doc := Document{ID: "doc_1", KBID: "kb_1", Filename: "a.md", FileSize: 9,
		MimeType: "text/markdown", Status: "ready", ChunkCount: 2,
		RawPath: "/tmp/a.md", CreatedAt: now()}
	if err := db.CreateDocument(doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range []Chunk{
		{ID: "chunk_1", DocID: "doc_1", KBID: "kb_1", Ordinal: 0, Text: "alpha"},
		{ID: "chunk_2", DocID: "doc_1", KBID: "kb_1", Ordinal: 1, Text: "beta"},
	} {
		if err := db.CreateChunk(c); err != nil {
			t.Fatal(err)
		}
	}

	docs, err := db.ListDocuments("kb_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].RawPath != "/tmp/a.md" {
		t.Fatalf("docs = %+v", docs)
	}
	kbs, err := db.ListKBs()
	if err != nil {
		t.Fatal(err)
	}
	if len(kbs) != 1 || kbs[0].DocCount != 1 {
		t.Fatalf("kb doc_count = %+v", kbs)
	}

	chunks, err := db.ListChunksByKB("kb_1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Text != "alpha" || chunks[1].Text != "beta" {
		t.Fatalf("chunks = %+v", chunks)
	}
}
