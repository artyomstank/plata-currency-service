package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"currency-quotes/internal/worker"
)

type processorFunc func(context.Context) (bool, error)

func (f processorFunc) Execute(ctx context.Context) (bool, error) { return f(ctx) }

type testListener struct {
	net.Listener
	closed chan struct{}
	once   sync.Once
}

func (l *testListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.closed) })
	return err
}

type lifecycleLogHandler struct {
	slog.Handler
	stopping chan struct{}
	once     sync.Once
}

func (h *lifecycleLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "shutting down application" {
		h.once.Do(func() { close(h.stopping) })
	}
	return h.Handler.Handle(ctx, record)
}

func startTestApplication(t *testing.T, processor worker.JobProcessor, handler http.Handler, count int, timeout time.Duration, cleanup func()) (context.CancelFunc, <-chan error, *testListener, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	watched := &testListener{Listener: listener, closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	stopping := make(chan struct{})
	logger := slog.New(&lifecycleLogHandler{Handler: slog.NewTextHandler(io.Discard, nil), stopping: stopping})
	a := &application{
		server: &http.Server{Handler: handler}, processor: processor,
		log: logger, workerCount: count,
		pollInterval: time.Millisecond, shutdownTimeout: timeout, closeResources: cleanup,
	}
	done := make(chan error, 1)
	go func() { done <- a.serve(ctx, watched) }()
	t.Cleanup(func() { cancel(); _ = a.server.Close() })
	return cancel, done, watched, stopping
}

func await(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for lifecycle event")
	}
}
func applicationResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("application did not stop")
		return nil
	}
}

func TestShutdownDrainsAllWorkersBeforeClosingResources(t *testing.T) {
	entered := make(chan struct{}, 3)
	finish := make(chan struct{})
	var calls, completed, cancelled atomic.Int32
	resourcesClosed := make(chan struct{})
	processor := processorFunc(func(ctx context.Context) (bool, error) {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-finish:
			completed.Add(1)
			return true, nil
		case <-ctx.Done():
			cancelled.Add(1)
			return true, ctx.Err()
		}
	})
	cancel, done, listener, _ := startTestApplication(t, processor, http.NotFoundHandler(), 3, time.Second, func() {
		if completed.Load() != 3 {
			t.Errorf("resources closed before all jobs finished: %d", completed.Load())
		}
		close(resourcesClosed)
	})
	for range 3 {
		await(t, entered)
	}
	cancel()
	await(t, listener.closed)
	select {
	case <-resourcesClosed:
		t.Fatal("resources closed with active workers")
	default:
	}
	if cancelled.Load() != 0 {
		t.Fatal("stop signal cancelled active jobs before deadline")
	}
	close(finish)
	if err := applicationResult(t, done); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || completed.Load() != 3 || cancelled.Load() != 0 {
		t.Fatalf("calls=%d completed=%d cancelled=%d", calls.Load(), completed.Load(), cancelled.Load())
	}
	await(t, resourcesClosed)
}

func TestShutdownDeadlineCancelsWorkerAndJoinsIt(t *testing.T) {
	entered := make(chan struct{})
	var exited atomic.Bool
	var calls atomic.Int32
	resourcesClosed := make(chan struct{})
	processor := processorFunc(func(ctx context.Context) (bool, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		exited.Store(true)
		return true, ctx.Err()
	})
	cancel, done, _, _ := startTestApplication(t, processor, http.NotFoundHandler(), 1, 30*time.Millisecond, func() {
		if !exited.Load() {
			t.Error("resources closed before cancelled worker exited")
		}
		close(resourcesClosed)
	})
	await(t, entered)
	cancel()
	if err := applicationResult(t, done); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error=%v", err)
	}
	if calls.Load() != 1 || !exited.Load() {
		t.Fatal("worker was not stopped")
	}
	await(t, resourcesClosed)
}

func TestHTTPFailureStopsPollingWithoutCancellingActiveJob(t *testing.T) {
	entered := make(chan struct{})
	finish := make(chan struct{})
	var calls atomic.Int32
	processor := processorFunc(func(ctx context.Context) (bool, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-finish:
			return true, nil
		case <-ctx.Done():
			return true, ctx.Err()
		}
	})
	_, done, listener, stopping := startTestApplication(t, processor, http.NotFoundHandler(), 1, time.Second, func() {})
	await(t, entered)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	await(t, stopping)
	close(finish)
	if err := applicationResult(t, done); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("serve error=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("worker kept polling after HTTP failure: %d", calls.Load())
	}
}

func TestShutdownAllowsActiveHTTPRequestToFinish(t *testing.T) {
	entered := make(chan struct{})
	finish := make(chan struct{})
	resourcesClosed := make(chan struct{})
	var cancelled atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-finish:
			_, _ = w.Write([]byte("done"))
		case <-r.Context().Done():
			cancelled.Store(true)
		}
	})
	cancel, done, listener, _ := startTestApplication(t, nil, handler, 0, time.Second, func() { close(resourcesClosed) })
	responseDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			defer response.Body.Close()
			var body []byte
			body, err = io.ReadAll(response.Body)
			if err == nil && string(body) != "done" {
				err = errors.New("active response truncated")
			}
		}
		responseDone <- err
	}()
	await(t, entered)
	cancel()
	await(t, listener.closed)
	select {
	case <-resourcesClosed:
		t.Fatal("closed resources before HTTP drain")
	default:
	}
	close(finish)
	if err := applicationResult(t, responseDone); err != nil {
		t.Fatal(err)
	}
	if err := applicationResult(t, done); err != nil {
		t.Fatal(err)
	}
	if cancelled.Load() {
		t.Fatal("active HTTP request cancelled during graceful drain")
	}
	await(t, resourcesClosed)
}

func TestShutdownDeadlineCancelsActiveHTTPRequest(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(cancelled) })
	cancel, done, listener, _ := startTestApplication(t, nil, handler, 0, 30*time.Millisecond, func() {})
	requestDone := make(chan struct{})
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = response.Body.Close()
		}
		close(requestDone)
	}()
	await(t, entered)
	cancel()
	if err := applicationResult(t, done); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error=%v", err)
	}
	await(t, cancelled)
	await(t, requestDone)
}
