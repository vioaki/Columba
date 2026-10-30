package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"columba.local/backend/internal/atomicfile"
	"columba.local/backend/internal/dbpath"
)

type upgradeFile struct {
	Path   string `json:"path"`
	Backup string `json:"backup"`
}

type upgradeJournal struct {
	Complete bool          `json:"complete"`
	Files    []upgradeFile `json:"files"`
}

// UpgradeDatabases runs before any request producers or store writers start.
// Only the last Elysia schema (business 2, logs 1) and Columba schemas are accepted.
func UpgradeDatabases(path, logPath, configPath string) error {
	return upgradeDatabases(path, logPath, configPath, migrateDatabase)
}

func upgradeDatabases(path, logPath, configPath string, migrate func(context.Context, *sql.DB, int, bool) error) error {
	if err := dbpath.Validate(path, logPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	unlock, err := lockUpgrade(path + ".upgrade.lock")
	if err != nil {
		return err
	}
	defer unlock()
	journalPath := path + ".upgrade.json"
	if raw, err := os.ReadFile(journalPath); err == nil {
		var journal upgradeJournal
		if err := json.Unmarshal(raw, &journal); err != nil {
			return fmt.Errorf("read upgrade journal: %w", err)
		}
		if !journal.Complete {
			if err := restoreUpgrade(journal); err != nil {
				return fmt.Errorf("recover interrupted upgrade: %w", err)
			}
			if err := os.Remove(journalPath); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	ctx := context.Background()
	paths := []string{path, logPath}
	versions := make([]int, 2)
	needsUpgrade := false
	for i, p := range paths {
		version, err := inspectDatabase(ctx, p, i == 1)
		if err != nil {
			return err
		}
		versions[i] = version
		target := currentSchemaVersion
		if i == 1 {
			target = logSchemaVersion
		}
		needsUpgrade = needsUpgrade || version > 0 && version < target
	}
	if !needsUpgrade {
		return nil
	}
	backupDir, err := os.MkdirTemp(filepath.Dir(path), "columba-upgrade-")
	if err != nil {
		return err
	}
	journal := upgradeJournal{}
	for i, p := range paths {
		if versions[i] == 0 {
			continue
		}
		backup := filepath.Join(backupDir, fmt.Sprintf("database-%d.sqlite3", i))
		db, err := openUpgradeDatabase(p)
		if err != nil {
			return err
		}
		// VACUUM INTO takes a consistent snapshot including committed WAL pages.
		_, err = db.ExecContext(ctx, "VACUUM INTO ?", backup)
		closeErr := db.Close()
		if err := errors.Join(err, closeErr); err != nil {
			return fmt.Errorf("backup %s: %w", p, err)
		}
		if err := os.Chmod(backup, 0600); err != nil {
			return err
		}
		if err := syncFile(backup); err != nil {
			return err
		}
		journal.Files = append(journal.Files, upgradeFile{p, backup})
	}
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			backup := filepath.Join(backupDir, "config.json")
			if err := copyUpgradeFile(configPath, backup); err != nil {
				return err
			}
			journal.Files = append(journal.Files, upgradeFile{configPath, backup})
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := writeUpgradeJournal(journalPath, journal); err != nil {
		return err
	}
	for i, p := range paths {
		if versions[i] == 0 {
			continue
		}
		db, err := openUpgradeDatabase(p)
		if err == nil {
			err = migrate(ctx, db, versions[i], i == 1)
			if err == nil {
				var result string
				err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
				if err == nil && result != "ok" {
					err = fmt.Errorf("integrity check: %s", result)
				}
			}
			err = errors.Join(err, db.Close())
		}
		if err != nil {
			if restoreErr := restoreUpgrade(journal); restoreErr != nil {
				return errors.Join(err, fmt.Errorf("restore upgrade: %w", restoreErr))
			}
			return errors.Join(err, os.Remove(journalPath))
		}
	}
	journal.Complete = true
	return writeUpgradeJournal(journalPath, journal)
}

func openUpgradeDatabase(path string) (*sql.DB, error) {
	dsn, err := sqliteDatabaseDSN(path, url.Values{"mode": {"rw"}, "_synchronous": {"FULL"}})
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func inspectDatabase(ctx context.Context, path string, logs bool) (int, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	db, err := openUpgradeDatabase(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var version, tables int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return 0, err
	}
	if version == 0 && tables == 0 {
		return 0, nil
	}
	minimum, current, required := 2, currentSchemaVersion, "settings"
	if logs {
		minimum, current, required = 1, logSchemaVersion, "usage_records"
	}
	if version < minimum || version > current {
		return 0, fmt.Errorf("unsupported database schema %d at %s (supported %d..%d)", version, path, minimum, current)
	}
	var found int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?", required).Scan(&found); err != nil {
		return 0, err
	}
	if found != 1 {
		return 0, fmt.Errorf("database %s is missing %s", path, required)
	}
	return version, nil
}

func migrateDatabase(ctx context.Context, db *sql.DB, version int, logs bool) error {
	target := currentSchemaVersion
	if logs {
		target = logSchemaVersion
	}
	if version == target {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Version 3 / 2 establish the Columba migration baseline without data changes.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", target)); err != nil {
		return err
	}
	return tx.Commit()
}

func writeUpgradeJournal(path string, journal upgradeJournal) error {
	raw, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, raw, 0600)
}

func restoreUpgrade(journal upgradeJournal) error {
	for _, file := range journal.Files {
		if _, err := os.Stat(file.Backup); err != nil {
			return err
		}
	}
	for _, file := range journal.Files {
		if err := copyUpgradeFile(file.Backup, file.Path); err != nil {
			return err
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.Remove(file.Path + suffix); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func copyUpgradeFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(destination), ".upgrade-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, copyErr := io.Copy(out, in)
	if err := errors.Join(copyErr, out.Sync(), out.Close()); err != nil {
		return err
	}
	if err := atomicfile.ReplaceFile(out.Name(), destination); err != nil {
		return err
	}
	return nil
}
