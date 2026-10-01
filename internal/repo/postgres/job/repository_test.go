package job

import (
	"context"
	"testing"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/usecase"
)

func TestRepositoryRequiresTransactionForWritesAndLocks(t *testing.T) {
	ctx := context.Background()
	jobs := New(nil)
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
}
