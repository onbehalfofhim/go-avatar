package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-avatar-service/internal/domain"
	"go-avatar-service/internal/image"
	"go-avatar-service/internal/service"

	"github.com/go-chi/chi/v5"
)

const userIDHeader = "X-User-ID"

var (
	errUserIDRequired = errors.New("X-User-ID header is required")
	errInvalidID      = errors.New("avatar id is required")
)

type AvatarService interface {
	Upload(ctx context.Context, input service.UploadInput) (domain.Avatar, error)
	GetByID(ctx context.Context, id string) (domain.Avatar, error)
	GetContent(ctx context.Context, id, size string) (service.AvatarContent, error)
	GetCurrentByUserID(ctx context.Context, userID string) (domain.Avatar, error)
	ListByUserID(ctx context.Context, userID string) ([]domain.Avatar, error)
	Delete(ctx context.Context, id, userID string) (domain.Avatar, error)
}

type AvatarHandler struct {
	service AvatarService
}

func NewAvatarHandler(service AvatarService) *AvatarHandler {
	return &AvatarHandler{
		service: service,
	}
}

func (h *AvatarHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/avatars", h.upload)

		r.Get("/avatars/{id}", h.getByID)
		r.Delete("/avatars/{id}", h.delete)

		r.Get("/avatars/{id}/metadata", h.metadata)

		r.Get("/users/{user_id}/avatar", h.getCurrent)
		r.Delete("/users/{user_id}/avatar", h.deleteCurrent)

		r.Get("/users/{user_id}/avatars", h.list)
	})
}

type errorResponse struct {
	Error string `json:"error"`
}

type uploadResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type avatarResponse struct {
	ID               string                  `json:"id"`
	UserID           string                  `json:"user_id"`
	FileName         string                  `json:"file_name"`
	MimeType         string                  `json:"mime_type"`
	SizeBytes        int64                   `json:"size_bytes"`
	UploadStatus     domain.UploadStatus     `json:"upload_status"`
	ProcessingStatus domain.ProcessingStatus `json:"processing_status"`
	IsActive         bool                    `json:"is_active"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	DeletedAt        *time.Time              `json:"deleted_at,omitempty"`
}

func newAvatarResponse(avatar domain.Avatar) avatarResponse {
	return avatarResponse{
		ID:               avatar.ID,
		UserID:           avatar.UserID,
		FileName:         avatar.FileName,
		MimeType:         avatar.MimeType,
		SizeBytes:        avatar.SizeBytes,
		UploadStatus:     avatar.UploadStatus,
		ProcessingStatus: avatar.ProcessingStatus,
		IsActive:         avatar.IsActive,
		CreatedAt:        avatar.CreatedAt,
		UpdatedAt:        avatar.UpdatedAt,
		DeletedAt:        avatar.DeletedAt,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorResponse{
		Error: err.Error(),
	})
}

func userIDFromRequest(r *http.Request) (string, error) {
	userID := strings.TrimSpace(r.Header.Get(userIDHeader))
	if userID == "" {
		return "", errUserIDRequired
	}

	return userID, nil
}

func pathID(r *http.Request) (string, error) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		return "", errInvalidID
	}

	return id, nil
}

func (h *AvatarHandler) upload(w http.ResponseWriter, r *http.Request) {
	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, image.MaxFileSize+1)
	if err := r.ParseMultipartForm(image.MaxFileSize + 1); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, err)
			return
		}

		writeError(w, http.StatusBadRequest, err)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		file, header, err = r.FormFile("file")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Error("close uploaded file", "error", err)
		}
	}()

	content, err := io.ReadAll(file)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, err)
			return
		}

		writeError(w, http.StatusBadRequest, err)
		return
	}

	if int64(len(content)) > image.MaxFileSize {
		writeError(w, http.StatusRequestEntityTooLarge, image.ErrFileTooLarge)
		return
	}

	avatar, err := h.service.Upload(
		r.Context(),
		service.UploadInput{
			UserID:    userID,
			FileName:  header.Filename,
			MimeType:  header.Header.Get("Content-Type"),
			SizeBytes: int64(len(content)),
			Content:   content,
		},
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, uploadResponse{
		ID:     avatar.ID,
		Status: string(avatar.ProcessingStatus),
	})
}

func (h *AvatarHandler) getByID(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	size := r.URL.Query().Get("size")

	content, err := h.service.GetContent(r.Context(), id, size)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer func() {
		if err := content.Body.Close(); err != nil {
			slog.Error("close response body", "error", err)
		}
	}()

	w.Header().Set("Content-Type", content.ContentType)

	if content.Size > 0 {
		w.Header().Set(
			"Content-Length",
			strconv.FormatInt(content.Size, 10),
		)
	}

	if _, err := io.Copy(w, content.Body); err != nil {
		return
	}
}

func (h *AvatarHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if _, err := h.service.Delete(r.Context(), id, userID); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AvatarHandler) getCurrent(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeError(
			w,
			http.StatusBadRequest,
			errors.New("user_id is required"),
		)
		return
	}

	avatar, err := h.service.GetCurrentByUserID(
		r.Context(),
		userID,
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	size := r.URL.Query().Get("size")

	content, err := h.service.GetContent(
		r.Context(),
		avatar.ID,
		size,
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer func() {
		if err := content.Body.Close(); err != nil {
			slog.Error("close response body", "error", err)
		}
	}()

	w.Header().Set("Content-Type", content.ContentType)

	if content.Size > 0 {
		w.Header().Set(
			"Content-Length",
			strconv.FormatInt(content.Size, 10),
		)
	}

	if _, err := io.Copy(w, content.Body); err != nil {
		return
	}
}

func (h *AvatarHandler) deleteCurrent(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeError(
			w,
			http.StatusBadRequest,
			errors.New("user_id is required"),
		)
		return
	}

	headerUserID, err := userIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if headerUserID != userID {
		writeError(
			w,
			http.StatusForbidden,
			errors.New("X-User-ID does not match user_id"),
		)
		return
	}

	avatar, err := h.service.GetCurrentByUserID(
		r.Context(),
		userID,
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	if _, err := h.service.Delete(
		r.Context(),
		avatar.ID,
		userID,
	); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AvatarHandler) list(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "user_id"))
	if userID == "" {
		writeError(
			w,
			http.StatusBadRequest,
			errors.New("user_id is required"),
		)
		return
	}

	avatars, err := h.service.ListByUserID(
		r.Context(),
		userID,
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	response := make([]avatarResponse, 0, len(avatars))
	for _, avatar := range avatars {
		response = append(response, newAvatarResponse(avatar))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *AvatarHandler) metadata(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	avatar, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, newAvatarResponse(avatar))
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err)

	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, err)

	case errors.Is(err, service.ErrForbidden):
		writeError(w, http.StatusForbidden, err)

	default:
		writeError(
			w,
			http.StatusInternalServerError,
			errors.New("internal server error"),
		)
	}
}
