package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/config"
	"go-avatar-service/internal/observability"
	"go-avatar-service/internal/storage/postgres"
	"go-avatar-service/internal/storage/s3"
	"go-avatar-service/internal/worker"
)

const (
	shutdownTimeout           = 10 * time.Second
	metricsCollectionInterval = 10 * time.Second
)

func main() {
	cfg, err := config.LoadForService("gophprofile-worker")
	if err != nil {
		log.Fatalf("load config: %v", err)
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

	pool, err := postgres.NewPool(
		ctx,
		cfg.PostgresURL(),
	)
	if err != nil {
		logger.Error("connect to postgres", "error", err)
		stop()
		return
	}
	defer pool.Close()

	dbMetricsCtx, stopDBMetrics := context.WithCancel(ctx)
	defer stopDBMetrics()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		update := func() {
			metrics.SetDBPoolStats(pool.Stat())
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

	s3Client, err := s3.NewClient(
		ctx,
		s3.Config{
			Endpoint:  cfg.MinIOEndpoint,
			AccessKey: cfg.MinIOAccessKey,
			SecretKey: cfg.MinIOSecretKey,
			UseSSL:    cfg.MinIOUseSSL,
		},
	)
	if err != nil {
		logger.Error("create s3 client", "error", err)
		stop()
		return
	}

	if err := s3Client.EnsureBucket(
		ctx,
		cfg.MinIOBucket,
	); err != nil {
		logger.Error("ensure s3 bucket", "error", err)
		stop()
		return
	}

	brokerClient, err := rabbitmq.NewClient(
		ctx,
		cfg.RabbitMQURL,
	)
	if err != nil {
		logger.Error("connect to rabbitmq", "error", err)
		stop()
		return
	}
	defer func() {
		if err := brokerClient.Close(); err != nil {
			logger.Error("close broker", "error", err)
		}
	}()

	observability.StartRabbitMQQueueCollector(
		ctx,
		metrics,
		brokerClient,
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

	consumer, err := rabbitmq.NewConsumer(brokerClient)
	if err != nil {
		logger.Error("create rabbitmq consumer", "error", err)
		stop()
		return
	}
	defer func() {
		if err := consumer.Close(); err != nil {
			logger.Error("close consumer", "error", err)
		}
	}()

	repository := postgres.NewAvatarRepository(pool)
	processedMessages := postgres.NewProcessedMessageRepository(pool)

	processor := worker.NewAvatarProcessor(
		repository,
		s3Client,
	)

	deleter := worker.NewAvatarDeleter(s3Client)
	outboxRepository := postgres.NewOutboxRepository(pool)

	outboxPublisher := worker.NewOutboxPublisher(
		outboxRepository,
		brokerClient,
	)

	go outboxPublisher.Run(ctx)

	avatarWorker := worker.NewWorker(
		consumer,
		processor,
		deleter,
		brokerClient,
		processedMessages,
		cfg.MinIOBucket,
	)

	if err := avatarWorker.Run(ctx); err != nil {
		logger.Error("run worker", "error", err)
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics server shutdown error", "error", err)
	}

	logger.Info("worker stopped")
}
