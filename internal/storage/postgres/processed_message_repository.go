package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ProcessedMessageRepository struct {
	pool *pgxpool.Pool
}

func NewProcessedMessageRepository(
	pool *pgxpool.Pool,
) *ProcessedMessageRepository {
	return &ProcessedMessageRepository{
		pool: pool,
	}
}

func (r *ProcessedMessageRepository) IsProcessed(
	ctx context.Context,
	messageID string,
) (bool, error) {
	var exists bool

	err := r.pool.QueryRow(
		ctx,
		`
		SELECT EXISTS (
			SELECT 1
			FROM processed_messages
			WHERE message_id = $1
		)
		`,
		messageID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf(
			"check processed message %q: %w",
			messageID,
			err,
		)
	}

	return exists, nil
}

func (r *ProcessedMessageRepository) MarkProcessed(
	ctx context.Context,
	messageID string,
) error {
	_, err := r.pool.Exec(
		ctx,
		`
		INSERT INTO processed_messages (message_id)
		VALUES ($1)
		ON CONFLICT (message_id) DO NOTHING
		`,
		messageID,
	)
	if err != nil {
		return fmt.Errorf(
			"mark message %q as processed: %w",
			messageID,
			err,
		)
	}

	return nil
}
