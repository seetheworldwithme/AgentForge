package store

import (
	"path/filepath"
	"testing"
)

// TestSettingsGetSet covers the kv settings table backing the title-provider
// (and future global) config: absent keys return "" with no error, and writes
// upsert.
func TestSettingsGetSet(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if v, err := db.GetSetting("missing"); err != nil || v != "" {
		t.Fatalf("missing key: got %q err %v, want empty/no-error", v, err)
	}
	if err := db.SetSetting("title_provider_id", "prov_1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, err := db.GetSetting("title_provider_id"); err != nil || v != "prov_1" {
		t.Fatalf("after set: got %q err %v, want prov_1", v, err)
	}
	// second write upserts (does not duplicate or error).
	if err := db.SetSetting("title_provider_id", "prov_2"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if v, _ := db.GetSetting("title_provider_id"); v != "prov_2" {
		t.Fatalf("after upsert: got %q want prov_2", v)
	}
}
