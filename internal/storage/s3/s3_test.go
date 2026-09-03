package s3

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"go-avatar-service/internal/config"
)

func TestClientObjectLifecycle(t *testing.T) {
	setTestEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, Config{
		Endpoint:  cfg.MinIOEndpoint,
		AccessKey: cfg.MinIOAccessKey,
		SecretKey: cfg.MinIOSecretKey,
		UseSSL:    cfg.MinIOUseSSL,
	})
	if err != nil {
		t.Fatalf("create s3 client: %v", err)
	}

	if err := client.EnsureBucket(ctx, cfg.MinIOBucket); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}

	if err := client.Ping(ctx, cfg.MinIOBucket); err != nil {
		t.Fatalf("ping s3: %v", err)
	}

	key := "integration-tests/test-object.txt"
	content := []byte("hello from gophprofile")

	if err := client.PutObject(
		ctx,
		cfg.MinIOBucket,
		key,
		bytes.NewReader(content),
		"text/plain",
		int64(len(content)),
	); err != nil {
		t.Fatalf("put object: %v", err)
	}

	body, contentType, contentLength, err := client.GetObject(
		ctx,
		cfg.MinIOBucket,
		key,
	)
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	defer func() {
		if err := body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}

	if !bytes.Equal(got, content) {
		t.Fatalf("expected content %q, got %q", content, got)
	}

	if contentType != "text/plain" {
		t.Fatalf(
			"expected content type %q, got %q",
			"text/plain",
			contentType,
		)
	}

	if contentLength != int64(len(content)) {
		t.Fatalf(
			"expected content length %d, got %d",
			len(content),
			contentLength,
		)
	}

	if err := client.DeleteObject(
		ctx,
		cfg.MinIOBucket,
		key,
	); err != nil {
		t.Fatalf("delete object: %v", err)
	}
}
