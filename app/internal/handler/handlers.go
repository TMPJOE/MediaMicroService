// Package handler provides HTTP request handlers, routing, and middleware.
// It handles incoming HTTP requests, delegates to the service layer for
// business logic, and returns JSON responses with appropriate status codes.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"hotel.com/app/internal/helper"
	"hotel.com/app/internal/service"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	s       service.Service
	l       *slog.Logger
	jwtAuth *JWTValidator
	bucket  string // default bucket name
}

// New constructs a Handler.
func New(s service.Service, l *slog.Logger, jwtAuth *JWTValidator, bucket string) *Handler {
	return &Handler{
		s:       s,
		l:       l,
		jwtAuth: jwtAuth,
		bucket:  bucket,
	}
}

// healthCheck always returns 200 OK while the process is running.
func (h *Handler) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// readinessCheck verifies DB connectivity before accepting traffic.
func (h *Handler) readinessCheck(w http.ResponseWriter, r *http.Request) {
	if err := h.s.Check(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "reason": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready", "db": "ok"})
}

// uploadFile handles POST /upload.
// Expects multipart/form-data with a "file" field.
// Returns the bucket and object key on success.
func (h *Handler) uploadFile(w http.ResponseWriter, r *http.Request) {
	// 32 MB max in-memory; the rest spills to disk.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		helper.RespondError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		helper.RespondError(w, http.StatusBadRequest, "missing or invalid 'file' field")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	resp, err := h.s.UploadFile(r.Context(), h.bucket, header.Filename, file, header.Size, contentType)
	if err != nil {
		h.l.Error("uploadFile service error", "err", err)
		helper.RespondError(w, http.StatusInternalServerError, helper.ErrProcessingFailed.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// downloadFile handles GET /download/{bucket}/{key}.
// Streams the object directly to the client with the correct Content-Type.
func (h *Handler) downloadFile(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	if bucket == "" || key == "" {
		helper.RespondError(w, http.StatusBadRequest, "bucket and key path parameters are required")
		return
	}

	result, err := h.s.DownloadFile(r.Context(), bucket, key)
	if err != nil {
		h.l.Error("downloadFile service error", "bucket", bucket, "key", key, "err", err)
		helper.RespondError(w, http.StatusNotFound, helper.ErrNotFound.Error())
		return
	}
	defer result.Body.Close()

	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, result.FileName))
	if result.Size > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", result.Size))
	}
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, result.Body); err != nil {
		h.l.Error("streaming file to client failed", "err", err)
	}
}
