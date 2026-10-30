package storage

import (
	"context"
	"database/sql"

	"github.com/bytedance/sonic"
)

const PanelPasswordSetting = "panel-password"

func (s *Store) ResetPanelPassword(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, PanelPasswordSetting)
	return err
}

// Compare-and-swap prevents concurrent setup or password changes from overwriting each other.
func (s *Store) SavePanelPassword(ctx context.Context, previous, passwordHash string) (bool, error) {
	payload, err := sonic.Marshal(passwordHash)
	if err != nil {
		return false, err
	}
	var result sql.Result
	if previous == "" {
		result, err = s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, PanelPasswordSetting, string(payload), nowString())
	} else {
		old, _ := sonic.Marshal(previous)
		result, err = s.db.ExecContext(ctx, `UPDATE settings SET value=?,updated_at=? WHERE key=? AND value=?`, string(payload), nowString(), PanelPasswordSetting, string(old))
	}
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
