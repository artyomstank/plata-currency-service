package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	uc    *UseCases
	log   *slog.Logger
	ready func(context.Context) error
}

func (h *Handler) requestUpdate(w http.ResponseWriter, r *http.Request) error {
	var request updateRequest
	if err := decodeJSON(r, &request); err != nil {
		return err
	}
	input := toRequestUpdateInput(request, r.Header.Get("Idempotency-Key"))
	result, err := h.uc.RequestUpdate.Execute(r.Context(), input)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusAccepted
	}
	return writeJSON(w, r, status, toUpdateResponse(result))
}

func (h *Handler) getUpdate(w http.ResponseWriter, r *http.Request) error {
	input, err := toGetUpdateInput(chi.URLParam(r, "job_id"))
	if err != nil {
		return err
	}
	result, err := h.uc.GetJobResult.Execute(r.Context(), input)
	if err != nil {
		return err
	}
	return writeJSON(w, r, http.StatusOK, toJobResponse(result))
}

func (h *Handler) getLatest(w http.ResponseWriter, r *http.Request) error {
	input := toGetLatestInput(r.URL.Query().Get("pair"))
	value, err := h.uc.GetLatest.Execute(r.Context(), input)
	if err != nil {
		return err
	}
	return writeJSON(w, r, http.StatusOK, toLatestResponse(value))
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) readiness(w http.ResponseWriter, r *http.Request) error {
	if err := h.ready(r.Context()); err != nil {
		h.log.WarnContext(r.Context(), "readiness check failed", "err", err)
		return writeJSON(w, r, http.StatusServiceUnavailable, healthResponse{Status: "not_ready"})
	}
	return writeJSON(w, r, http.StatusOK, healthResponse{Status: "ready"})
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %w", errInvalidJSON, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: multiple JSON values", errInvalidJSON)
		}
		return fmt.Errorf("%w: %w", errInvalidJSON, err)
	}
	return nil
}
