package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-avatar-service/internal/domain"
)

type AvatarRepository struct {
	pool *pgxpool.Pool
}

func NewAvatarRepository(pool *pgxpool.Pool) *AvatarRepository {
	return &AvatarRepository{
		pool: pool,
	}
}

func (r *AvatarRepository) Create(
	ctx context.Context,
	avatar domain.Avatar,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(
		ctx,
		`
		UPDATE avatars
		SET
			is_active = FALSE,
			updated_at = NOW()
		WHERE user_id = $1
			AND is_active = TRUE
			AND deleted_at IS NULL
		`,
		avatar.UserID,
	)
	if err != nil {
		return fmt.Errorf("deactivate current avatar: %w", err)
	}

	thumbnailS3Keys, err := json.Marshal(avatar.ThumbnailS3Keys)
	if err != nil {
		return fmt.Errorf("marshal thumbnail s3 keys: %w", err)
	}

	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO avatars (
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			s3_key,
			thumbnail_s3_keys,
			upload_status,
			processing_status,
			is_active
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10
		)
		`,
		avatar.ID,
		avatar.UserID,
		avatar.FileName,
		avatar.MimeType,
		avatar.SizeBytes,
		avatar.S3Key,
		thumbnailS3Keys,
		avatar.UploadStatus,
		avatar.ProcessingStatus,
		avatar.IsActive,
	)
	if err != nil {
		return fmt.Errorf("insert avatar: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (r *AvatarRepository) CreateWithOutbox(
	ctx context.Context,
	avatar domain.Avatar,
	event domain.OutboxEvent,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	_, err = tx.Exec(
		ctx,
		`
		UPDATE avatars
		SET
			is_active = FALSE,
			updated_at = NOW()
		WHERE user_id = $1
			AND is_active = TRUE
			AND deleted_at IS NULL
		`,
		avatar.UserID,
	)
	if err != nil {
		return fmt.Errorf("deactivate current avatar: %w", err)
	}

	thumbnailS3Keys, err := json.Marshal(avatar.ThumbnailS3Keys)
	if err != nil {
		return fmt.Errorf("marshal thumbnail s3 keys: %w", err)
	}

	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO avatars (
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			s3_key,
			thumbnail_s3_keys,
			upload_status,
			processing_status,
			is_active
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10
		)
		`,
		avatar.ID,
		avatar.UserID,
		avatar.FileName,
		avatar.MimeType,
		avatar.SizeBytes,
		avatar.S3Key,
		thumbnailS3Keys,
		avatar.UploadStatus,
		avatar.ProcessingStatus,
		avatar.IsActive,
	)
	if err != nil {
		return fmt.Errorf("insert avatar: %w", err)
	}

	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (r *AvatarRepository) GetByID(
	ctx context.Context,
	id string,
) (domain.Avatar, error) {
	row := r.pool.QueryRow(
		ctx,
		`
		SELECT
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			s3_key,
			thumbnail_s3_keys,
			upload_status,
			processing_status,
			is_active,
			created_at,
			updated_at,
			deleted_at
		FROM avatars
		WHERE id = $1
		`,
		id,
	)

	return scanAvatar(row)
}

func (r *AvatarRepository) GetCurrentByUserID(
	ctx context.Context,
	userID string,
) (domain.Avatar, error) {
	row := r.pool.QueryRow(
		ctx,
		`
		SELECT
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			s3_key,
			thumbnail_s3_keys,
			upload_status,
			processing_status,
			is_active,
			created_at,
			updated_at,
			deleted_at
		FROM avatars
		WHERE user_id = $1
			AND is_active = TRUE
			AND deleted_at IS NULL
		`,
		userID,
	)

	return scanAvatar(row)
}

func (r *AvatarRepository) ListByUserID(
	ctx context.Context,
	userID string,
) ([]domain.Avatar, error) {
	rows, err := r.pool.Query(
		ctx,
		`
		SELECT
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			s3_key,
			thumbnail_s3_keys,
			upload_status,
			processing_status,
			is_active,
			created_at,
			updated_at,
			deleted_at
		FROM avatars
		WHERE user_id = $1
			AND deleted_at IS NULL
		ORDER BY created_at DESC
		`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list avatars: %w", err)
	}
	defer rows.Close()

	avatars := make([]domain.Avatar, 0)

	for rows.Next() {
		avatar, err := scanAvatar(rows)
		if err != nil {
			return nil, fmt.Errorf("scan avatar: %w", err)
		}

		avatars = append(avatars, avatar)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate avatars: %w", err)
	}

	return avatars, nil
}

func (r *AvatarRepository) Delete(
	ctx context.Context,
	id string,
	userID string,
) (domain.Avatar, error) {
	row := r.pool.QueryRow(
		ctx,
		`
		UPDATE avatars
		SET
			upload_status = $1,
			is_active = FALSE,
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $2
			AND user_id = $3
			AND deleted_at IS NULL
		RETURNING
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			s3_key,
			thumbnail_s3_keys,
			upload_status,
			processing_status,
			is_active,
			created_at,
			updated_at,
			deleted_at
		`,
		domain.UploadStatusDeleted,
		id,
		userID,
	)

	avatar, err := scanAvatar(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Avatar{}, pgx.ErrNoRows
		}

		return domain.Avatar{}, fmt.Errorf("delete avatar: %w", err)
	}

	return avatar, nil
}

func (r *AvatarRepository) DeleteWithOutbox(
	ctx context.Context,
	id string,
	userID string,
	event domain.OutboxEvent,
) (domain.Avatar, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Avatar{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	row := tx.QueryRow(
		ctx,
		`
		UPDATE avatars
		SET
			upload_status = $1,
			is_active = FALSE,
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $2
			AND user_id = $3
			AND deleted_at IS NULL
		RETURNING
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			s3_key,
			thumbnail_s3_keys,
			upload_status,
			processing_status,
			is_active,
			created_at,
			updated_at,
			deleted_at
		`,
		domain.UploadStatusDeleted,
		id,
		userID,
	)

	avatar, err := scanAvatar(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Avatar{}, pgx.ErrNoRows
		}

		return domain.Avatar{}, fmt.Errorf("delete avatar: %w", err)
	}

	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return domain.Avatar{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Avatar{}, fmt.Errorf("commit transaction: %w", err)
	}

	return avatar, nil
}

func (r *AvatarRepository) UpdateProcessingStatus(
	ctx context.Context,
	id string,
	status domain.ProcessingStatus,
) error {
	tag, err := r.pool.Exec(
		ctx,
		`
		UPDATE avatars
		SET
			processing_status = $1,
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
		`,
		status,
		id,
	)
	if err != nil {
		return fmt.Errorf("update processing status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

func (r *AvatarRepository) UpdateThumbnailS3Keys(
	ctx context.Context,
	id string,
	keys map[string]string,
) error {
	thumbnailS3Keys, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("marshal thumbnail s3 keys: %w", err)
	}

	tag, err := r.pool.Exec(
		ctx,
		`
		UPDATE avatars
		SET thumbnail_s3_keys = $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		`,
		thumbnailS3Keys,
		id,
	)
	if err != nil {
		return fmt.Errorf("update thumbnail s3 keys: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

type avatarRow interface {
	Scan(dest ...any) error
}

func scanAvatar(row avatarRow) (domain.Avatar, error) {
	var (
		avatar          domain.Avatar
		thumbnailS3Keys []byte
	)

	err := row.Scan(
		&avatar.ID,
		&avatar.UserID,
		&avatar.FileName,
		&avatar.MimeType,
		&avatar.SizeBytes,
		&avatar.S3Key,
		&thumbnailS3Keys,
		&avatar.UploadStatus,
		&avatar.ProcessingStatus,
		&avatar.IsActive,
		&avatar.CreatedAt,
		&avatar.UpdatedAt,
		&avatar.DeletedAt,
	)
	if err != nil {
		return domain.Avatar{}, err
	}

	if err := json.Unmarshal(thumbnailS3Keys, &avatar.ThumbnailS3Keys); err != nil {
		return domain.Avatar{}, fmt.Errorf(
			"unmarshal thumbnail s3 keys: %w",
			err,
		)
	}

	return avatar, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func insertOutboxEvent(
	ctx context.Context,
	tx pgx.Tx,
	event domain.OutboxEvent,
) error {
	_, err := tx.Exec(
		ctx,
		`
		INSERT INTO outbox_events (
			message_id,
			routing_key,
			payload
		)
		VALUES ($1, $2, $3)
		`,
		event.MessageID,
		event.RoutingKey,
		event.Payload,
	)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return nil
}
