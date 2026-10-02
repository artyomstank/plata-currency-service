package idempotency

import (
	"context"
	"testing"
)

func TestRepositoryRequiresTransaction(t *testing.T) {
	repository := New()
	if _, err := repository.Lock(context.Background(), "key"); err == nil {
		t.Fatal("key reserved without a transaction")
	}
	if err := repository.Save(context.Background(), "key", nil); err == nil {
		t.Fatal("response saved without a transaction")
	}
}
