package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"time"

	"go-avatar-service/internal/domain"
)

const (
	outboxBatchSize = 100
	outboxInterval  = time.Second
)

type OutboxRepository interface {
	GetPending(
		ctx context.Context,
		limit int,
	) ([]domain.OutboxEvent, error)

	MarkPublished(
		ctx context.Context,
		messageID string,
	) error
}

type OutboxBroker interface {
	PublishJSON(
		ctx context.Context,
		routingKey string,
		messageID string,
		payload any,
	) error
}

type OutboxPublisher struct {
	repository OutboxRepository
	broker     OutboxBroker
}

func NewOutboxPublisher(
	repository OutboxRepository,
	broker OutboxBroker,
) *OutboxPublisher {
	return &OutboxPublisher{
		repository: repository,
		broker:     broker,
	}
}

func (p *OutboxPublisher) Run(ctx context.Context) {
	p.publish(ctx)

	ticker := time.NewTicker(outboxInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			p.publish(ctx)
		}
	}
}

func (p *OutboxPublisher) publish(ctx context.Context) {
	events, err := p.repository.GetPending(
		ctx,
		outboxBatchSize,
	)
	if err != nil {
		slog.Error(
			"get pending outbox events",
			"error", err,
		)
		return
	}

	for _, event := range events {
		if err := p.publishEvent(ctx, event); err != nil {
			slog.Error(
				"publish outbox event",
				"message_id", event.MessageID,
				"error", err,
			)
			return
		}
	}
}

func (p *OutboxPublisher) publishEvent(
	ctx context.Context,
	event domain.OutboxEvent,
) error {
	if !json.Valid(event.Payload) {
		return fmt.Errorf("invalid event payload")
	}

	if err := p.broker.PublishJSON(
		ctx,
		event.RoutingKey,
		event.MessageID,
		json.RawMessage(event.Payload),
	); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	if err := p.repository.MarkPublished(
		ctx,
		event.MessageID,
	); err != nil {
		return fmt.Errorf("mark event as published: %w", err)
	}

	return nil
}
