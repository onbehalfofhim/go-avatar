package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"go-avatar-service/internal/broker/events"
	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/domain"
	"go-avatar-service/internal/image"
	"go-avatar-service/internal/storage/postgres"
	"go-avatar-service/internal/storage/s3"
)

const (
	thumbnail100Key = "100x100"
	thumbnail300Key = "300x300"
)

type AvatarProcessor struct {
	repository *postgres.AvatarRepository
	storage    *s3.Client
}

func NewAvatarProcessor(
	repository *postgres.AvatarRepository,
	storage *s3.Client,
) *AvatarProcessor {
	return &AvatarProcessor{
		repository: repository,
		storage:    storage,
	}
}

func (p *AvatarProcessor) ProcessUpload(
	ctx context.Context,
	message rabbitmq.Message,
	bucket string,
) error {
	var event events.AvatarUploadEvent

	if err := json.Unmarshal(message.Body, &event); err != nil {
		return fmt.Errorf("unmarshal avatar upload event: %w", err)
	}

	if event.AvatarID == "" {
		return fmt.Errorf("avatar upload event: avatar_id is empty")
	}

	if event.S3Key == "" {
		return fmt.Errorf("avatar upload event: s3_key is empty")
	}

	if err := p.repository.UpdateProcessingStatus(
		ctx,
		event.AvatarID,
		domain.ProcessingStatusProcessing,
	); err != nil {
		return fmt.Errorf("set processing status: %w", err)
	}

	thumbnailKeys, err := p.createThumbnails(
		ctx,
		bucket,
		event,
	)
	if err != nil {
		return fmt.Errorf("create thumbnails: %w", err)
	}

	if err := p.repository.UpdateThumbnailS3Keys(
		ctx,
		event.AvatarID,
		thumbnailKeys,
	); err != nil {
		return fmt.Errorf("save thumbnail keys: %w", err)
	}

	if err := p.repository.UpdateProcessingStatus(
		ctx,
		event.AvatarID,
		domain.ProcessingStatusCompleted,
	); err != nil {
		return fmt.Errorf("set completed status: %w", err)
	}

	return nil
}

func (p *AvatarProcessor) createThumbnails(
	ctx context.Context,
	bucket string,
	event events.AvatarUploadEvent,
) (map[string]string, error) {
	body, _, _, err := p.storage.GetObject(
		ctx,
		bucket,
		event.S3Key,
	)
	if err != nil {
		return nil, fmt.Errorf("get original image: %w", err)
	}
	defer func() {
		if err := body.Close(); err != nil {
			log.Printf("close S3 response body: %v", err)
		}
	}()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read original image: %w", err)
	}

	img, _, err := image.DecodeAndValidate(data)
	if err != nil {
		return nil, fmt.Errorf("decode original image: %w", err)
	}

	keys := map[string]string{
		thumbnail100Key: fmt.Sprintf(
			"avatars/%s/100x100.jpg",
			event.AvatarID,
		),
		thumbnail300Key: fmt.Sprintf(
			"avatars/%s/300x300.jpg",
			event.AvatarID,
		),
	}

	thumbnail100, err := image.Thumbnail(
		img,
		image.Thumbnail100,
	)
	if err != nil {
		return nil, fmt.Errorf("create 100x100 thumbnail: %w", err)
	}

	if err := p.storage.PutObject(
		ctx,
		bucket,
		keys[thumbnail100Key],
		bytes.NewReader(thumbnail100),
		"image/jpeg",
		int64(len(thumbnail100)),
	); err != nil {
		return nil, fmt.Errorf("upload 100x100 thumbnail: %w", err)
	}

	thumbnail300, err := image.Thumbnail(
		img,
		image.Thumbnail300,
	)
	if err != nil {
		return nil, fmt.Errorf("create 300x300 thumbnail: %w", err)
	}

	if err := p.storage.PutObject(
		ctx,
		bucket,
		keys[thumbnail300Key],
		bytes.NewReader(thumbnail300),
		"image/jpeg",
		int64(len(thumbnail300)),
	); err != nil {
		return nil, fmt.Errorf("upload 300x300 thumbnail: %w", err)
	}

	return keys, nil
}
