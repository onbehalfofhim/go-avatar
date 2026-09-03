package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"go-avatar-service/internal/broker/events"
	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/domain"
	"go-avatar-service/internal/image"
	"go-avatar-service/internal/storage/postgres"
	"go-avatar-service/internal/storage/s3"
)

type UploadInput struct {
	UserID    string
	FileName  string
	MimeType  string
	SizeBytes int64
	Content   []byte
}

type AvatarRepository interface {
	Create(ctx context.Context, avatar domain.Avatar) error
	CreateWithOutbox(
		ctx context.Context,
		avatar domain.Avatar,
		event domain.OutboxEvent,
	) error

	GetByID(ctx context.Context, id string) (domain.Avatar, error)
	GetCurrentByUserID(ctx context.Context, userID string) (domain.Avatar, error)
	ListByUserID(ctx context.Context, userID string) ([]domain.Avatar, error)

	Delete(ctx context.Context, id string, userID string) (domain.Avatar, error)
	DeleteWithOutbox(
		ctx context.Context,
		id string,
		userID string,
		event domain.OutboxEvent,
	) (domain.Avatar, error)
}

type AvatarService struct {
	repository AvatarRepository
	storage    *s3.Client
	bucket     string
}

type AvatarContent struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

func NewAvatarService(
	repository AvatarRepository,
	storage *s3.Client,
	bucket string,
) *AvatarService {
	return &AvatarService{
		repository: repository,
		storage:    storage,
		bucket:     bucket,
	}
}

func (s *AvatarService) Upload(
	ctx context.Context,
	input UploadInput,
) (domain.Avatar, error) {
	if strings.TrimSpace(input.UserID) == "" {
		return domain.Avatar{}, fmt.Errorf(
			"%w: user ID is required",
			ErrInvalidInput,
		)
	}

	img, info, err := image.DecodeAndValidate(input.Content)
	if err != nil {
		return domain.Avatar{}, fmt.Errorf(
			"%w: validate image: %w",
			ErrInvalidInput,
			err,
		)
	}

	avatarID := uuid.NewString()

	extension := fileExtension(info.Format)
	s3Key := fmt.Sprintf(
		"avatars/%s/original.%s",
		avatarID,
		extension,
	)

	if err := s.storage.PutObject(
		ctx,
		s.bucket,
		s3Key,
		bytes.NewReader(input.Content),
		info.ContentType,
		int64(len(input.Content)),
	); err != nil {
		return domain.Avatar{}, fmt.Errorf(
			"upload original image: %w",
			err,
		)
	}

	avatar := domain.Avatar{
		ID:               avatarID,
		UserID:           input.UserID,
		FileName:         input.FileName,
		MimeType:         info.ContentType,
		SizeBytes:        int64(len(input.Content)),
		S3Key:            s3Key,
		ThumbnailS3Keys:  map[string]string{},
		UploadStatus:     domain.UploadStatusUploaded,
		ProcessingStatus: domain.ProcessingStatusPending,
		IsActive:         true,
	}

	if err := s.repository.Create(ctx, avatar); err != nil {
		return domain.Avatar{}, fmt.Errorf(
			"create avatar: %w",
			err,
		)
	}

	messageID := uuid.NewString()

	event := events.AvatarUploadEvent{

		MessageID: messageID,
		AvatarID:  avatar.ID,
		UserID:    avatar.UserID,
		S3Key:     avatar.S3Key,
	}

	payload, err := json.Marshal(event)

	if err != nil {

		return domain.Avatar{}, fmt.Errorf(
			"marshal avatar upload event: %w",
			err,
		)

	}

	outboxEvent := domain.OutboxEvent{

		MessageID:  messageID,
		RoutingKey: rabbitmq.UploadRoutingKey,
		Payload:    payload,
	}

	if err := s.repository.CreateWithOutbox(

		ctx,
		avatar,
		outboxEvent,
	); err != nil {

		return domain.Avatar{}, fmt.Errorf(
			"create avatar: %w",
			err,
		)

	}

	_ = img

	return avatar, nil
}

func fileExtension(format string) string {
	switch format {
	case "jpeg":
		return "jpg"
	case "png":
		return "png"
	case "webp":
		return "webp"
	default:
		return ""
	}
}

func (s *AvatarService) GetByID(
	ctx context.Context,
	id string,
) (domain.Avatar, error) {
	avatar, err := s.repository.GetByID(ctx, id)
	if err != nil {
		if postgres.IsNotFound(err) {
			return domain.Avatar{}, fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)
		}

		return domain.Avatar{}, fmt.Errorf(
			"get avatar %q: %w",
			id,
			err,
		)
	}

	if avatar.DeletedAt != nil {
		return domain.Avatar{}, fmt.Errorf(
			"%w: %q",
			ErrNotFound,
			id,
		)
	}

	return avatar, nil
}

func (s *AvatarService) GetCurrentByUserID(
	ctx context.Context,
	userID string,
) (domain.Avatar, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.Avatar{}, fmt.Errorf(
			"%w: user ID is empty",
			ErrInvalidInput,
		)
	}

	avatar, err := s.repository.GetCurrentByUserID(ctx, userID)
	if err != nil {
		if postgres.IsNotFound(err) {
			return domain.Avatar{}, fmt.Errorf(
				"%w: current avatar for user %q",
				ErrNotFound,
				userID,
			)
		}

		return domain.Avatar{}, fmt.Errorf(
			"get current avatar for user %q: %w",
			userID,
			err,
		)
	}

	return avatar, nil
}

func (s *AvatarService) ListByUserID(
	ctx context.Context,
	userID string,
) ([]domain.Avatar, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf(
			"%w: user ID is empty",
			ErrInvalidInput,
		)
	}

	avatars, err := s.repository.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf(
			"list avatars for user %q: %w",
			userID,
			err,
		)
	}

	return avatars, nil
}

func (s *AvatarService) Delete(
	ctx context.Context,
	id string,
	userID string,
) (domain.Avatar, error) {
	if strings.TrimSpace(id) == "" {
		return domain.Avatar{}, fmt.Errorf(
			"%w: avatar ID is empty",
			ErrInvalidInput,
		)
	}

	if strings.TrimSpace(userID) == "" {
		return domain.Avatar{}, fmt.Errorf(
			"%w: user ID is empty",
			ErrInvalidInput,
		)
	}

	avatar, err := s.repository.GetByID(ctx, id)
	if err != nil {
		if postgres.IsNotFound(err) {
			return domain.Avatar{}, fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)
		}

		return domain.Avatar{}, fmt.Errorf(
			"get avatar %q: %w",
			id,
			err,
		)
	}

	if avatar.DeletedAt != nil {
		return domain.Avatar{}, fmt.Errorf(
			"%w: %q",
			ErrNotFound,
			id,
		)
	}

	if avatar.UserID != userID {
		return domain.Avatar{}, fmt.Errorf(
			"%w: avatar %q belongs to another user",
			ErrForbidden,
			id,
		)
	}

	avatar, err = s.repository.Delete(ctx, id, userID)
	if err != nil {
		if postgres.IsNotFound(err) {
			return domain.Avatar{}, fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)
		}

		return domain.Avatar{}, fmt.Errorf(
			"delete avatar %q: %w",
			id,
			err,
		)
	}

	s3Keys := make([]string, 0, 3)
	s3Keys = append(s3Keys, avatar.S3Key)

	for _, key := range avatar.ThumbnailS3Keys {
		if key != "" {
			s3Keys = append(s3Keys, key)
		}
	}

	messageID := uuid.NewString()

	event := events.AvatarDeleteEvent{
		MessageID: messageID,
		AvatarID:  avatar.ID,
		S3Keys:    s3Keys,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return domain.Avatar{}, fmt.Errorf(
			"marshal avatar delete event: %w",
			err,
		)
	}

	outboxEvent := domain.OutboxEvent{
		MessageID:  messageID,
		RoutingKey: rabbitmq.DeleteRoutingKey,
		Payload:    payload,
	}

	avatar, err = s.repository.DeleteWithOutbox(
		ctx,
		id,
		userID,
		outboxEvent,
	)
	if err != nil {
		if postgres.IsNotFound(err) {
			return domain.Avatar{}, fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)
		}

		return domain.Avatar{}, fmt.Errorf(
			"delete avatar %q: %w",
			id,
			err,
		)
	}

	return avatar, nil
}

func (s *AvatarService) GetContent(
	ctx context.Context,
	id string,
	size string,
) (AvatarContent, error) {
	if strings.TrimSpace(id) == "" {
		return AvatarContent{}, fmt.Errorf(
			"%w: avatar ID is required",
			ErrInvalidInput,
		)
	}

	avatar, err := s.repository.GetByID(ctx, id)
	if err != nil {
		if postgres.IsNotFound(err) {
			return AvatarContent{}, fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)
		}

		return AvatarContent{}, fmt.Errorf(
			"get avatar %q: %w",
			id,
			err,
		)
	}

	if avatar.DeletedAt != nil {
		return AvatarContent{}, fmt.Errorf(
			"%w: %q",
			ErrNotFound,
			id,
		)
	}

	key, err := avatarContentKey(avatar, size)
	if err != nil {
		return AvatarContent{}, err
	}

	body, contentType, contentLength, err := s.storage.GetObject(
		ctx,
		s.bucket,
		key,
	)
	if err != nil {
		return AvatarContent{}, fmt.Errorf(
			"get avatar object %q: %w",
			key,
			err,
		)
	}

	return AvatarContent{
		Body:        body,
		ContentType: contentType,
		Size:        contentLength,
	}, nil
}

func avatarContentKey(
	avatar domain.Avatar,
	size string,
) (string, error) {
	switch size {
	case "", "original":
		return avatar.S3Key, nil

	case "100":
		key := avatar.ThumbnailS3Keys["100x100"]
		if key == "" {
			return "", fmt.Errorf(
				"%w: 100x100 thumbnail is not ready",
				ErrNotFound,
			)
		}

		return key, nil

	case "300":
		key := avatar.ThumbnailS3Keys["300x300"]
		if key == "" {
			return "", fmt.Errorf(
				"%w: 300x300 thumbnail is not ready",
				ErrNotFound,
			)
		}

		return key, nil

	default:
		return "", fmt.Errorf(
			"%w: unsupported size %q",
			ErrInvalidInput,
			size,
		)
	}
}
