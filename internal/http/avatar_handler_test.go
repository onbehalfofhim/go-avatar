package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-avatar-service/internal/domain"
	"go-avatar-service/internal/http/mocks"
	"go-avatar-service/internal/image"
	"go-avatar-service/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func multipartRequest(
	t *testing.T,
	fieldName string,
	fileName string,
	content []byte,
) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile(fieldName, fileName)
	require.NoError(t, err)

	_, err = part.Write(content)
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/avatars",
		&body,
	)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-User-ID", "user-123")

	return request
}

func avatarContent(contentType, body string) service.AvatarContent {
	return service.AvatarContent{
		Body:        io.NopCloser(strings.NewReader(body)),
		ContentType: contentType,
		Size:        int64(len(body)),
	}
}

func TestAvatarHandlerUpload(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	expectedAvatar := domain.Avatar{
		ID:               "avatar-123",
		UserID:           "user-123",
		ProcessingStatus: domain.ProcessingStatusPending,
	}

	serviceMock.
		EXPECT().
		Upload(gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
			input service.UploadInput,
		) (domain.Avatar, error) {
			require.Equal(t, "user-123", input.UserID)
			require.Equal(t, "avatar.jpg", input.FileName)
			require.Equal(t, []byte("image content"), input.Content)

			return expectedAvatar, nil
		})

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := multipartRequest(
		t,
		"image",
		"avatar.jpg",
		[]byte("image content"),
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	var result uploadResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))

	require.Equal(t, expectedAvatar.ID, result.ID)
	require.Equal(t, "pending", result.Status)
}

func TestAvatarHandlerUploadWithFileField(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		Upload(gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
			input service.UploadInput,
		) (domain.Avatar, error) {
			require.Equal(t, "user-123", input.UserID)
			require.Equal(t, "avatar.jpg", input.FileName)
			require.Equal(t, []byte("image content"), input.Content)

			return domain.Avatar{
				ID:               "avatar-123",
				ProcessingStatus: domain.ProcessingStatusPending,
			}, nil
		})

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := multipartRequest(
		t,
		"file",
		"avatar.jpg",
		[]byte("image content"),
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusCreated, response.Code)
}

func TestAvatarHandlerUploadWithoutUserID(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := multipartRequest(
		t,
		"image",
		"avatar.jpg",
		[]byte("image content"),
	)
	request.Header.Del("X-User-ID")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAvatarHandlerUploadWithoutFile(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/avatars",
		nil,
	)
	request.Header.Set("X-User-ID", "user-123")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAvatarHandlerUploadTooLarge(t *testing.T) {
	content := bytes.Repeat([]byte("x"), int(image.MaxFileSize)+1)

	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := multipartRequest(
		t,
		"image",
		"large.jpg",
		content,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
}

func TestAvatarHandlerGetByIDOriginal(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetContent(gomock.Any(), "avatar-123", "").
		Return(avatarContent("image/jpeg", "original image"), nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar-123",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "image/jpeg", response.Header().Get("Content-Type"))
	require.Equal(t, "14", response.Header().Get("Content-Length"))
	require.Equal(t, "original image", response.Body.String())
}

func TestAvatarHandlerGetByID100Thumbnail(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetContent(gomock.Any(), "avatar-123", "100").
		Return(avatarContent("image/jpeg", "100 thumbnail"), nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar-123?size=100",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "image/jpeg", response.Header().Get("Content-Type"))
	require.Equal(t, "100 thumbnail", response.Body.String())
}

func TestAvatarHandlerGetByID300Thumbnail(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetContent(gomock.Any(), "avatar-123", "300").
		Return(avatarContent("image/jpeg", "300 thumbnail"), nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar-123?size=300",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "image/jpeg", response.Header().Get("Content-Type"))
	require.Equal(t, "300 thumbnail", response.Body.String())
}

func TestAvatarHandlerGetByIDNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetContent(gomock.Any(), "avatar-123", "").
		Return(service.AvatarContent{}, service.ErrNotFound)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar-123",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestAvatarHandlerGetByIDUnsupportedSize(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetContent(gomock.Any(), "avatar-123", "500").
		Return(service.AvatarContent{}, service.ErrInvalidInput)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar-123?size=500",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAvatarHandlerGetCurrent(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	expectedAvatar := domain.Avatar{
		ID:     "avatar-123",
		UserID: "user-123",
	}

	serviceMock.
		EXPECT().
		GetCurrentByUserID(gomock.Any(), "user-123").
		Return(expectedAvatar, nil)

	serviceMock.
		EXPECT().
		GetContent(gomock.Any(), "avatar-123", "").
		Return(avatarContent("image/jpeg", "current avatar"), nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/user-123/avatar",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "image/jpeg", response.Header().Get("Content-Type"))
	require.Equal(t, "current avatar", response.Body.String())
}

func TestAvatarHandlerGetCurrentThumbnail(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetCurrentByUserID(gomock.Any(), "user-123").
		Return(domain.Avatar{
			ID:     "avatar-123",
			UserID: "user-123",
		}, nil)

	serviceMock.
		EXPECT().
		GetContent(gomock.Any(), "avatar-123", "100").
		Return(avatarContent("image/jpeg", "100 thumbnail"), nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/user-123/avatar?size=100",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "image/jpeg", response.Header().Get("Content-Type"))
	require.Equal(t, "100 thumbnail", response.Body.String())
}

func TestAvatarHandlerGetCurrentNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetCurrentByUserID(gomock.Any(), "user-123").
		Return(domain.Avatar{}, service.ErrNotFound)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/user-123/avatar",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestAvatarHandlerGetCurrentWithoutUserID(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/user-123/avatar",
		nil,
	)

	request.SetPathValue("user_id", "")

	response := httptest.NewRecorder()

	handler.getCurrent(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAvatarHandlerList(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	createdAt := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	avatars := []domain.Avatar{
		{
			ID:               "avatar-2",
			UserID:           "user-123",
			FileName:         "new.jpg",
			MimeType:         "image/jpeg",
			SizeBytes:        2000,
			UploadStatus:     domain.UploadStatusUploaded,
			ProcessingStatus: domain.ProcessingStatusCompleted,
			IsActive:         true,
			CreatedAt:        createdAt,
			UpdatedAt:        updatedAt,
		},
		{
			ID:               "avatar-1",
			UserID:           "user-123",
			FileName:         "old.png",
			MimeType:         "image/png",
			SizeBytes:        1000,
			UploadStatus:     domain.UploadStatusUploaded,
			ProcessingStatus: domain.ProcessingStatusCompleted,
			IsActive:         false,
			CreatedAt:        createdAt.Add(-time.Hour),
			UpdatedAt:        createdAt,
		},
	}

	serviceMock.
		EXPECT().
		ListByUserID(gomock.Any(), "user-123").
		Return(avatars, nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/user-123/avatars",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)

	var result []avatarResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))

	require.Len(t, result, 2)
	require.Equal(t, "avatar-2", result[0].ID)
	require.True(t, result[0].IsActive)
	require.Equal(t, "avatar-1", result[1].ID)
	require.False(t, result[1].IsActive)
}

func TestAvatarHandlerListEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		ListByUserID(gomock.Any(), "user-123").
		Return([]domain.Avatar{}, nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/user-123/avatars",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)

	var result []avatarResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))

	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestAvatarHandlerListError(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		ListByUserID(gomock.Any(), "user-123").
		Return(nil, service.ErrInvalidInput)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/users/user-123/avatars",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAvatarHandlerDelete(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		Delete(gomock.Any(), "avatar-123", "user-123").
		Return(domain.Avatar{
			ID:     "avatar-123",
			UserID: "user-123",
		}, nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/avatars/avatar-123",
		nil,
	)
	request.Header.Set("X-User-ID", "user-123")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Empty(t, response.Body.String())
}

func TestAvatarHandlerDeleteWithoutUserID(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/avatars/avatar-123",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAvatarHandlerDeleteNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		Delete(gomock.Any(), "avatar-123", "user-123").
		Return(domain.Avatar{}, service.ErrNotFound)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/avatars/avatar-123",
		nil,
	)
	request.Header.Set("X-User-ID", "user-123")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestAvatarHandlerDeleteForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		Delete(gomock.Any(), "avatar-123", "user-123").
		Return(domain.Avatar{}, service.ErrForbidden)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/avatars/avatar-123",
		nil,
	)
	request.Header.Set("X-User-ID", "user-123")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestAvatarHandlerDeletePassesUserID(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		Delete(gomock.Any(), "avatar-123", "another-user").
		Return(domain.Avatar{
			ID:     "avatar-123",
			UserID: "another-user",
		}, nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/avatars/avatar-123",
		nil,
	)
	request.Header.Set("X-User-ID", "another-user")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestAvatarHandlerDeleteCurrent(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetCurrentByUserID(gomock.Any(), "user-123").
		Return(domain.Avatar{
			ID:       "avatar-123",
			UserID:   "user-123",
			IsActive: true,
		}, nil)

	serviceMock.
		EXPECT().
		Delete(gomock.Any(), "avatar-123", "user-123").
		Return(domain.Avatar{
			ID:     "avatar-123",
			UserID: "user-123",
		}, nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/users/user-123/avatar",
		nil,
	)
	request.Header.Set("X-User-ID", "user-123")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Empty(t, response.Body.String())
}

func TestAvatarHandlerDeleteCurrentWithoutUserID(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/users/user-123/avatar",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestAvatarHandlerDeleteCurrentForbiddenForAnotherUser(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/users/user-123/avatar",
		nil,
	)
	request.Header.Set("X-User-ID", "another-user")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestAvatarHandlerDeleteCurrentNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetCurrentByUserID(gomock.Any(), "user-123").
		Return(domain.Avatar{}, service.ErrNotFound)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/users/user-123/avatar",
		nil,
	)
	request.Header.Set("X-User-ID", "user-123")

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestAvatarHandlerMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	createdAt := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	serviceMock.
		EXPECT().
		GetByID(gomock.Any(), "avatar-123").
		Return(domain.Avatar{
			ID:               "avatar-123",
			UserID:           "user-123",
			FileName:         "avatar.jpg",
			MimeType:         "image/jpeg",
			SizeBytes:        12345,
			UploadStatus:     domain.UploadStatusUploaded,
			ProcessingStatus: domain.ProcessingStatusCompleted,
			IsActive:         true,
			CreatedAt:        createdAt,
			UpdatedAt:        updatedAt,
		}, nil)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar-123/metadata",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "application/json", response.Header().Get("Content-Type"))

	var result avatarResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))

	require.Equal(t, "avatar-123", result.ID)
	require.Equal(t, "user-123", result.UserID)
	require.Equal(t, "avatar.jpg", result.FileName)
	require.Equal(t, "image/jpeg", result.MimeType)
	require.Equal(t, int64(12345), result.SizeBytes)
	require.Equal(t, domain.ProcessingStatusCompleted, result.ProcessingStatus)
	require.True(t, result.IsActive)
	require.Nil(t, result.DeletedAt)
}

func TestAvatarHandlerMetadataNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	serviceMock.
		EXPECT().
		GetByID(gomock.Any(), "avatar-123").
		Return(domain.Avatar{}, service.ErrNotFound)

	handler := NewAvatarHandler(serviceMock)

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar-123/metadata",
		nil,
	)

	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestAvatarHandlerMetadataWithoutID(t *testing.T) {
	ctrl := gomock.NewController(t)

	serviceMock := mocks.NewMockAvatarService(ctrl)

	handler := NewAvatarHandler(serviceMock)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/avatar/metadata",
		nil,
	)
	request.SetPathValue("id", "")

	response := httptest.NewRecorder()

	handler.metadata(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}
