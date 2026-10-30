package storage

import (
	"context"
	"database/sql"
)

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
