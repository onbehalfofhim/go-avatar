package worker

import (
	"context"
	"errors"
	"testing"

	"go-avatar-service/internal/domain"
)

type outboxRepositoryMock struct {
	events    []domain.OutboxEvent
	published []string
}

func (m *outboxRepositoryMock) GetPending(
	ctx context.Context,
	limit int,
) ([]domain.OutboxEvent, error) {
	return m.events, nil
}

func (m *outboxRepositoryMock) MarkPublished(
	ctx context.Context,
	messageID string,
) error {
	m.published = append(m.published, messageID)
	return nil
}

type outboxBrokerMock struct {
	messageIDs []string
}

func (m *outboxBrokerMock) PublishJSON(
	ctx context.Context,
	routingKey string,
	messageID string,
	payload any,
) error {
	m.messageIDs = append(m.messageIDs, messageID)
	return nil
}

func TestOutboxPublisherPublish(t *testing.T) {
	repository := &outboxRepositoryMock{
		events: []domain.OutboxEvent{
			{
				MessageID:  "message-1",
				RoutingKey: "avatar.uploaded",
				Payload:    []byte(`{"message_id":"message-1"}`),
			},
		},
	}
	broker := &outboxBrokerMock{}

	publisher := NewOutboxPublisher(
		repository,
		broker,
	)

	publisher.publish(context.Background())

	if len(broker.messageIDs) != 1 {
		t.Fatalf(
			"published messages = %d, want 1",
			len(broker.messageIDs),
		)
	}

	if broker.messageIDs[0] != "message-1" {
		t.Fatalf(
			"message ID = %q, want %q",
			broker.messageIDs[0],
			"message-1",
		)
	}

	if len(repository.published) != 1 {
		t.Fatalf(
			"marked messages = %d, want 1",
			len(repository.published),
		)
	}

	if repository.published[0] != "message-1" {
		t.Fatalf(
			"marked message ID = %q, want %q",
			repository.published[0],
			"message-1",
		)
	}
}

func TestOutboxPublisherDoesNotMarkFailedEvent(t *testing.T) {
	repository := &outboxRepositoryMock{
		events: []domain.OutboxEvent{
			{
				MessageID:  "message-1",
				RoutingKey: "avatar.uploaded",
				Payload:    []byte(`invalid json`),
			},
		},
	}
	broker := &outboxBrokerMock{}

	publisher := NewOutboxPublisher(
		repository,
		broker,
	)

	publisher.publish(context.Background())

	if len(repository.published) != 0 {
		t.Fatalf(
			"marked messages = %d, want 0",
			len(repository.published),
		)
	}
}

func TestOutboxPublisherReturnsToPendingOnMarkError(t *testing.T) {
	repository := &failingMarkOutboxRepository{
		events: []domain.OutboxEvent{
			{
				MessageID:  "message-1",
				RoutingKey: "avatar.uploaded",
				Payload:    []byte(`{"message_id":"message-1"}`),
			},
		},
	}
	broker := &outboxBrokerMock{}

	publisher := NewOutboxPublisher(
		repository,
		broker,
	)

	publisher.publish(context.Background())

	if !repository.markCalled {
		t.Fatal("expected MarkPublished to be called")
	}

	if !errors.Is(repository.markErr, repository.markErr) {
		t.Fatal("expected mark error")
	}
}

type failingMarkOutboxRepository struct {
	events     []domain.OutboxEvent
	markCalled bool
	markErr    error
}

func (m *failingMarkOutboxRepository) GetPending(
	ctx context.Context,
	limit int,
) ([]domain.OutboxEvent, error) {
	return m.events, nil
}

func (m *failingMarkOutboxRepository) MarkPublished(
	ctx context.Context,
	messageID string,
) error {
	m.markCalled = true
	m.markErr = errors.New("mark failed")
	return m.markErr
}
