package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"go-avatar-service/internal/domain"
)

func TestOutboxRepositoryCreateAndGetPending(t *testing.T) {
	pool := testPool(t)
	avatarRepository := NewAvatarRepository(pool)
	outboxRepository := NewOutboxRepository(pool)

	ctx := context.Background()
	avatar := testAvatar("outbox-test-user-" + uuid.NewString())

	event := domain.OutboxEvent{
		MessageID:  uuid.NewString(),
		RoutingKey: "avatar.uploaded",
		Payload:    []byte(`{"message_id":"test"}`),
	}

	if err := avatarRepository.CreateWithOutbox(
		ctx,
		avatar,
		event,
	); err != nil {
		t.Fatalf("create avatar with outbox: %v", err)
	}

	events, err := outboxRepository.GetPending(ctx, 10)
	if err != nil {
		t.Fatalf("get pending events: %v", err)
	}

	var found *domain.OutboxEvent

	for i := range events {
		if events[i].MessageID == event.MessageID {
			found = &events[i]
			break
		}
	}

	if found == nil {
		t.Fatalf("outbox event %q not found", event.MessageID)
	}

	if found.RoutingKey != event.RoutingKey {
		t.Fatalf(
			"routing key = %q, want %q",
			found.RoutingKey,
			event.RoutingKey,
		)
	}

	if !json.Valid(found.Payload) {
		t.Fatalf("stored payload is not valid JSON: %q", found.Payload)
	}

	var gotPayload any
	var wantPayload any

	if err := json.Unmarshal(found.Payload, &gotPayload); err != nil {
		t.Fatalf("unmarshal stored payload: %v", err)
	}

	if err := json.Unmarshal(event.Payload, &wantPayload); err != nil {
		t.Fatalf("unmarshal expected payload: %v", err)
	}

	if !reflect.DeepEqual(gotPayload, wantPayload) {
		t.Fatalf(
			"payload = %q, want %q",
			found.Payload,
			event.Payload,
		)
	}

	if err := outboxRepository.MarkPublished(
		ctx,
		event.MessageID,
	); err != nil {
		t.Fatalf("mark event as published: %v", err)
	}

	events, err = outboxRepository.GetPending(ctx, 10)
	if err != nil {
		t.Fatalf("get pending events after publish: %v", err)
	}

	for _, got := range events {
		if got.MessageID == event.MessageID {
			t.Fatalf(
				"published event %q is still pending",
				event.MessageID,
			)
		}
	}
}

func TestAvatarRepositoryCreateWithOutboxIsAtomic(t *testing.T) {
	pool := testPool(t)
	avatarRepository := NewAvatarRepository(pool)

	ctx := context.Background()
	avatar := testAvatar("outbox-atomicity-user-" + uuid.NewString())

	event := domain.OutboxEvent{
		MessageID:  uuid.NewString(),
		RoutingKey: "avatar.uploaded",
		Payload:    []byte(`invalid json`),
	}

	err := avatarRepository.CreateWithOutbox(
		ctx,
		avatar,
		event,
	)
	if err == nil {
		t.Fatal("expected create to fail")
	}

	_, err = avatarRepository.GetByID(ctx, avatar.ID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf(
			"expected avatar insert to be rolled back, got %v",
			err,
		)
	}
}

func TestAvatarRepositoryDeleteWithOutboxIsAtomic(t *testing.T) {
	pool := testPool(t)
	avatarRepository := NewAvatarRepository(pool)

	ctx := context.Background()
	avatar := testAvatar("outbox-delete-atomicity-user-" + uuid.NewString())

	if err := avatarRepository.Create(ctx, avatar); err != nil {
		t.Fatalf("create avatar: %v", err)
	}

	event := domain.OutboxEvent{
		MessageID:  uuid.NewString(),
		RoutingKey: "avatar.deleted",
		Payload:    []byte(`invalid json`),
	}

	_, err := avatarRepository.DeleteWithOutbox(
		ctx,
		avatar.ID,
		avatar.UserID,
		event,
	)
	if err == nil {
		t.Fatal("expected delete to fail")
	}

	got, err := avatarRepository.GetByID(ctx, avatar.ID)
	if err != nil {
		t.Fatalf("get avatar: %v", err)
	}

	if got.DeletedAt != nil {
		t.Fatal("expected avatar deletion to be rolled back")
	}

	if !got.IsActive {
		t.Fatal("expected avatar to remain active")
	}
}
