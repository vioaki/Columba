package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"columba.local/backend/internal/dbpath"
	"github.com/bytedance/sonic"
)

const sqliteBusyTimeout = 5 * time.Second
const currentSchemaVersion = 6

func Open(path string) (*Store, error) {
	return OpenWithKey(path, nil)
}

// OpenWithKey 打开 SQLite store，并用给定 key 对落库的敏感字段
// （api token、上游 api_key）做透明 AES-256-GCM 加解密。
// key 为空时使用显式明文模式。
func OpenWithKey(path string, key []byte) (*Store, error) {
	return OpenWithLogPath(path, dbpath.DefaultLogPath(path), key)
}

func OpenWithLogPath(path, logPath string, key []byte) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	if err := dbpath.Validate(path, logPath); err != nil {
		return nil, err
	}
	if err := UpgradeDatabases(path, logPath, ""); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn, err := sqliteDatabaseDSN(path, url.Values{
		"_synchronous": {"NORMAL"},
		"_pragma":      {"cache_size(-65536)"},
	})
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	codec, err := newSecretCodec(key)
	if err != nil {
		db.Close()
		return nil, err
	}
	store := &Store{db: db, codec: codec, path: path}
	if err := store.checkSchema(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	store.LogStore, err = OpenLogStore(logPath)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open log database: %w", err)
	}
	return store, nil
}

// Pair upgrades finish before opening the current live schema.
func (s *Store) checkSchema(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version == currentSchemaVersion {
		return nil
	}
	if version == 0 {
		var tables int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil {
			return err
		}
		if tables == 0 {
			return nil
		}
	}
	return fmt.Errorf("unsupported database schema %d: only the latest Elysia schema and current Columba schema are supported", version)
}

func (s *Store) Close() error {
	s.closeOnce.Do(func() { s.closeErr = errors.Join(s.LogStore.Close(), s.db.Close()) })
	return s.closeErr
}

// Ping 验证底层 SQLite 连接是否可用，供 /health 依赖探测使用。
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}
	return s.db.PingContext(ctx)
}

func (s *Store) init(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA auto_vacuum=INCREMENTAL",
		"PRAGMA journal_mode=WAL",
	}
	for _, stmt := range pragmas {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	if err := s.createSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", currentSchemaVersion))
	return err
}

func sqlBoolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sqlIntToBool(v int) bool { return v != 0 }

func nowString() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(raw string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t
	}
	return time.Time{}
}

func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	payload, err := sonic.ConfigStd.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, string(payload), nowString())
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string, target any) (bool, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, sonic.ConfigStd.Unmarshal([]byte(payload), target)
}

// scanAPIToken 是 api_tokens 行的统一映射（列表与按名查询共用）：
// 解密失败时明文清空（行级容错，见 decryptOrClear）。
func (s *Store) scanAPIToken(row interface{ Scan(dest ...any) error }) (APIToken, error) {
	var item APIToken
	var enabled int
	var created, updated string
	if err := row.Scan(&item.Name, &item.Token, &enabled, &created, &updated); err != nil {
		return APIToken{}, err
	}
	item.Token = s.decryptOrClear("api token", item.Name, item.Token)
	item.Enabled = sqlIntToBool(enabled)
	item.CreatedAt = parseTime(created)
	item.UpdatedAt = parseTime(updated)
	return item, nil
}

func (s *Store) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, token, enabled, created_at, updated_at FROM api_tokens ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []APIToken{}
	for rows.Next() {
		item, err := s.scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertAPIToken(ctx context.Context, item APIToken) error {
	return s.writeAPIToken(ctx, s.db, item, true)
}

func (s *Store) writeAPIToken(ctx context.Context, executor interface {
	sqlExecutor
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, item APIToken, isUpsert bool) error {
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("token name is required")
	}
	// 去重检查：同一 token 值不允许配置到两个不同 name 上。用 SHA256 hash 走唯一索引快速判重。
	tokenHash := hashToken(item.Token)
	if tokenHash != "" {
		var existingName string
		err := executor.QueryRowContext(ctx, `SELECT name FROM api_tokens WHERE token_hash = ? AND name != ?`, tokenHash, item.Name).Scan(&existingName)
		if err == nil {
			return fmt.Errorf("token already used by API key %q, choose a different value", existingName)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	stored, err := s.codec.encrypt(item.Token)
	if err != nil {
		return err
	}
	now := nowString()
	query := `INSERT INTO api_tokens(name, token, token_hash, enabled, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`
	if isUpsert {
		query += ` ON CONFLICT(name) DO UPDATE SET token=excluded.token, token_hash=excluded.token_hash, enabled=excluded.enabled, updated_at=excluded.updated_at`
	}
	_, err = executor.ExecContext(ctx, query, item.Name, stored, tokenHash, sqlBoolToInt(item.Enabled), now, now)
	return err
}

func (s *Store) DeleteAPIToken(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE name = ?`, name)
	return err
}

// FindAPITokenByName 按名称查找单个 token（含解密后的明文），
// 供「留空即不变」编辑时保留原 token 使用。
func (s *Store) FindAPITokenByName(ctx context.Context, name string) (APIToken, bool, error) {
	item, err := s.scanAPIToken(s.db.QueryRowContext(ctx,
		`SELECT name, token, enabled, created_at, updated_at FROM api_tokens WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, false, nil
	}
	if err != nil {
		return APIToken{}, false, err
	}
	return item, true, nil
}
