package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-avatar-service/internal/config"
	"go-avatar-service/internal/domain"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, cfg.PostgresURL())
	if err != nil {
		t.Fatalf("create postgres pool: %v", err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func testAvatar(userID string) domain.Avatar {
	return domain.Avatar{
		ID:        uuid.NewString(),
		UserID:    userID,
		FileName:  "avatar.jpg",
		MimeType:  "image/jpeg",
		SizeBytes: 1024,
		S3Key:     "avatars/test/original.jpg",
		ThumbnailS3Keys: map[string]string{
			"100x100": "avatars/test/100x100.jpg",
			"300x300": "avatars/test/300x300.jpg",
		},
		UploadStatus:     domain.UploadStatusUploaded,
		ProcessingStatus: domain.ProcessingStatusPending,
		IsActive:         true,
	}
}
