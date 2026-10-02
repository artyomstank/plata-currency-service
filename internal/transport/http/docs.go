package http

import (
	"bytes"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"currency-quotes/docs"
)

func serveDocumentation(w http.ResponseWriter, r *http.Request) error {
	name := chi.URLParam(r, "*")
	if name == "" {
		name = "index.html"
	}
	switch name {
	case "index.html", "swagger-ui.css", "swagger-ui-bundle.js", "swagger-initializer.js":
	default:
		return errRouteNotFound
	}
	data, err := docs.Files.ReadFile("swagger-ui/" + name)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	return nil
}

func serveOpenAPI(w http.ResponseWriter, r *http.Request) error {
	data, err := docs.Files.ReadFile("openapi.yaml")
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "openapi.yaml", time.Time{}, bytes.NewReader(data))
	return nil
}

func redirectDocumentation(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
}
