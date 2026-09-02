package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/config"
	"go-avatar-service/internal/health"
	httpHandler "go-avatar-service/internal/http"
	"go-avatar-service/internal/service"
	"go-avatar-service/internal/storage/postgres"
	"go-avatar-service/internal/storage/s3"
	"go-avatar-service/web"
)

const shutdownTimeout = 10 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	db, err := postgres.NewPool(ctx, cfg.PostgresURL())
	if err != nil {
		log.Fatalf("create postgres pool: %v", err)
	}
	defer db.Close()

	if err := postgres.RunEmbeddedMigrations(cfg.PostgresURL()); err != nil {
		log.Fatalf("run database migrations: %v", err)
	}

	storage, err := s3.NewClient(
		ctx,
		s3.Config{
			Endpoint:  cfg.MinIOEndpoint,
			AccessKey: cfg.MinIOAccessKey,
			SecretKey: cfg.MinIOSecretKey,
			UseSSL:    cfg.MinIOUseSSL,
		},
	)
	if err != nil {
		log.Fatalf("create S3 client: %v", err)
	}

	if err := storage.EnsureBucket(ctx, cfg.MinIOBucket); err != nil {
		log.Fatalf("ensure S3 bucket: %v", err)
	}

	broker, err := rabbitmq.NewClient(ctx, cfg.RabbitMQURL)
	if err != nil {
		log.Fatalf("connect to RabbitMQ: %v", err)
	}
	defer func() {
		if err := broker.Close(); err != nil {
			log.Printf("close broker: %v", err)
		}
	}()

	repository := postgres.NewAvatarRepository(db)

	avatarService := service.NewAvatarService(
		repository,
		storage,
		broker,
		cfg.MinIOBucket,
	)

	handler := httpHandler.NewAvatarHandler(avatarService)

	healthChecker := health.NewChecker()

	healthChecker.Add("postgres", db.Ping)

	healthChecker.Add("s3", func(ctx context.Context) error {
		return storage.Ping(ctx, cfg.MinIOBucket)
	})

	healthChecker.Add("rabbitmq", broker.Ping)

	healthHandler := httpHandler.NewHealthHandler(healthChecker)

	router := chi.NewRouter()

	handler.RegisterRoutes(router)
	router.Get("/health", healthHandler.Handle)

	staticFS, err := fs.Sub(web.StaticFiles, "static")
	if err != nil {
		log.Fatalf("create web filesystem: %v", err)
	}

	webHandler := httpHandler.NewWebHandler(staticFS)

	router.Handle("/", webHandler)

	server := &http.Server{
		Addr:              cfg.HTTPAddress(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)

	go func() {
		log.Printf("HTTP server listening on %s", cfg.HTTPAddress())

		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		log.Fatalf("HTTP server error: %v", err)

	case <-ctx.Done():
		log.Println("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("HTTP server stopped")
}
