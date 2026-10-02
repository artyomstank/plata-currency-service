package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type idempotencyStoreStub struct {
	lock func(context.Context, string) (*StoredResponse, error)
	save func(context.Context, string, *StoredResponse) error
}

func (s idempotencyStoreStub) Lock(ctx context.Context, key string) (*StoredResponse, error) {
	return s.lock(ctx, key)
}

func (s idempotencyStoreStub) Save(ctx context.Context, key string, response *StoredResponse) error {
	return s.save(ctx, key, response)
}

type middlewareTransaction func(context.Context, func(context.Context) error) error

func (fn middlewareTransaction) WithinTransaction(ctx context.Context, callback func(context.Context) error) error {
	return fn(ctx, callback)
}

func TestIdempotencyReplaysResponseAndPreservesCurrentRequestID(t *testing.T) {
	var saved *StoredResponse
	var current *httptest.ResponseRecorder
	calls := 0
	store := idempotencyStoreStub{
		lock: func(ctx context.Context, key string) (*StoredResponse, error) {
			if key != "opaque key" || IdempotencyKeyFromContext(ctx) != key {
				t.Fatalf("key = %q, context key = %q", key, IdempotencyKeyFromContext(ctx))
			}
			return saved, nil
		},
		save: func(_ context.Context, _ string, response *StoredResponse) error {
			if current.Body.Len() != 0 || current.Flushed {
				t.Fatal("response sent before persistence")
			}
			saved = response
			return nil
		},
	}
	tx := middlewareTransaction(func(ctx context.Context, fn func(context.Context) error) error {
		if err := fn(ctx); err != nil {
			return err
		}
		if current.Body.Len() != 0 {
			t.Fatal("response sent before commit")
		}
		return nil
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if IdempotencyKeyFromContext(r.Context()) != "opaque key" || RequestIDFromContext(r.Context()) != "first" {
			t.Fatal("request context lost")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "first")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("{\"jobId\":\"original\",\"status\":\"JOB_STATUS_PENDING\"}\n"))
	})
	handler := RequestID(Idempotency(store, tx, func(http.ResponseWriter, *http.Request, error) { t.Fatal("unexpected error") })(next))
	var firstBody string
	for _, requestID := range []string{"first", "second"} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(requestID))
		request.Header.Set("Idempotency-Key", "opaque key")
		request.Header.Set("X-Request-ID", requestID)
		current = httptest.NewRecorder()
		handler.ServeHTTP(current, request)
		if current.Code != http.StatusAccepted {
			t.Errorf("status = %d, want 202", current.Code)
		}
		if current.Header().Get("X-Request-ID") != requestID {
			t.Errorf("request ID = %q, want %q", current.Header().Get("X-Request-ID"), requestID)
		}
		if current.Header().Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", current.Header().Get("Content-Type"))
		}
		if firstBody == "" {
			firstBody = current.Body.String()
		} else if current.Body.String() != firstBody {
			t.Error("replayed response differs from original")
		}
	}
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
}

func TestIdempotencyRejectsInvalidKeysBeforeStorage(t *testing.T) {
	for _, key := range []string{strings.Repeat("x", 129), strings.Repeat("я", 65), "invalid\x00key", string([]byte{0xff})} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.Header.Set("Idempotency-Key", key)
		called := false
		handler := Idempotency(nil, nil, func(_ http.ResponseWriter, _ *http.Request, err error) {
			called = true
			if !errors.Is(err, ErrInvalidIdempotencyKey) {
				t.Errorf("error = %v", err)
			}
		})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid key reached handler") }))
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if !called {
			t.Error("invalid key was accepted")
		}
	}
}

func TestIdempotencyAccepts128ByteKeys(t *testing.T) {
	for _, key := range []string{strings.Repeat("x", 128), strings.Repeat("я", 64)} {
		store := idempotencyStoreStub{lock: func(_ context.Context, got string) (*StoredResponse, error) {
			if got != key {
				t.Errorf("key = %q, want original key", got)
			}
			return &StoredResponse{StatusCode: 202, Header: make(http.Header)}, nil
		}}
		tx := middlewareTransaction(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
		handler := Idempotency(store, tx, func(http.ResponseWriter, *http.Request, error) { t.Fatal("valid key rejected") })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("cached key reached handler") }))
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.Header.Set("Idempotency-Key", key)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != 202 {
			t.Errorf("status = %d, want 202", recorder.Code)
		}
	}
}

func TestIdempotencyWithoutKeyBypassesStorage(t *testing.T) {
	handler := Idempotency(nil, nil, func(http.ResponseWriter, *http.Request, error) { t.Fatal("unexpected error") })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IdempotencyKeyFromContext(r.Context()) != "" {
			t.Fatal("unexpected context key")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", nil))
	if recorder.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", recorder.Code)
	}
}

func TestIdempotencyDoesNotStoreFailedResponses(t *testing.T) {
	var saved *StoredResponse
	calls := 0
	store := idempotencyStoreStub{
		lock: func(context.Context, string) (*StoredResponse, error) { return saved, nil },
		save: func(_ context.Context, _ string, response *StoredResponse) error { saved = response; return nil },
	}
	tx := middlewareTransaction(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	handler := Idempotency(store, tx, func(http.ResponseWriter, *http.Request, error) { t.Fatal("unexpected error") })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("invalid input"))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("created"))
	}))
	for _, want := range []int{400, 202, 202} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.Header.Set("Idempotency-Key", "same")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Errorf("status = %d, want %d", recorder.Code, want)
		}
	}
	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyDoesNotSendSuccessWhenStorageOrCommitFails(t *testing.T) {
	for _, stage := range []string{"lock", "save", "commit", "rollback"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("private persistence failure")
			store := idempotencyStoreStub{
				lock: func(context.Context, string) (*StoredResponse, error) {
					if stage == "lock" {
						return nil, failure
					}
					return nil, nil
				},
				save: func(context.Context, string, *StoredResponse) error {
					if stage == "save" {
						return failure
					}
					return nil
				},
			}
			tx := middlewareTransaction(func(ctx context.Context, fn func(context.Context) error) error {
				err := fn(ctx)
				if stage == "commit" {
					return failure
				}
				if stage == "rollback" {
					return errors.Join(err, failure)
				}
				return err
			})
			handler := Idempotency(store, tx, func(w http.ResponseWriter, _ *http.Request, err error) {
				if !errors.Is(err, failure) {
					t.Errorf("error = %v", err)
				}
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if stage == "lock" {
					t.Fatal("handler called when lock failed")
				}
				if stage == "rollback" {
					w.WriteHeader(http.StatusBadRequest)
				} else {
					w.WriteHeader(http.StatusAccepted)
				}
				_, _ = w.Write([]byte("unsaved response"))
			}))
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request.Header.Set("Idempotency-Key", "same")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != 500 || recorder.Body.String() != "internal error" {
				t.Errorf("uncommitted response leaked: %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
