package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"columba.local/backend/internal/dbpath"
)

func TestUpgradeLatestPairPreservesEncryptedCredentials(t *testing.T) {
	path, logs, key := previousDatabases(t)
	config := filepath.Join(filepath.Dir(path), "config.json")
	if err := os.WriteFile(config, []byte(`{"databasePath":"business.sqlite3"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := UpgradeDatabases(path, logs, config); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path + ".upgrade.json")
	if err != nil {
		t.Fatal(err)
	}
	var journal upgradeJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	if !journal.Complete || len(journal.Files) != 3 {
		t.Fatalf("unexpected journal: %+v", journal)
	}
	for _, file := range journal.Files {
		if _, err := os.Stat(file.Backup); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenWithKey(path, key)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := store.ListAPITokens(t.Context())
	if err != nil || len(tokens) != 1 || tokens[0].Token != "test-secret" {
		t.Fatalf("credentials changed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := UpgradeDatabases(path, logs, config); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path + ".upgrade.json")
	if string(raw) != string(after) {
		t.Fatal("repeat startup created another migration")
	}
}

func TestUpgradeFailureRestoresBothDatabases(t *testing.T) {
	path, logs, _ := previousDatabases(t)
	failed := errors.New("injected log upgrade failure")
	err := upgradeDatabases(path, logs, "", func(ctx context.Context, db *sql.DB, version int, logs bool) error {
		if logs {
			return failed
		}
		if _, err := db.Exec("DELETE FROM api_tokens"); err != nil {
			return err
		}
		return migrateDatabase(ctx, db, version, logs)
	})
	if !errors.Is(err, failed) {
		t.Fatal(err)
	}
	for i, p := range []string{path, logs} {
		db, err := openUpgradeDatabase(p)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != 2-i {
			t.Fatalf("database %d not restored: %d", i, version)
		}
		if i == 0 {
			var count int
			if err := db.QueryRow("SELECT count(*) FROM api_tokens").Scan(&count); err != nil || count != 1 {
				t.Fatal("token lost", err)
			}
		}
		db.Close()
	}
}

func TestUpgradeRecoversInterruptedPair(t *testing.T) {
	path, logs, _ := previousDatabases(t)
	if err := UpgradeDatabases(path, logs, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path + ".upgrade.json")
	var journal upgradeJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	journal.Complete = false
	if err := writeUpgradeJournal(path+".upgrade.json", journal); err != nil {
		t.Fatal(err)
	}
	db, err := openUpgradeDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM api_tokens"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := UpgradeDatabases(path, logs, ""); err != nil {
		t.Fatal(err)
	}
	db, err = openUpgradeDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM api_tokens").Scan(&count); err != nil || count != 1 {
		t.Fatal("interrupted upgrade did not recover", err)
	}
}

func TestUpgradeUnknownLogLeavesBusinessUntouched(t *testing.T) {
	path, logs, _ := previousDatabases(t)
	db, err := openUpgradeDatabase(logs)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("PRAGMA user_version=999")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := UpgradeDatabases(path, logs, ""); err == nil {
		t.Fatal("unknown log schema accepted")
	}
	db, err = openUpgradeDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("business changed before pair validation", err)
	}
}

func TestUpgradeBackupIncludesCommittedWAL(t *testing.T) {
	path, logs, _ := previousDatabases(t)
	db, err := openUpgradeDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; INSERT INTO settings VALUES('wal-proof','42','now')"); err != nil {
		t.Fatal(err)
	}
	if err := UpgradeDatabases(path, logs, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path + ".upgrade.json")
	var journal upgradeJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	backup, err := openUpgradeDatabase(journal.Files[0].Backup)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var value string
	if err := backup.QueryRow("SELECT value FROM settings WHERE key='wal-proof'").Scan(&value); err != nil || value != "42" {
		t.Fatal("committed WAL missing from backup", err)
	}
}

func TestUpgradeRetiresAssistantWithoutPromotingItsKeys(t *testing.T) {
	path, logs, key := previousDatabases(t)
	db, err := openUpgradeDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agent_sessions(id TEXT); INSERT INTO agent_sessions VALUES('retired'); CREATE TABLE agent_messages(id TEXT); INSERT INTO api_tokens(name,token,scopes,created_at,updated_at) VALUES('assistant','old','["agent"]','','')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := UpgradeDatabases(path, logs, ""); err != nil {
		t.Fatal(err)
	}
	st, err := OpenWithKey(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, found, err := st.FindAPITokenByName(t.Context(), "assistant"); err != nil || found {
		t.Fatal("assistant key was promoted", err)
	}
	var count int
	if err := st.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name IN ('agent_sessions','agent_messages')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("retired tables remain", count, err)
	}
	var journal upgradeJournal
	raw, err := os.ReadFile(path + ".upgrade.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	backup, err := openUpgradeDatabase(journal.Files[0].Backup)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if err := backup.QueryRow(`SELECT count(*) FROM agent_sessions`).Scan(&count); err != nil || count != 1 {
		t.Fatal("backup lost retired data", count, err)
	}
}

func previousDatabases(t *testing.T) (string, string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "business.sqlite3")
	key := []byte("test master key")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/latest-business.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	codec, err := newSecretCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := codec.encrypt("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO api_tokens(name,token,token_hash,created_at,updated_at) VALUES('client',?,?,'','')`, secret, hashToken("test-secret")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	logs := dbpath.DefaultLogPath(path)
	ls, err := OpenLogStore(logs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.db.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err := ls.Close(); err != nil {
		t.Fatal(err)
	}
	return path, logs, key
}
