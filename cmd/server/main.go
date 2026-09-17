package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/config"
	"go-avatar-service/internal/health"
	httpHandler "go-avatar-service/internal/http"
	"go-avatar-service/internal/observability"
	"go-avatar-service/internal/service"
	"go-avatar-service/internal/storage/postgres"
	"go-avatar-service/internal/storage/s3"
	"go-avatar-service/web"
)

const (
	shutdownTimeout           = 10 * time.Second
	metricsCollectionInterval = 10 * time.Second
)

func main() {
	cfg, err := config.LoadForService("gophprofile-server")
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logger := observability.NewLogger(
		cfg.OTelServiceName,
		cfg.LogLevel,
	)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	shutdownTracing, err := observability.InitTracing(
		ctx,
		cfg.OTelServiceName,
		cfg.OTelExporterEndpoint,
	)
	if err != nil {
		logger.Error("initialize tracing", "error", err)
		os.Exit(1)
	}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancel()

		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Error("shutdown tracing", "error", err)
		}
	}()

	metricsRegistry := prometheus.NewRegistry()
	metrics := observability.NewMetrics(metricsRegistry)

	metricsServer := &http.Server{
		Addr: cfg.MetricsAddress(),
		Handler: promhttp.HandlerFor(
			metricsRegistry,
			promhttp.HandlerOpts{},
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		logger.Info(
			"metrics server listening",
			"address", cfg.MetricsAddress(),
		)

		if err := metricsServer.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server error", "error", err)
			stop()
		}
	}()

	db, err := postgres.NewPool(ctx, cfg.PostgresURL())
	if err != nil {
		logger.Error("create postgres pool", "error", err)
		stop()
		return
	}
	defer db.Close()

	if err := postgres.RunEmbeddedMigrations(cfg.PostgresURL()); err != nil {
		logger.Error("run database migrations", "error", err)
		stop()
		return
	}

	dbMetricsCtx, stopDBMetrics := context.WithCancel(ctx)
	defer stopDBMetrics()

	go func() {
		ticker := time.NewTicker(metricsCollectionInterval)
		defer ticker.Stop()

		update := func() {
			metrics.SetDBPoolStats(db.Stat())
		}

		update()

		for {
			select {
			case <-ticker.C:
				update()
			case <-dbMetricsCtx.Done():
				return
			}
		}
	}()

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
		logger.Error("create S3 client", "error", err)
		stop()
		return
	}

	if err := storage.EnsureBucket(ctx, cfg.MinIOBucket); err != nil {
		logger.Error("ensure S3 bucket", "error", err)
		stop()
		return
	}

	broker, err := rabbitmq.NewClient(ctx, cfg.RabbitMQURL)
	if err != nil {
		logger.Error("connect to RabbitMQ", "error", err)
		stop()
		return
	}

	defer func() {
		if err := broker.Close(); err != nil {
			logger.Error("close broker", "error", err)
		}
	}()

	repository := postgres.NewAvatarRepository(db)

	observability.StartStorageUsageCollector(
		ctx,
		metrics,
		repository,
		metricsCollectionInterval,
	)

	observability.StartRabbitMQQueueCollector(
		ctx,
		metrics,
		broker,
		[]string{
			rabbitmq.ProcessingQueue,
			rabbitmq.DeletionQueue,
			rabbitmq.UploadRetry5sQueue,
			rabbitmq.UploadRetry10sQueue,
			rabbitmq.UploadRetry20sQueue,
			rabbitmq.DeleteRetry5sQueue,
			rabbitmq.DeleteRetry10sQueue,
			rabbitmq.DeleteRetry20sQueue,
			rabbitmq.DeadLetterQueue,
		},
		metricsCollectionInterval,
	)

	avatarService := service.NewAvatarService(
		repository,
		storage,
		cfg.MinIOBucket,
		metrics,
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
	router.Use(httpHandler.MetricsMiddleware(metrics))

	handler.RegisterRoutes(router)
	router.Get("/health", healthHandler.Handle)

	staticFS, err := fs.Sub(web.StaticFiles, "static")
	if err != nil {
		logger.Error("create web filesystem", "error", err)
		stop()
		return
	}

	webHandler := httpHandler.NewWebHandler(staticFS)
	router.Handle("/", webHandler)

	tracedHandler := otelhttp.NewHandler(
		router,
		"http.server",
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddress(),
		Handler:           tracedHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)

	go func() {
		logger.Info(
			"HTTP server listening",
			"address", cfg.HTTPAddress(),
		)

		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	select {
	case err := <-serverErrors:
		logger.Error("HTTP server error", "error", err)
		stop()

	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics server shutdown error", "error", err)
	}

	logger.Info("HTTP server stopped")
}
