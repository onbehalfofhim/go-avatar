package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeName = "avatars.exchange"
	DLXName      = "avatars.dlx"

	UploadRoutingKey = "avatar.uploaded"
	DeleteRoutingKey = "avatar.deleted"
	DeadLetterKey    = "avatar.dead"

	ProcessingQueue = "avatars.processing"
	DeletionQueue   = "avatars.deletion"
	DeadLetterQueue = "avatars.dlq"

	UploadRetry5sQueue  = "avatars.processing.retry.5s"
	UploadRetry10sQueue = "avatars.processing.retry.10s"
	UploadRetry20sQueue = "avatars.processing.retry.20s"

	DeleteRetry5sQueue  = "avatars.deletion.retry.5s"
	DeleteRetry10sQueue = "avatars.deletion.retry.10s"
	DeleteRetry20sQueue = "avatars.deletion.retry.20s"
)

type Client struct {
	connection *amqp.Connection
	channel    *amqp.Channel
}

func NewClient(ctx context.Context, url string) (*Client, error) {
	connection, err := amqp.DialConfig(
		url,
		amqp.Config{},
	)
	if err != nil {
		return nil, fmt.Errorf("connect to rabbitmq: %w", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open rabbitmq channel: %w", err)
	}

	client := &Client{
		connection: connection,
		channel:    channel,
	}

	if err := client.declareTopology(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}

	return client, nil
}

func (c *Client) declareTopology(ctx context.Context) error {
	if err := c.channel.ExchangeDeclare(
		ExchangeName,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare exchange %q: %w", ExchangeName, err)
	}

	if err := c.channel.ExchangeDeclare(
		DLXName,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare dead letter exchange %q: %w", DLXName, err)
	}

	if err := c.declareProcessingQueue(); err != nil {
		return err
	}

	if err := c.declareDeletionQueue(); err != nil {
		return err
	}

	if err := c.declareUploadRetryQueues(); err != nil {
		return err
	}

	if err := c.declareDeleteRetryQueues(); err != nil {
		return err
	}

	if err := c.declareDeadLetterQueue(); err != nil {
		return err
	}

	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c.connection.IsClosed() {
		return fmt.Errorf("rabbitmq connection is closed")
	}

	return nil
}

func (c *Client) Close() error {
	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			return fmt.Errorf("close rabbitmq channel: %w", err)
		}
	}

	if c.connection != nil {
		if err := c.connection.Close(); err != nil {
			return fmt.Errorf("close rabbitmq connection: %w", err)
		}
	}

	return nil
}

func (c *Client) declareProcessingQueue() error {
	queue, err := c.channel.QueueDeclare(
		ProcessingQueue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    DLXName,
			"x-dead-letter-routing-key": DeadLetterKey,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"declare processing queue %q: %w",
			ProcessingQueue,
			err,
		)
	}

	if err := c.channel.QueueBind(
		queue.Name,
		UploadRoutingKey,
		ExchangeName,
		false,
		nil,
	); err != nil {
		return fmt.Errorf(
			"bind processing queue %q: %w",
			ProcessingQueue,
			err,
		)
	}

	return nil
}

func (c *Client) declareDeletionQueue() error {
	queue, err := c.channel.QueueDeclare(
		DeletionQueue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    DLXName,
			"x-dead-letter-routing-key": DeadLetterKey,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"declare deletion queue %q: %w",
			DeletionQueue,
			err,
		)
	}

	if err := c.channel.QueueBind(
		queue.Name,
		DeleteRoutingKey,
		ExchangeName,
		false,
		nil,
	); err != nil {
		return fmt.Errorf(
			"bind deletion queue %q: %w",
			DeletionQueue,
			err,
		)
	}

	return nil
}

func (c *Client) declareDeadLetterQueue() error {
	queue, err := c.channel.QueueDeclare(
		DeadLetterQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"declare dead letter queue %q: %w",
			DeadLetterQueue,
			err,
		)
	}

	if err := c.channel.QueueBind(
		queue.Name,
		DeadLetterKey,
		DLXName,
		false,
		nil,
	); err != nil {
		return fmt.Errorf(
			"bind dead letter queue %q: %w",
			DeadLetterQueue,
			err,
		)
	}

	return nil
}

func (c *Client) PublishJSON(
	ctx context.Context,
	routingKey string,
	messageID string,
	payload any,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal rabbitmq message: %w", err)
	}

	err = c.channel.PublishWithContext(
		ctx,
		ExchangeName,
		routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    messageID,
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"publish rabbitmq message with routing key %q: %w",
			routingKey,
			err,
		)
	}

	return nil
}

func (c *Client) declareUploadRetryQueues() error {
	queues := []struct {
		name string
		ttl  int32
	}{
		{
			name: UploadRetry5sQueue,
			ttl:  5000,
		},
		{
			name: UploadRetry10sQueue,
			ttl:  10000,
		},
		{
			name: UploadRetry20sQueue,
			ttl:  20000,
		},
	}

	for _, retryQueue := range queues {
		if _, err := c.channel.QueueDeclare(
			retryQueue.name,
			true,
			false,
			false,
			false,
			amqp.Table{
				"x-message-ttl":             retryQueue.ttl,
				"x-dead-letter-exchange":    ExchangeName,
				"x-dead-letter-routing-key": UploadRoutingKey,
			},
		); err != nil {
			return fmt.Errorf(
				"declare upload retry queue %q: %w",
				retryQueue.name,
				err,
			)
		}
	}

	return nil
}

func (c *Client) declareDeleteRetryQueues() error {
	queues := []struct {
		name string
		ttl  int32
	}{
		{
			name: DeleteRetry5sQueue,
			ttl:  5000,
		},
		{
			name: DeleteRetry10sQueue,
			ttl:  10000,
		},
		{
			name: DeleteRetry20sQueue,
			ttl:  20000,
		},
	}

	for _, retryQueue := range queues {
		if _, err := c.channel.QueueDeclare(
			retryQueue.name,
			true,
			false,
			false,
			false,
			amqp.Table{
				"x-message-ttl":             retryQueue.ttl,
				"x-dead-letter-exchange":    ExchangeName,
				"x-dead-letter-routing-key": DeleteRoutingKey,
			},
		); err != nil {
			return fmt.Errorf(
				"declare delete retry queue %q: %w",
				retryQueue.name,
				err,
			)
		}
	}

	return nil
}

func (c *Client) PublishRetry(
	ctx context.Context,
	queue string,
	message Message,
) error {
	err := c.channel.PublishWithContext(
		ctx,
		"",
		queue,
		false,
		false,
		amqp.Publishing{
			ContentType:  message.ContentType,
			DeliveryMode: amqp.Persistent,
			MessageId:    message.ID,
			Headers:      message.Headers,
			Body:         message.Body,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"publish message %q to retry queue %q: %w",
			message.ID,
			queue,
			err,
		)
	}

	return nil
}

func UploadRetryQueue(attempt int) (string, bool) {
	switch attempt {
	case 1:
		return UploadRetry5sQueue, true
	case 2:
		return UploadRetry10sQueue, true
	case 3:
		return UploadRetry20sQueue, true
	default:
		return "", false
	}
}

func DeleteRetryQueue(attempt int) (string, bool) {
	switch attempt {
	case 1:
		return DeleteRetry5sQueue, true
	case 2:
		return DeleteRetry10sQueue, true
	case 3:
		return DeleteRetry20sQueue, true
	default:
		return "", false
	}
}
