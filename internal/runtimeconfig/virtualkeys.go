package runtimeconfig

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

// ReconcileVirtualKeyIDs keeps an existing virtual key when a config edit changes
// its id but keeps its name. Bifrost syncs governance keys by id and refuses to
// create a second row with a name that is already stored.
func ReconcileVirtualKeyIDs(dbPath string, configJSON []byte) error {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat config database: %w", err)
	}
	var root struct {
		Governance struct {
			VirtualKeys []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"virtual_keys"`
		} `json:"governance"`
	}
	if err := json.Unmarshal(configJSON, &root); err != nil {
		return fmt.Errorf("decode config for virtual key reconciliation: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("open config database: %w", err)
	}
	defer db.Close()
	tables, err := virtualKeyReferenceTables(db)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin virtual key reconciliation: %w", err)
	}
	defer tx.Rollback()
	for _, key := range root.Governance.VirtualKeys {
		if key.ID == "" || key.Name == "" {
			continue
		}
		var storedID string
		err := tx.QueryRow(`SELECT id FROM governance_virtual_keys WHERE name = ?`, key.Name).Scan(&storedID)
		if errors.Is(err, sql.ErrNoRows) || storedID == key.ID {
			continue
		}
		if err != nil {
			return fmt.Errorf("look up virtual key %q: %w", key.Name, err)
		}
		var taken int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM governance_virtual_keys WHERE id = ?`, key.ID).Scan(&taken); err != nil {
			return fmt.Errorf("check virtual key id %q: %w", key.ID, err)
		}
		if taken > 0 {
			continue
		}
		for _, table := range tables {
			query := fmt.Sprintf(`UPDATE %s SET virtual_key_id = ? WHERE virtual_key_id = ?`, table)
			if _, err := tx.Exec(query, key.ID, storedID); err != nil {
				return fmt.Errorf("retarget %s from %q to %q: %w", table, storedID, key.ID, err)
			}
		}
		if _, err := tx.Exec(`UPDATE governance_virtual_keys SET id = ? WHERE id = ?`, key.ID, storedID); err != nil {
			return fmt.Errorf("rename virtual key %q to %q: %w", storedID, key.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit virtual key reconciliation: %w", err)
	}
	return nil
}

func virtualKeyReferenceTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name != 'governance_virtual_keys'`)
	if err != nil {
		return nil, fmt.Errorf("list config tables: %w", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'virtual_key_id'`, name).Scan(&count); err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		if count > 0 {
			tables = append(tables, name)
		}
	}
	return tables, rows.Err()
}
