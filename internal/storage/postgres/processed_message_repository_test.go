package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProcessedMessageRepository(t *testing.T) {
	pool := testPool(t)
	repository := NewProcessedMessageRepository(pool)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	messageID := uuid.NewString()

	processed, err := repository.IsProcessed(ctx, messageID)
	if err != nil {
		t.Fatalf("check message: %v", err)
	}

	if processed {
		t.Fatal("message should not be processed")
	}

	if err := repository.MarkProcessed(ctx, messageID); err != nil {
		t.Fatalf("mark message: %v", err)
	}

	processed, err = repository.IsProcessed(ctx, messageID)
	if err != nil {
		t.Fatalf("check processed message: %v", err)
	}

	if !processed {
		t.Fatal("message should be processed")
	}

	if err := repository.MarkProcessed(ctx, messageID); err != nil {
		t.Fatalf("mark duplicate message: %v", err)
	}
}
