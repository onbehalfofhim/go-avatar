package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/config"
	"go-avatar-service/internal/storage/postgres"
	"go-avatar-service/internal/storage/s3"
	"go-avatar-service/internal/worker"
)

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

	pool, err := postgres.NewPool(
		ctx,
		cfg.PostgresURL(),
	)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()

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
		log.Fatalf("create s3 client: %v", err)
	}

	if err := s3Client.EnsureBucket(
		ctx,
		cfg.MinIOBucket,
	); err != nil {
		log.Fatalf("ensure s3 bucket: %v", err)
	}

	brokerClient, err := rabbitmq.NewClient(
		ctx,
		cfg.RabbitMQURL,
	)
	if err != nil {
		log.Fatalf("connect to rabbitmq: %v", err)
	}
	defer func() {
		if err := brokerClient.Close(); err != nil {
			log.Printf("close broker: %v", err)
		}
	}()

	consumer := rabbitmq.NewConsumer(brokerClient)
	repository := postgres.NewAvatarRepository(pool)
	processedMessages := postgres.NewProcessedMessageRepository(pool)

	processor := worker.NewAvatarProcessor(
		repository,
		s3Client,
	)

	deleter := worker.NewAvatarDeleter(s3Client)

	avatarWorker := worker.NewWorker(
		consumer,
		processor,
		deleter,
		brokerClient,
		processedMessages,
		cfg.MinIOBucket,
	)

	if err := avatarWorker.Run(ctx); err != nil {
		log.Fatalf("run worker: %v", err)
	}

	log.Println("Worker stopped")
}
