package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-avatar-service/internal/domain"
)

type OutboxRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{
		pool: pool,
	}
}

func (r *OutboxRepository) GetPending(
	ctx context.Context,
	limit int,
) ([]domain.OutboxEvent, error) {
	rows, err := r.pool.Query(
		ctx,
		`
		SELECT
			message_id,
			routing_key,
			payload
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1
		`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get pending outbox events: %w", err)
	}
	defer rows.Close()

	events := make([]domain.OutboxEvent, 0, limit)

	for rows.Next() {
		var event domain.OutboxEvent

		if err := rows.Scan(
			&event.MessageID,
			&event.RoutingKey,
			&event.Payload,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox events: %w", err)
	}

	return events, nil
}

func (r *OutboxRepository) MarkPublished(
	ctx context.Context,
	messageID string,
) error {
	_, err := r.pool.Exec(
		ctx,
		`
		UPDATE outbox_events
		SET published_at = NOW()
		WHERE message_id = $1
			AND published_at IS NULL
		`,
		messageID,
	)
	if err != nil {
		return fmt.Errorf(
			"mark outbox event %q as published: %w",
			messageID,
			err,
		)
	}

	return nil
}
