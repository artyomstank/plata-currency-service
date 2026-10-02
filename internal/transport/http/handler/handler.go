package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

var ErrInvalidJSON = errors.New("invalid JSON request body")

type Handler struct {
	uc        *UseCases
	log       *slog.Logger
	ready     func(context.Context) error
	writeJSON func(http.ResponseWriter, *http.Request, int, any) error
}

func New(uc *UseCases, log *slog.Logger, ready func(context.Context) error, writeJSON func(http.ResponseWriter, *http.Request, int, any) error) *Handler {
	return &Handler{uc: uc, log: log, ready: ready, writeJSON: writeJSON}
}

func (h *Handler) RequestUpdate(w http.ResponseWriter, r *http.Request) error {
	var request updateRequest
	if err := decodeJSON(r, &request); err != nil {
		return err
	}
	input := toRequestUpdateInput(request)
	result, err := h.uc.RequestUpdate.Execute(r.Context(), input)
	if err != nil {
		return err
	}
	return h.writeJSON(w, r, http.StatusAccepted, toUpdateResponse(result))
}

func (h *Handler) GetUpdate(w http.ResponseWriter, r *http.Request) error {
	input, err := toGetUpdateInput(chi.URLParam(r, "job_id"))
	if err != nil {
		return err
	}
	result, err := h.uc.GetJobResult.Execute(r.Context(), input)
	if err != nil {
		return err
	}
	return h.writeJSON(w, r, http.StatusOK, toJobResponse(result))
}

func (h *Handler) GetLatest(w http.ResponseWriter, r *http.Request) error {
	input := toGetLatestInput(r.URL.Query().Get("pair"))
	value, err := h.uc.GetLatest.Execute(r.Context(), input)
	if err != nil {
		return err
	}
	return h.writeJSON(w, r, http.StatusOK, toLatestResponse(value))
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) error {
	return h.writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) error {
	if err := h.ready(r.Context()); err != nil {
		h.log.WarnContext(r.Context(), "readiness check failed", "err", err)
		return h.writeJSON(w, r, http.StatusServiceUnavailable, healthResponse{Status: "not_ready"})
	}
	return h.writeJSON(w, r, http.StatusOK, healthResponse{Status: "ready"})
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%w: multiple JSON values", ErrInvalidJSON)
		}
		return fmt.Errorf("%w: %w", ErrInvalidJSON, err)
	}
	return nil
}
