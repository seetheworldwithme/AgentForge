package store

import (
	"path/filepath"
	"testing"
	"time"
)

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func TestProviderCRUD(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()

	p := Provider{
		ID: "prov_1", Name: "p", BaseURL: "http://x", APIKey: "k",
		ChatModel: "m", IsDefault: true, CreatedAt: now(), UpdatedAt: now(),
	}
	if err := db.CreateProvider(p); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetProvider("prov_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "p" {
		t.Errorf("name=%q", got.Name)
	}
	all, _ := db.ListProviders()
	if len(all) != 1 {
		t.Errorf("len=%d", len(all))
	}
	if err := db.DeleteProvider("prov_1"); err != nil {
		t.Fatal(err)
	}
	_, err = db.GetProvider("prov_1")
	if err == nil {
		t.Errorf("expected not-found after delete")
	}
}

// TestDeleteProviderClearsReferences reproduces the FOREIGN KEY constraint
// failure: deleting a provider that sessions and knowledge_bases point at must
// succeed, nulling the links rather than failing.
func TestDeleteProviderClearsReferences(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()

	db.CreateProvider(Provider{
		ID: "prov_1", Name: "p", BaseURL: "u", APIKey: "k", ChatModel: "m",
		IsDefault: true, CreatedAt: now(), UpdatedAt: now(),
	})
	db.CreateSession(Session{ID: "sess_1", Title: "t", ProviderID: "prov_1",
		ToolsEnabled: 1, CreatedAt: now(), UpdatedAt: now()})
	db.CreateKB(KnowledgeBase{ID: "kb_1", Name: "k", EmbedProviderID: "prov_1", CreatedAt: now()})

	if err := db.DeleteProvider("prov_1"); err != nil {
		t.Fatalf("delete blocked by FK: %v", err)
	}
	if _, err := db.GetProvider("prov_1"); err == nil {
		t.Errorf("provider still present after delete")
	}

	// session keeps its row but loses the provider link
	sess, _ := db.GetSession("sess_1")
	if sess.ProviderID != "" {
		t.Errorf("session provider_id=%q, want empty", sess.ProviderID)
	}
	// kb keeps its row but loses the embed provider link
	kb, _ := db.GetKB("kb_1")
	if kb.EmbedProviderID != "" {
		t.Errorf("kb embed_provider_id=%q, want empty", kb.EmbedProviderID)
	}
}

func TestSessionAndMessages(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()

	// need a provider row first (FK)
	db.CreateProvider(Provider{
		ID: "prov_1", Name: "p", BaseURL: "u", APIKey: "k", ChatModel: "m",
		IsDefault: true, CreatedAt: now(), UpdatedAt: now(),
	})

	s := Session{ID: "sess_1", Title: "t", ProviderID: "prov_1",
		ToolsEnabled: 1, CreatedAt: now(), UpdatedAt: now()}
	if err := db.CreateSession(s); err != nil {
		t.Fatal(err)
	}

	msgs := []Message{
		{ID: "m1", SessionID: "sess_1", Role: "user", Content: "hi", CreatedAt: now()},
		{ID: "m2", SessionID: "sess_1", Role: "assistant", Content: "yo", CreatedAt: now()},
	}
	for _, m := range msgs {
		if err := db.AppendMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListMessages("sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "m1" {
		t.Errorf("got=%+v", got)
	}

	// delete session cascades messages
	if err := db.DeleteSession("sess_1"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListMessages("sess_1")
	if len(got) != 0 {
		t.Errorf("messages not cascaded, len=%d", len(got))
	}
}
