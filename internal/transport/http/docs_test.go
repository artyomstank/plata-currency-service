package http

import (
	"bytes"
	"mime"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"currency-quotes/docs"
)

func TestDocumentationServesEmbeddedFiles(t *testing.T) {
	handler := newTestHandler(jobsStub{}, quotesStub{})
	for _, tc := range []struct {
		path, file   string
		contentTypes []string
	}{
		{"/docs/", "swagger-ui/index.html", []string{"text/html"}},
		{"/docs/swagger-ui.css", "swagger-ui/swagger-ui.css", []string{"text/css"}},
		{"/docs/swagger-ui-bundle.js", "swagger-ui/swagger-ui-bundle.js", []string{"text/javascript", "application/javascript"}},
		{"/docs/swagger-initializer.js", "swagger-ui/swagger-initializer.js", []string{"text/javascript", "application/javascript"}},
		{"/openapi.yaml", "openapi.yaml", []string{"application/yaml"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			data, err := docs.Files.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				request := httptest.NewRequest(method, tc.path, nil)
				request.Header.Set("X-Request-ID", "docs-request")
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusOK {
					t.Fatalf("%s status = %d, want 200", method, recorder.Code)
				}
				contentType, _, err := mime.ParseMediaType(recorder.Header().Get("Content-Type"))
				if err != nil {
					t.Fatalf("%s invalid Content-Type: %v", method, err)
				}
				if !slices.Contains(tc.contentTypes, contentType) {
					t.Errorf("%s Content-Type = %q, want one of %v", method, contentType, tc.contentTypes)
				}
				if got := recorder.Header().Get("X-Request-ID"); got != "docs-request" {
					t.Errorf("%s X-Request-ID = %q, want docs-request", method, got)
				}
				if method == http.MethodHead {
					if recorder.Body.Len() != 0 {
						t.Errorf("HEAD returned %d body bytes", recorder.Body.Len())
					}
				} else if !bytes.Equal(recorder.Body.Bytes(), data) {
					t.Error("GET response differs from embedded file")
				}
			}
		})
	}
}

func TestDocumentationRedirectsToTrailingSlash(t *testing.T) {
	handler := newTestHandler(jobsStub{}, quotesStub{})
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, "/docs", nil))
		if recorder.Code != http.StatusMovedPermanently {
			t.Errorf("%s status = %d, want 301", method, recorder.Code)
		}
		if got := recorder.Header().Get("Location"); got != "/docs/" {
			t.Errorf("%s Location = %q, want /docs/", method, got)
		}
	}
}

func TestDocumentationDoesNotExposeOtherFiles(t *testing.T) {
	handler := newTestHandler(jobsStub{}, quotesStub{})
	for _, path := range []string{"/docs/missing.js", "/docs/LICENSE", "/docs/../openapi.yaml", "/docs/%2e%2e/README.md"} {
		t.Run(path, func(t *testing.T) {
			recorder, response := call(t, handler, http.MethodGet, path, "")
			if recorder.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", recorder.Code)
			}
			if response["code"] != "NOT_FOUND" {
				t.Errorf("code = %v, want NOT_FOUND", response["code"])
			}
		})
	}
}

func TestDocumentationAcceptsOnlyReadMethods(t *testing.T) {
	handler := newTestHandler(jobsStub{}, quotesStub{})
	for _, path := range []string{"/docs", "/docs/", "/docs/swagger-ui-bundle.js", "/openapi.yaml"} {
		t.Run(path, func(t *testing.T) {
			recorder, response := call(t, handler, http.MethodPost, path, "")
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", recorder.Code)
			}
			if response["code"] != "METHOD_NOT_ALLOWED" {
				t.Errorf("code = %v, want METHOD_NOT_ALLOWED", response["code"])
			}
			if got := recorder.Header().Get("Allow"); got != "GET, HEAD" {
				t.Errorf("Allow = %q, want GET, HEAD", got)
			}
		})
	}
}
