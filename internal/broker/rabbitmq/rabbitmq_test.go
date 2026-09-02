package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"go-avatar-service/internal/broker/events"
	"go-avatar-service/internal/config"
)

func TestClient(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, cfg.RabbitMQURL)
	if err != nil {
		t.Fatalf("create rabbitmq client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping rabbitmq: %v", err)
	}
}

func TestClientPublishJSON(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, cfg.RabbitMQURL)
	if err != nil {
		t.Fatalf("create rabbitmq client: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()

	queue, err := client.channel.QueueDeclare(
		"",
		false,
		true,
		true,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("declare test queue: %v", err)
	}

	if err := client.channel.QueueBind(
		queue.Name,
		UploadRoutingKey,
		ExchangeName,
		false,
		nil,
	); err != nil {
		t.Fatalf("bind test queue: %v", err)
	}

	delivery, err := client.channel.Consume(
		queue.Name,
		"rabbitmq-test",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("consume test queue: %v", err)
	}

	event := events.AvatarUploadEvent{
		MessageID: "test-message-id",
		AvatarID:  "test-avatar-id",
		UserID:    "test-user-id",
		S3Key:     "avatars/test/original.jpg",
	}

	if err := client.PublishJSON(
		ctx,
		UploadRoutingKey,
		event.MessageID,
		event,
	); err != nil {
		t.Fatalf("publish event: %v", err)
	}

	select {
	case message := <-delivery:
		if message.MessageId != event.MessageID {
			t.Fatalf(
				"unexpected message ID: got %q, want %q",
				message.MessageId,
				event.MessageID,
			)
		}

		if message.ContentType != "application/json" {
			t.Fatalf(
				"unexpected content type: got %q, want %q",
				message.ContentType,
				"application/json",
			)
		}

		var got events.AvatarUploadEvent
		if err := json.Unmarshal(message.Body, &got); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}

		if got.MessageID != event.MessageID {
			t.Errorf(
				"unexpected event message ID: got %q, want %q",
				got.MessageID,
				event.MessageID,
			)
		}

		if got.AvatarID != event.AvatarID {
			t.Errorf(
				"unexpected avatar ID: got %q, want %q",
				got.AvatarID,
				event.AvatarID,
			)
		}

		if got.UserID != event.UserID {
			t.Errorf(
				"unexpected user ID: got %q, want %q",
				got.UserID,
				event.UserID,
			)
		}

		if got.S3Key != event.S3Key {
			t.Errorf(
				"unexpected S3 key: got %q, want %q",
				got.S3Key,
				event.S3Key,
			)
		}

		expectedBody, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal expected event: %v", err)
		}

		if !bytes.Equal(message.Body, expectedBody) {
			t.Fatalf(
				"unexpected message body: got %s, want %s",
				message.Body,
				expectedBody,
			)
		}

	case <-ctx.Done():
		t.Fatal("timeout waiting for published event")
	}
}
