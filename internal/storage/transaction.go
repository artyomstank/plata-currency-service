package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type transactionKey struct{}

type transactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type TransactionManager struct {
	pool transactionBeginner
}

func NewTransactionManager(pool *pgxpool.Pool) *TransactionManager {
	return &TransactionManager{pool: pool}
}

func (m *TransactionManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) (err error) {
	if _, ok := transactionFromContext(ctx); ok {
		return errors.New("nested transactions are not supported")
	}
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
		}
	}()
	if err = fn(context.WithValue(ctx, transactionKey{}, tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func transactionFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(transactionKey{}).(pgx.Tx)
	return tx, ok
}

func queryExecutor(ctx context.Context, pool *pgxpool.Pool) executor {
	if tx, ok := transactionFromContext(ctx); ok {
		return tx
	}
	return pool
}

func requireTransaction(ctx context.Context) (pgx.Tx, error) {
	if tx, ok := transactionFromContext(ctx); ok {
		return tx, nil
	}
	return nil, errors.New("repository mutation or row lock requires a transaction")
}
