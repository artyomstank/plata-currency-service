package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

var ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")

var errResponseNotStored = errors.New("unsuccessful response is not stored")

type idempotencyKey struct{}

type StoredResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type IdempotencyStore interface {
	Lock(context.Context, string) (*StoredResponse, error)
	Save(context.Context, string, *StoredResponse) error
}

type TransactionManager interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

func IdempotencyKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKey{}).(string)
	return key
}

func Idempotency(store IdempotencyStore, tx TransactionManager, handleError func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if len(key) > 128 || !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
				handleError(w, r, fmt.Errorf("%w: must be valid text of at most 128 bytes", ErrInvalidIdempotencyKey))
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), idempotencyKey{}, key))
			var response *StoredResponse
			err := tx.WithinTransaction(r.Context(), func(ctx context.Context) error {
				var err error
				response, err = store.Lock(ctx, key)
				if err != nil || response != nil {
					return err
				}
				buffer := &bufferedResponse{header: make(http.Header)}
				next.ServeHTTP(chimiddleware.NewWrapResponseWriter(buffer, r.ProtoMajor), r.WithContext(ctx))
				if err := ctx.Err(); err != nil {
					return err
				}
				body := make([]byte, buffer.body.Len())
				copy(body, buffer.body.Bytes())
				response = &StoredResponse{StatusCode: buffer.status, Header: buffer.header.Clone(), Body: body}
				if response.StatusCode == 0 {
					response.StatusCode = http.StatusOK
				}
				response.Header.Del("X-Request-ID")
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					return errResponseNotStored
				}
				return store.Save(ctx, key, response)
			})
			if err != nil && err != errResponseNotStored {
				handleError(w, r, err)
				return
			}
			for name, values := range response.Header {
				if strings.EqualFold(name, "X-Request-ID") {
					continue
				}
				w.Header()[name] = append([]string(nil), values...)
			}
			w.WriteHeader(response.StatusCode)
			if _, err := w.Write(response.Body); err != nil {
				handleError(w, r, err)
			}
		})
	}
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}
