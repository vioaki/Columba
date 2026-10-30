package storage

import (
	"context"
	"database/sql"
	"errors"
)

const nativeContinuationSchema = `CREATE TABLE IF NOT EXISTS native_continuations (protocol TEXT NOT NULL, client_hash TEXT NOT NULL, id TEXT NOT NULL, model_id TEXT NOT NULL, source_id TEXT NOT NULL, credential_id TEXT NOT NULL, base_url TEXT NOT NULL, PRIMARY KEY(protocol,client_hash,id))`

type NativeContinuation struct {
	Protocol, ClientHash, ID, ModelID, SourceID, CredentialID, BaseURL string
}

func (s *Store) SaveNativeContinuations(ctx context.Context, owner NativeContinuation, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if id == "" {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO native_continuations(protocol,client_hash,id,model_id,source_id,credential_id,base_url) VALUES(?,?,?,?,?,?,?) ON CONFLICT(protocol,client_hash,id) DO UPDATE SET source_id=CASE WHEN model_id=excluded.model_id AND source_id=excluded.source_id AND credential_id=excluded.credential_id AND base_url=excluded.base_url THEN source_id ELSE '' END`, owner.Protocol, owner.ClientHash, id, owner.ModelID, owner.SourceID, owner.CredentialID, owner.BaseURL)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FindNativeContinuation(ctx context.Context, protocol, clientHash, id string) (NativeContinuation, bool, error) {
	item := NativeContinuation{Protocol: protocol, ClientHash: clientHash, ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT model_id,source_id,credential_id,base_url FROM native_continuations WHERE protocol=? AND client_hash=? AND id=?`, protocol, clientHash, id).Scan(&item.ModelID, &item.SourceID, &item.CredentialID, &item.BaseURL)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	return item, err == nil, err
}
