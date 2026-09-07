package rabbitmq

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go-avatar-service/internal/broker/events"
	"go-avatar-service/internal/config"
)

func TestConsumerReceivesMessage(t *testing.T) {
	setTestEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
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

	consumer, err := NewConsumer(client)
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	defer func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("close consumer: %v", err)
		}
	}()

	messages, err := consumer.Consume(
		ctx,
		ProcessingQueue,
		"consumer-test",
	)
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}

	event := events.AvatarUploadEvent{
		MessageID: "consumer-test-message",
		AvatarID:  "consumer-test-avatar",
		UserID:    "consumer-test-user",
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
	case message := <-messages:
		if message.ID != event.MessageID {
			t.Fatalf(
				"expected message id %q, got %q",
				event.MessageID,
				message.ID,
			)
		}

		if message.ContentType != "application/json" {
			t.Fatalf(
				"expected content type %q, got %q",
				"application/json",
				message.ContentType,
			)
		}

		if err := message.Ack(); err != nil {
			t.Fatalf("ack message: %v", err)
		}

	case <-ctx.Done():
		t.Fatal("timeout waiting for message")
	}
}

func TestClientPublishRetry(t *testing.T) {
	setTestEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
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

	consumer, err := NewConsumer(client)
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	defer func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("close consumer: %v", err)
		}
	}()

	messages, err := consumer.Consume(
		ctx,
		ProcessingQueue,
		"retry-test",
	)
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}

	message := Message{
		ID:          "retry-test-message",
		ContentType: "application/json",
		Body:        []byte(`{"test":"retry"}`),
	}

	retryQueue, ok := UploadRetryQueue(1)
	if !ok {
		t.Fatal("retry queue for attempt 1 not found")
	}

	if err := client.PublishRetry(
		ctx,
		retryQueue,
		message,
	); err != nil {
		t.Fatalf("publish retry message: %v", err)
	}

	select {
	case received := <-messages:
		if received.ID != message.ID {
			t.Fatalf(
				"expected message id %q, got %q",
				message.ID,
				received.ID,
			)
		}

		if string(received.Body) != string(message.Body) {
			t.Fatalf("unexpected retry message body")
		}

		if err := received.Ack(); err != nil {
			t.Fatalf("ack retry message: %v", err)
		}

	case <-ctx.Done():
		t.Fatal("timeout waiting for retry message")
	}
}

func TestUploadRetryQueue(t *testing.T) {
	tests := []struct {
		attempt int
		want    string
		ok      bool
	}{
		{
			attempt: 1,
			want:    UploadRetry5sQueue,
			ok:      true,
		},
		{
			attempt: 2,
			want:    UploadRetry10sQueue,
			ok:      true,
		},
		{
			attempt: 3,
			want:    UploadRetry20sQueue,
			ok:      true,
		},
		{
			attempt: 4,
			want:    "",
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			got, ok := UploadRetryQueue(tt.attempt)

			if got != tt.want {
				t.Fatalf("queue = %q, want %q", got, tt.want)
			}

			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
		})
	}
}

func TestDeleteRetryQueue(t *testing.T) {
	tests := []struct {
		attempt int
		want    string
		ok      bool
	}{
		{
			attempt: 1,
			want:    DeleteRetry5sQueue,
			ok:      true,
		},
		{
			attempt: 2,
			want:    DeleteRetry10sQueue,
			ok:      true,
		},
		{
			attempt: 3,
			want:    DeleteRetry20sQueue,
			ok:      true,
		},
		{
			attempt: 4,
			want:    "",
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			got, ok := DeleteRetryQueue(tt.attempt)

			if got != tt.want {
				t.Fatalf("queue = %q, want %q", got, tt.want)
			}

			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
		})
	}
}
