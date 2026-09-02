package postgres

import (
	"context"
	"testing"
	"time"

	"go-avatar-service/internal/config"
)

func TestNewPool(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, cfg.PostgresURL())
	if err != nil {
		t.Fatalf("create postgres pool: %v", err)
	}
	defer pool.Close()
}
