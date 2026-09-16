package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"go-avatar-service/internal/broker/events"
	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/domain"
	"go-avatar-service/internal/image"
	"go-avatar-service/internal/observability"
	"go-avatar-service/internal/storage/postgres"
	"go-avatar-service/internal/storage/s3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	metrics    *observability.Metrics
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
	metrics *observability.Metrics,
) *AvatarService {
	return &AvatarService{
		repository: repository,
		storage:    storage,
		bucket:     bucket,
		metrics:    metrics,
	}
}

func (s *AvatarService) Upload(
	ctx context.Context,
	input UploadInput,
) (domain.Avatar, error) {
	start := time.Now()
	status := "error"

	defer func() {
		s.metrics.UploadsTotal.WithLabelValues(status).Inc()
		s.metrics.UploadDuration.WithLabelValues(status).Observe(
			time.Since(start).Seconds(),
		)
	}()

	tracer := otel.Tracer("gophprofile/service")
	ctx, span := tracer.Start(ctx, "avatar.upload")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.user_id", input.UserID),
		attribute.String("avatar.file_name", input.FileName),
		attribute.Int("avatar.input_size_bytes", len(input.Content)),
	)

	if strings.TrimSpace(input.UserID) == "" {
		err := fmt.Errorf(
			"%w: user ID is required",
			ErrInvalidInput,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	img, info, err := image.DecodeAndValidate(input.Content)
	if err != nil {
		err = fmt.Errorf(
			"%w: validate image: %w",
			ErrInvalidInput,
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
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
		err = fmt.Errorf(
			"upload original image: %w",
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
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

	messageID := uuid.NewString()

	event := events.AvatarUploadEvent{
		MessageID: messageID,
		AvatarID:  avatar.ID,
		UserID:    avatar.UserID,
		S3Key:     avatar.S3Key,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		err = fmt.Errorf(
			"marshal avatar upload event: %w",
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
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
		err = fmt.Errorf(
			"create avatar: %w",
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	_ = img

	status = "success"

	span.SetAttributes(
		attribute.String("avatar.id", avatar.ID),
		attribute.String("avatar.mime_type", avatar.MimeType),
		attribute.Int64("avatar.size_bytes", avatar.SizeBytes),
	)

	span.SetStatus(codes.Ok, "")

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
	tracer := otel.Tracer("gophprofile/service")
	ctx, span := tracer.Start(ctx, "avatar.get")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.id", id),
	)

	avatar, err := s.repository.GetByID(ctx, id)
	if err != nil {
		if postgres.IsNotFound(err) {
			err = fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)

			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			return domain.Avatar{}, err
		}

		err = fmt.Errorf(
			"get avatar %q: %w",
			id,
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	if avatar.DeletedAt != nil {
		err := fmt.Errorf(
			"%w: %q",
			ErrNotFound,
			id,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	span.SetAttributes(
		attribute.String("avatar.user_id", avatar.UserID),
	)
	span.SetStatus(codes.Ok, "")

	return avatar, nil
}

func (s *AvatarService) GetCurrentByUserID(
	ctx context.Context,
	userID string,
) (domain.Avatar, error) {
	tracer := otel.Tracer("gophprofile/service")
	ctx, span := tracer.Start(ctx, "avatar.get_current")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.user_id", userID),
	)

	if strings.TrimSpace(userID) == "" {
		err := fmt.Errorf(
			"%w: user ID is empty",
			ErrInvalidInput,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	avatar, err := s.repository.GetCurrentByUserID(ctx, userID)
	if err != nil {
		if postgres.IsNotFound(err) {
			err = fmt.Errorf(
				"%w: current avatar for user %q",
				ErrNotFound,
				userID,
			)

			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			return domain.Avatar{}, err
		}

		err = fmt.Errorf(
			"get current avatar for user %q: %w",
			userID,
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	span.SetAttributes(
		attribute.String("avatar.id", avatar.ID),
	)
	span.SetStatus(codes.Ok, "")

	return avatar, nil
}

func (s *AvatarService) ListByUserID(
	ctx context.Context,
	userID string,
) ([]domain.Avatar, error) {
	tracer := otel.Tracer("gophprofile/service")
	ctx, span := tracer.Start(ctx, "avatar.list")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.user_id", userID),
	)

	if strings.TrimSpace(userID) == "" {
		err := fmt.Errorf(
			"%w: user ID is empty",
			ErrInvalidInput,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	avatars, err := s.repository.ListByUserID(ctx, userID)
	if err != nil {
		err = fmt.Errorf(
			"list avatars for user %q: %w",
			userID,
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, err
	}

	span.SetAttributes(
		attribute.Int("avatar.count", len(avatars)),
	)
	span.SetStatus(codes.Ok, "")

	return avatars, nil
}

func (s *AvatarService) Delete(
	ctx context.Context,
	id string,
	userID string,
) (domain.Avatar, error) {
	tracer := otel.Tracer("gophprofile/service")
	ctx, span := tracer.Start(ctx, "avatar.delete")
	defer span.End()

	span.SetAttributes(
		attribute.String("avatar.id", id),
		attribute.String("avatar.user_id", userID),
	)

	if strings.TrimSpace(id) == "" {
		err := fmt.Errorf(
			"%w: avatar ID is empty",
			ErrInvalidInput,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	if strings.TrimSpace(userID) == "" {
		err := fmt.Errorf(
			"%w: user ID is empty",
			ErrInvalidInput,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	avatar, err := s.repository.GetByID(ctx, id)
	if err != nil {
		if postgres.IsNotFound(err) {
			err = fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)

			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			return domain.Avatar{}, err
		}

		err = fmt.Errorf(
			"get avatar %q: %w",
			id,
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	if avatar.DeletedAt != nil {
		err := fmt.Errorf(
			"%w: %q",
			ErrNotFound,
			id,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	if avatar.UserID != userID {
		err := fmt.Errorf(
			"%w: avatar %q belongs to another user",
			ErrForbidden,
			id,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	avatar, err = s.repository.Delete(ctx, id, userID)
	if err != nil {
		if postgres.IsNotFound(err) {
			err = fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)

			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			return domain.Avatar{}, err
		}

		err = fmt.Errorf(
			"delete avatar %q: %w",
			id,
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
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
		err = fmt.Errorf(
			"marshal avatar delete event: %w",
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
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
			err = fmt.Errorf(
				"%w: %q",
				ErrNotFound,
				id,
			)

			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())

			return domain.Avatar{}, err
		}

		err = fmt.Errorf(
			"delete avatar %q: %w",
			id,
			err,
		)

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return domain.Avatar{}, err
	}

	span.SetStatus(codes.Ok, "")

	return avatar, nil
}

func (s *AvatarService) GetContent(
	ctx context.Context,
	id string,
	userID string,
) (AvatarContent, error) {
	if strings.TrimSpace(id) == "" {
		return AvatarContent{}, fmt.Errorf(
			"%w: avatar ID is empty",
			ErrInvalidInput,
		)
	}

	if strings.TrimSpace(userID) == "" {
		return AvatarContent{}, fmt.Errorf(
			"%w: user ID is empty",
			ErrInvalidInput,
		)
	}

	avatar, err := s.GetByID(ctx, id)
	if err != nil {
		return AvatarContent{}, err
	}

	if avatar.UserID != userID {
		return AvatarContent{}, fmt.Errorf(
			"%w: avatar does not belong to user",
			ErrForbidden,
		)
	}

	body, contentType, size, err := s.storage.GetObject(
		ctx,
		s.bucket,
		avatar.S3Key,
	)
	if err != nil {
		return AvatarContent{}, fmt.Errorf(
			"get avatar content: %w",
			err,
		)
	}

	return AvatarContent{
		Body:        body,
		ContentType: contentType,
		Size:        size,
	}, nil
}
