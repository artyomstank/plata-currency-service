package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	pg "currency-quotes/pkg/postgres"
)

type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func QueryExecutor(ctx context.Context, pool *pgxpool.Pool) executor {
	if tx, ok := pg.TransactionFromContext(ctx); ok {
		return tx
	}
	return pool
}
