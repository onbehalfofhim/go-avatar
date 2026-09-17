package observability

import (
	"context"
	"log/slog"
	"time"
)

type StorageUsageProvider interface {
	GetStorageUsageByUser(context.Context) (map[string]int64, error)
}

type RabbitMQQueueDepthProvider interface {
	QueueDepth(context.Context, string) (int, error)
}

func StartStorageUsageCollector(
	ctx context.Context,
	metrics *Metrics,
	provider StorageUsageProvider,
	interval time.Duration,
) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		update := func() {
			usage, err := provider.GetStorageUsageByUser(ctx)
			if err != nil {
				slog.Error(
					"collect storage usage",
					"error", err,
				)
				return
			}

			metrics.SetStorageUsage(usage)
		}

		update()

		for {
			select {
			case <-ticker.C:
				update()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func StartRabbitMQQueueCollector(
	ctx context.Context,
	metrics *Metrics,
	provider RabbitMQQueueDepthProvider,
	queues []string,
	interval time.Duration,
) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		update := func() {
			for _, queue := range queues {
				depth, err := provider.QueueDepth(ctx, queue)
				if err != nil {
					slog.Error(
						"collect rabbitmq queue depth",
						"queue", queue,
						"error", err,
					)
					continue
				}

				metrics.SetRabbitMQQueueDepth(
					queue,
					depth,
				)
			}
		}

		update()

		for {
			select {
			case <-ticker.C:
				update()
			case <-ctx.Done():
				return
			}
		}
	}()
}
