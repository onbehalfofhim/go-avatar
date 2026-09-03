package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"go-avatar-service/internal/broker/events"
	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/storage/s3"
)

type AvatarDeleter struct {
	storage *s3.Client
}

func NewAvatarDeleter(storage *s3.Client) *AvatarDeleter {
	return &AvatarDeleter{
		storage: storage,
	}
}

func (d *AvatarDeleter) ProcessDelete(
	ctx context.Context,
	message rabbitmq.Message,
	bucket string,
) error {
	var event events.AvatarDeleteEvent

	if err := json.Unmarshal(message.Body, &event); err != nil {
		return fmt.Errorf("unmarshal avatar delete event: %w", err)
	}

	if event.AvatarID == "" {
		return fmt.Errorf("avatar delete event: avatar_id is empty")
	}

	if len(event.S3Keys) == 0 {
		return fmt.Errorf("avatar delete event: s3_keys is empty")
	}

	for _, key := range event.S3Keys {
		if key == "" {
			return fmt.Errorf(
				"avatar delete event: s3 key is empty",
			)
		}

		if err := d.storage.DeleteObject(
			ctx,
			bucket,
			key,
		); err != nil {
			return fmt.Errorf(
				"delete object %q: %w",
				key,
				err,
			)
		}
	}

	return nil
}
