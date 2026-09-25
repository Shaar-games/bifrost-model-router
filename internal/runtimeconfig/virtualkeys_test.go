package runtimeconfig

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestReconcileVirtualKeyIDsRenamesStoredKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE governance_virtual_keys (id TEXT PRIMARY KEY, name TEXT UNIQUE);
		CREATE TABLE governance_virtual_key_provider_configs (id INTEGER PRIMARY KEY, virtual_key_id TEXT);
		INSERT INTO governance_virtual_keys (id, name) VALUES ('local-codex-vokeapi', 'Local Codex router');
		INSERT INTO governance_virtual_key_provider_configs (virtual_key_id) VALUES ('local-codex-vokeapi');
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"governance":{"virtual_keys":[{"id":"local-codex-nvidia","name":"Local Codex router"}]}}`)
	if err := ReconcileVirtualKeyIDs(dbPath, config); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, providerKey string
	if err := db.QueryRow(`SELECT id FROM governance_virtual_keys`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT virtual_key_id FROM governance_virtual_key_provider_configs`).Scan(&providerKey); err != nil {
		t.Fatal(err)
	}
	if id != "local-codex-nvidia" || providerKey != "local-codex-nvidia" {
		t.Fatalf("id=%q provider key=%q", id, providerKey)
	}
}

func TestReconcileVirtualKeyIDsIgnoresMissingDatabase(t *testing.T) {
	if err := ReconcileVirtualKeyIDs(filepath.Join(t.TempDir(), "missing.db"), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
}
