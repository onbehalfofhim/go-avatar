package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"go-avatar-service/internal/domain"
)

func TestAvatarRepository_CreateAndGetCurrent(t *testing.T) {
	pool := testPool(t)
	repository := NewAvatarRepository(pool)

	ctx := context.Background()
	userID := "repository-test-user-" + uuid.NewString()

	avatar := testAvatar(userID)

	if err := repository.Create(ctx, avatar); err != nil {
		t.Fatalf("create avatar: %v", err)
	}

	got, err := repository.GetCurrentByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("get current avatar: %v", err)
	}

	if got.ID != avatar.ID {
		t.Fatalf("expected avatar ID %q, got %q", avatar.ID, got.ID)
	}

	if got.UserID != userID {
		t.Fatalf("expected user ID %q, got %q", userID, got.UserID)
	}

	if !got.IsActive {
		t.Fatal("expected avatar to be active")
	}

	if got.DeletedAt != nil {
		t.Fatal("expected avatar to not be deleted")
	}
}

func TestAvatarRepository_CreateDeactivatesPreviousAvatar(t *testing.T) {
	pool := testPool(t)
	repository := NewAvatarRepository(pool)

	ctx := context.Background()
	userID := "repository-test-user-" + uuid.NewString()

	first := testAvatar(userID)
	if err := repository.Create(ctx, first); err != nil {
		t.Fatalf("create first avatar: %v", err)
	}

	second := testAvatar(userID)
	if err := repository.Create(ctx, second); err != nil {
		t.Fatalf("create second avatar: %v", err)
	}

	current, err := repository.GetCurrentByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("get current avatar: %v", err)
	}

	if current.ID != second.ID {
		t.Fatalf(
			"expected second avatar %q to be current, got %q",
			second.ID,
			current.ID,
		)
	}

	previous, err := repository.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("get previous avatar: %v", err)
	}

	if previous.IsActive {
		t.Fatal("expected previous avatar to be inactive")
	}

	if previous.DeletedAt != nil {
		t.Fatal("expected previous avatar to remain undeleted")
	}
}

func TestAvatarRepository_ListByUserIDDoesNotReturnDeleted(t *testing.T) {
	pool := testPool(t)
	repository := NewAvatarRepository(pool)

	ctx := context.Background()
	userID := "repository-test-user-" + uuid.NewString()

	avatar := testAvatar(userID)
	if err := repository.Create(ctx, avatar); err != nil {
		t.Fatalf("create avatar: %v", err)
	}

	deleted, err := repository.Delete(ctx, avatar.ID, avatar.UserID)
	if err != nil {
		t.Fatalf("delete avatar: %v", err)
	}

	if deleted.ID != avatar.ID {
		t.Fatalf(
			"expected deleted avatar ID %q, got %q",
			avatar.ID,
			deleted.ID,
		)
	}

	if deleted.UploadStatus != domain.UploadStatusDeleted {
		t.Fatalf(
			"expected upload status %q, got %q",
			domain.UploadStatusDeleted,
			deleted.UploadStatus,
		)
	}

	if deleted.IsActive {
		t.Fatal("expected deleted avatar to be inactive")
	}

	if deleted.DeletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}

	avatars, err := repository.ListByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("list avatars: %v", err)
	}

	if len(avatars) != 0 {
		t.Fatalf("expected no avatars, got %d", len(avatars))
	}

	deleted, err = repository.GetByID(ctx, avatar.ID)
	if err != nil {
		t.Fatalf("get deleted avatar: %v", err)
	}

	_, err = repository.Delete(ctx, avatar.ID, avatar.UserID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows on repeated delete, got %v", err)
	}

	if deleted.UploadStatus != domain.UploadStatusDeleted {
		t.Fatalf(
			"expected upload status %q, got %q",
			domain.UploadStatusDeleted,
			deleted.UploadStatus,
		)
	}

	if deleted.DeletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}

	if deleted.IsActive {
		t.Fatal("expected deleted avatar to be inactive")
	}
}

func TestAvatarRepository_UpdateProcessingStatus(t *testing.T) {
	pool := testPool(t)
	repository := NewAvatarRepository(pool)

	ctx := context.Background()
	userID := "repository-test-user-" + uuid.NewString()

	avatar := testAvatar(userID)
	if err := repository.Create(ctx, avatar); err != nil {
		t.Fatalf("create avatar: %v", err)
	}

	if err := repository.UpdateProcessingStatus(
		ctx,
		avatar.ID,
		domain.ProcessingStatusProcessing,
	); err != nil {
		t.Fatalf("update processing status: %v", err)
	}

	got, err := repository.GetByID(ctx, avatar.ID)
	if err != nil {
		t.Fatalf("get avatar: %v", err)
	}

	if got.ProcessingStatus != domain.ProcessingStatusProcessing {
		t.Fatalf(
			"expected processing status %q, got %q",
			domain.ProcessingStatusProcessing,
			got.ProcessingStatus,
		)
	}
}

func TestAvatarRepositoryUpdateThumbnailS3Keys(t *testing.T) {
	pool := testPool(t)
	repository := NewAvatarRepository(pool)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	avatar := testAvatar("thumbnail-update-user")

	if err := repository.Create(ctx, avatar); err != nil {
		t.Fatalf("create avatar: %v", err)
	}

	keys := map[string]string{
		"100x100": "avatars/test/100x100.jpg",
		"300x300": "avatars/test/300x300.jpg",
	}

	if err := repository.UpdateThumbnailS3Keys(
		ctx,
		avatar.ID,
		keys,
	); err != nil {
		t.Fatalf("update thumbnail keys: %v", err)
	}

	got, err := repository.GetByID(ctx, avatar.ID)
	if err != nil {
		t.Fatalf("get avatar: %v", err)
	}

	if len(got.ThumbnailS3Keys) != len(keys) {
		t.Fatalf(
			"expected %d thumbnail keys, got %d",
			len(keys),
			len(got.ThumbnailS3Keys),
		)
	}

	for size, expectedKey := range keys {
		if got.ThumbnailS3Keys[size] != expectedKey {
			t.Fatalf(
				"expected thumbnail key %q for size %q, got %q",
				expectedKey,
				size,
				got.ThumbnailS3Keys[size],
			)
		}
	}
}

func TestAvatarRepository_GetCurrentReturnsNotFound(t *testing.T) {
	pool := testPool(t)
	repository := NewAvatarRepository(pool)

	ctx := context.Background()

	_, err := repository.GetCurrentByUserID(
		ctx,
		"repository-test-user-"+uuid.NewString(),
	)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got %v", err)
	}
}

func TestAvatarRepositoryDeleteDoesNotDeleteAnotherUsersAvatar(t *testing.T) {
	pool := testPool(t)
	repository := NewAvatarRepository(pool)

	avatar := testAvatar("user-owner")
	err := repository.Create(context.Background(), avatar)
	if err != nil {
		t.Fatalf("create avatar: %v", err)
	}

	_, err = repository.Delete(
		context.Background(),
		avatar.ID,
		"another-user",
	)
	if !IsNotFound(err) {
		t.Fatalf("delete with another user error = %v, want not found", err)
	}

	current, err := repository.GetCurrentByUserID(
		context.Background(),
		avatar.UserID,
	)
	if err != nil {
		t.Fatalf("get current avatar: %v", err)
	}

	if current.ID != avatar.ID {
		t.Fatalf(
			"current avatar ID = %q, want %q",
			current.ID,
			avatar.ID,
		)
	}
}
