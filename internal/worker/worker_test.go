package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type processorFunc func(context.Context) (bool, error)

func (f processorFunc) Execute(ctx context.Context) (bool, error) { return f(ctx) }

func TestProcessOneDelegatesToUseCase(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "claimed"}[claimed], func(t *testing.T) {
			called := false
			ctx := context.Background()
			w := newTestWorker(processorFunc(func(got context.Context) (bool, error) {
				called = true
				if got != ctx {
					t.Fatal("worker replaced context")
				}
				return claimed, nil
			}))
			if got := w.ProcessOne(ctx); got != claimed || !called {
				t.Fatalf("claimed=%v called=%v", got, called)
			}
		})
	}
}

func TestProcessOnePreservesClaimedOnError(t *testing.T) {
	w := newTestWorker(processorFunc(func(context.Context) (bool, error) { return true, errors.New("provider failed") }))
	if !w.ProcessOne(context.Background()) {
		t.Fatal("claimed job reported as unclaimed")
	}
}

func TestRunDoesNotProcessCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := newTestWorker(processorFunc(func(context.Context) (bool, error) { t.Fatal("use case called after cancellation"); return false, nil }))
	w.Run(ctx, make(chan struct{}))
}

func newTestWorker(processor JobProcessor) *Worker {
	return New(processor, Config{PollInterval: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunDoesNotPollAfterStop(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	w := newTestWorker(processorFunc(func(context.Context) (bool, error) { t.Fatal("polled after stop"); return false, nil }))
	w.Run(context.Background(), stop)
}
