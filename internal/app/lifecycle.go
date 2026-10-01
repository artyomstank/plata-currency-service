package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"currency-quotes/internal/worker"
)

func (a *application) run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		a.closeResources()
		return fmt.Errorf("listen HTTP: %w", err)
	}
	return a.serve(ctx, listener)
}

func (a *application) serve(ctx context.Context, listener net.Listener) error {
	workerCtx, cancelWorkers := context.WithCancel(context.WithoutCancel(ctx))
	httpCtx, cancelHTTP := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWorkers()
	defer cancelHTTP()
	stopPolling := make(chan struct{})
	a.server.BaseContext = func(net.Listener) context.Context { return httpCtx }
	var workers sync.WaitGroup
	for range a.workerCount {
		w := worker.New(a.processor, worker.Config{PollInterval: a.pollInterval}, a.log)
		workers.Go(func() { w.Run(workerCtx, stopPolling) })
	}
	workersDone := make(chan struct{})
	go func() { workers.Wait(); close(workersDone) }()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- a.server.Serve(listener) }()
	a.log.InfoContext(ctx, "HTTP service started", "addr", listener.Addr().String())
	var result error
	select {
	case <-ctx.Done():
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			result = fmt.Errorf("serve HTTP: %w", err)
		}
	}

	close(stopPolling)
	a.log.Info("shutting down application")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancelShutdown()
	httpDone := make(chan error, 1)
	go func() { httpDone <- a.server.Shutdown(shutdownCtx) }()
	deadline := shutdownCtx.Done()
	for httpDone != nil || workersDone != nil {
		select {
		case err := <-httpDone:
			httpDone = nil
			if err != nil {
				result = errors.Join(result, fmt.Errorf("shutdown HTTP: %w", err))
				cancelHTTP()
				if closeErr := a.server.Close(); closeErr != nil {
					result = errors.Join(result, fmt.Errorf("close HTTP: %w", closeErr))
				}
			}
		case <-workersDone:
			workersDone = nil
		case <-deadline:
			deadline = nil
			result = errors.Join(result, fmt.Errorf("shutdown deadline: %w", shutdownCtx.Err()))
			cancelWorkers()
			cancelHTTP()
			if err := a.server.Close(); err != nil {
				result = errors.Join(result, fmt.Errorf("close HTTP: %w", err))
			}
		}
	}
	a.closeResources()
	a.log.Info("application stopped")
	return result
}
