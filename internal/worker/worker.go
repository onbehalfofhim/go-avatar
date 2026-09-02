package worker

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"go-avatar-service/internal/broker/rabbitmq"
	"go-avatar-service/internal/storage/postgres"
)

const retryAttemptHeader = "x-retry-attempt"

type Worker struct {
	consumer          *rabbitmq.Consumer
	processor         *AvatarProcessor
	deleter           *AvatarDeleter
	broker            *rabbitmq.Client
	processedMessages *postgres.ProcessedMessageRepository
	bucket            string
}

func NewWorker(
	consumer *rabbitmq.Consumer,
	processor *AvatarProcessor,
	deleter *AvatarDeleter,
	broker *rabbitmq.Client,
	processedMessages *postgres.ProcessedMessageRepository,
	bucket string,
) *Worker {
	return &Worker{
		consumer:          consumer,
		processor:         processor,
		deleter:           deleter,
		broker:            broker,
		processedMessages: processedMessages,
		bucket:            bucket,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	uploadMessages, err := w.consumer.Consume(
		ctx,
		rabbitmq.ProcessingQueue,
		"avatar-upload-worker",
	)
	if err != nil {
		return fmt.Errorf("start avatar upload consumer: %w", err)
	}

	deleteMessages, err := w.consumer.Consume(
		ctx,
		rabbitmq.DeletionQueue,
		"avatar-delete-worker",
	)
	if err != nil {
		return fmt.Errorf("start avatar delete consumer: %w", err)
	}

	log.Println("Avatar worker started")

	for {
		select {
		case <-ctx.Done():
			return nil

		case message, ok := <-uploadMessages:
			if !ok {
				uploadMessages = nil
				continue
			}

			if err := w.processUploadMessage(ctx, message); err != nil {
				log.Printf(
					"process upload message %q: %v",
					message.ID,
					err,
				)
			}

		case message, ok := <-deleteMessages:
			if !ok {
				deleteMessages = nil
				continue
			}

			if err := w.processDeleteMessage(ctx, message); err != nil {
				log.Printf(
					"process delete message %q: %v",
					message.ID,
					err,
				)
			}
		}

		if uploadMessages == nil && deleteMessages == nil {
			return nil
		}
	}
}

func (w *Worker) processUploadMessage(
	ctx context.Context,
	message rabbitmq.Message,
) error {
	processed, err := w.checkProcessed(ctx, message)
	if err != nil {
		return w.retryOrReject(ctx, message, err)
	}

	if processed {
		if err := message.Ack(); err != nil {
			return fmt.Errorf(
				"ack already processed upload message: %w",
				err,
			)
		}

		return nil
	}

	if err := w.processor.ProcessUpload(
		ctx,
		message,
		w.bucket,
	); err != nil {
		return w.retryOrReject(ctx, message, err)
	}

	if err := w.processedMessages.MarkProcessed(
		ctx,
		message.ID,
	); err != nil {
		return w.retryOrReject(
			ctx,
			message,
			fmt.Errorf(
				"mark upload message as processed: %w",
				err,
			),
		)
	}

	if err := message.Ack(); err != nil {
		return fmt.Errorf("ack upload message: %w", err)
	}

	return nil
}

func (w *Worker) processDeleteMessage(
	ctx context.Context,
	message rabbitmq.Message,
) error {
	processed, err := w.checkProcessed(ctx, message)
	if err != nil {
		return w.retryOrRejectDelete(ctx, message, err)
	}

	if processed {
		if err := message.Ack(); err != nil {
			return fmt.Errorf(
				"ack already processed delete message: %w",
				err,
			)
		}

		return nil
	}

	if err := w.deleter.ProcessDelete(
		ctx,
		message,
		w.bucket,
	); err != nil {
		return w.retryOrRejectDelete(
			ctx,
			message,
			err,
		)
	}

	if err := w.processedMessages.MarkProcessed(
		ctx,
		message.ID,
	); err != nil {
		return w.retryOrRejectDelete(
			ctx,
			message,
			fmt.Errorf(
				"mark delete message as processed: %w",
				err,
			),
		)
	}

	if err := message.Ack(); err != nil {
		return fmt.Errorf("ack delete message: %w", err)
	}

	return nil
}

func (w *Worker) checkProcessed(
	ctx context.Context,
	message rabbitmq.Message,
) (bool, error) {
	if message.ID == "" {
		return false, fmt.Errorf("message ID is empty")
	}

	processed, err := w.processedMessages.IsProcessed(
		ctx,
		message.ID,
	)
	if err != nil {
		return false, fmt.Errorf(
			"check message %q idempotency: %w",
			message.ID,
			err,
		)
	}

	return processed, nil
}

func retryAttempt(message rabbitmq.Message) int {
	if message.Headers == nil {
		return 0
	}

	value, ok := message.Headers[retryAttemptHeader]
	if !ok {
		return 0
	}

	switch value := value.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case string:
		attempt, err := strconv.Atoi(value)
		if err != nil {
			return 0
		}
		return attempt
	default:
		return 0
	}
}

func (w *Worker) retryOrReject(
	ctx context.Context,
	message rabbitmq.Message,
	processingErr error,
) error {
	attempt := retryAttempt(message)

	retryQueue, ok := rabbitmq.UploadRetryQueue(attempt + 1)
	if !ok {
		if err := message.Reject(false); err != nil {
			return fmt.Errorf(
				"processing failed: %w; reject message: %v",
				processingErr,
				err,
			)
		}

		return fmt.Errorf(
			"processing failed after %d retries: %w",
			attempt,
			processingErr,
		)
	}

	if message.Headers == nil {
		message.Headers = make(map[string]any)
	}
	message.Headers[retryAttemptHeader] = attempt + 1

	if err := w.broker.PublishRetry(
		ctx,
		retryQueue,
		message,
	); err != nil {
		if rejectErr := message.Reject(false); rejectErr != nil {
			return fmt.Errorf(
				"processing failed: %w; publish retry: %v; reject message: %v",
				processingErr,
				err,
				rejectErr,
			)
		}

		return fmt.Errorf(
			"processing failed: %w; publish retry: %v",
			processingErr,
			err,
		)
	}

	if err := message.Ack(); err != nil {
		return fmt.Errorf(
			"ack original message after retry: %w",
			err,
		)
	}

	return fmt.Errorf(
		"processing failed, scheduled retry %d: %w",
		attempt+1,
		processingErr,
	)
}

func (w *Worker) retryOrRejectDelete(
	ctx context.Context,
	message rabbitmq.Message,
	processingErr error,
) error {
	attempt := retryAttempt(message)

	retryQueue, ok := rabbitmq.DeleteRetryQueue(attempt + 1)
	if !ok {
		if err := message.Reject(false); err != nil {
			return fmt.Errorf(
				"deletion failed: %w; reject message: %v",
				processingErr,
				err,
			)
		}

		return fmt.Errorf(
			"deletion failed after %d retries: %w",
			attempt,
			processingErr,
		)
	}

	if message.Headers == nil {
		message.Headers = make(map[string]any)
	}
	message.Headers[retryAttemptHeader] = attempt + 1

	if err := w.broker.PublishRetry(
		ctx,
		retryQueue,
		message,
	); err != nil {
		if rejectErr := message.Reject(false); rejectErr != nil {
			return fmt.Errorf(
				"deletion failed: %w; publish retry: %v; reject message: %v",
				processingErr,
				err,
				rejectErr,
			)
		}

		return fmt.Errorf(
			"deletion failed: %w; publish retry: %v",
			processingErr,
			err,
		)
	}

	if err := message.Ack(); err != nil {
		return fmt.Errorf(
			"ack original deletion message after retry: %w",
			err,
		)
	}

	return fmt.Errorf(
		"deletion failed, scheduled retry %d: %w",
		attempt+1,
		processingErr,
	)
}
