package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyLimitEnforcesConfiguredSize(t *testing.T) {
	for _, body := range []string{"1234", "12345"} {
		t.Run(body, func(t *testing.T) {
			handler := BodyLimit(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if len(body) == 4 {
					if err != nil || string(data) != body {
						t.Fatalf("body = %q, err = %v", data, err)
					}
					return
				}
				var limitError *http.MaxBytesError
				if !errors.As(err, &limitError) || limitError.Limit != 4 {
					t.Fatalf("err = %v", err)
				}
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		})
	}
}
