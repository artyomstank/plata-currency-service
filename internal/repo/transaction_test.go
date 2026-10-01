package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
)

type beginnerStub struct {
	tx  pgx.Tx
	err error
}

func (s beginnerStub) Begin(context.Context) (pgx.Tx, error) { return s.tx, s.err }

type transactionStub struct {
	pgx.Tx
	committed, rolledBack      bool
	commitError, rollbackError error
	rollbackContextError       error
}

func (tx *transactionStub) Commit(context.Context) error {
	tx.committed = true
	return tx.commitError
}
func (tx *transactionStub) Rollback(ctx context.Context) error {
	tx.rolledBack = true
	tx.rollbackContextError = ctx.Err()
	if tx.committed && tx.commitError == nil {
		return pgx.ErrTxClosed
	}
	return tx.rollbackError
}

type requestContextKey struct{}

func TestTransactionManagerCommitsAndSharesTransaction(t *testing.T) {
	tx := &transactionStub{}
	manager := &TransactionManager{pool: beginnerStub{tx: tx}}
	ctx := context.WithValue(context.Background(), requestContextKey{}, "request-1")
	err := manager.WithinTransaction(ctx, func(txCtx context.Context) error {
		if txCtx.Value(requestContextKey{}) != "request-1" {
			t.Fatal("request context lost")
		}
		if got := queryExecutor(txCtx, nil); got != tx {
			t.Fatal("repository did not use transaction")
		}
		if got, err := requireTransaction(txCtx); err != nil || got != tx {
			t.Fatalf("transaction=%v err=%v", got, err)
		}
		return nil
	})
	if err != nil || !tx.committed {
		t.Fatalf("committed=%v err=%v", tx.committed, err)
	}
}

func TestTransactionManagerRollsBackOnErrorAndCancellation(t *testing.T) {
	tx := &transactionStub{}
	manager := &TransactionManager{pool: beginnerStub{tx: tx}}
	ctx, cancel := context.WithCancel(context.Background())
	failure := errors.New("quote save failed")
	err := manager.WithinTransaction(ctx, func(context.Context) error { cancel(); return failure })
	if !errors.Is(err, failure) || tx.committed || !tx.rolledBack || tx.rollbackContextError != nil {
		t.Fatalf("err=%v committed=%v rolledBack=%v rollbackContext=%v", err, tx.committed, tx.rolledBack, tx.rollbackContextError)
	}
}

func TestTransactionManagerPropagatesCommitAndRollbackErrors(t *testing.T) {
	commitError, rollbackError := errors.New("commit failed"), errors.New("rollback failed")
	tx := &transactionStub{commitError: commitError, rollbackError: rollbackError}
	manager := &TransactionManager{pool: beginnerStub{tx: tx}}
	err := manager.WithinTransaction(context.Background(), func(context.Context) error { return nil })
	if !errors.Is(err, commitError) || !errors.Is(err, rollbackError) || !tx.rolledBack {
		t.Fatalf("err=%v rollback=%v", err, tx.rolledBack)
	}
}

func TestTransactionManagerRollsBackAndPreservesPanic(t *testing.T) {
	tx := &transactionStub{}
	manager := &TransactionManager{pool: beginnerStub{tx: tx}}
	defer func() {
		if got := recover(); got != "handler panic" {
			t.Fatalf("panic=%v", got)
		}
		if !tx.rolledBack || tx.committed {
			t.Fatal("panic transaction was not rolled back")
		}
	}()
	_ = manager.WithinTransaction(context.Background(), func(context.Context) error { panic("handler panic") })
}

func TestTransactionManagerRejectsNestedTransactions(t *testing.T) {
	tx := &transactionStub{}
	manager := &TransactionManager{pool: beginnerStub{tx: tx}}
	err := manager.WithinTransaction(context.Background(), func(ctx context.Context) error {
		return manager.WithinTransaction(ctx, func(context.Context) error { t.Fatal("nested transaction callback executed"); return nil })
	})
	if err == nil || tx.committed || !tx.rolledBack {
		t.Fatalf("err=%v committed=%v rollback=%v", err, tx.committed, tx.rolledBack)
	}
}

func TestTransactionManagerDoesNotRunCallbackWhenBeginFails(t *testing.T) {
	failure := errors.New("begin failed")
	manager := &TransactionManager{pool: beginnerStub{err: failure}}
	err := manager.WithinTransaction(context.Background(), func(context.Context) error { t.Fatal("callback executed"); return nil })
	if !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
}

func TestRepositoriesRequireTransactionForWritesAndLocks(t *testing.T) {
	ctx := context.Background()
	jobs, quotes := NewJobsRepo(nil), NewQuotesRepo(nil)
	if _, _, err := jobs.Create(ctx, nil); err == nil {
		t.Fatal("create accepted without transaction")
	}
	if _, err := jobs.LockNextAvailable(ctx); err == nil {
		t.Fatal("claim accepted without transaction")
	}
	if _, err := jobs.GetByIDForUpdate(ctx, domain.JobID{}); err == nil {
		t.Fatal("lock accepted without transaction")
	}
	if err := jobs.Save(ctx, nil, usecase.JobUpdate{}); err == nil {
		t.Fatal("save accepted without transaction")
	}
	if err := quotes.Save(ctx, nil); err == nil {
		t.Fatal("quote accepted without transaction")
	}
}
