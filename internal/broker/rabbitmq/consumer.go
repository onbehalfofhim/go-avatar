package rabbitmq

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Message struct {
	ID          string
	Body        []byte
	ContentType string
	Headers     amqp.Table
	delivery    amqp.Delivery
}

type Consumer struct {
	channel *amqp.Channel
}

func NewConsumer(client *Client) (*Consumer, error) {
	channel, err := client.connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open consumer channel: %w", err)
	}

	return &Consumer{
		channel: channel,
	}, nil
}

func (c *Consumer) Close() error {
	if err := c.channel.Close(); err != nil {
		return fmt.Errorf("close consumer channel: %w", err)
	}

	return nil
}

func (c *Consumer) Consume(
	ctx context.Context,
	queue string,
	consumerName string,
) (<-chan Message, error) {
	deliveries, err := c.channel.Consume(
		queue,
		consumerName,
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"consume queue %q: %w",
			queue,
			err,
		)
	}

	messages := make(chan Message)

	go func() {
		defer close(messages)
		for {
			select {
			case <-ctx.Done():
				return
			case delivery, ok := <-deliveries:
				if !ok {
					return
				}

				message := Message{
					ID:          delivery.MessageId,
					Body:        delivery.Body,
					ContentType: delivery.ContentType,
					Headers:     delivery.Headers,
					delivery:    delivery,
				}

				select {
				case <-ctx.Done():
					return
				case messages <- message:
				}
			}
		}
	}()

	return messages, nil
}

func (m Message) Ack() error {
	if err := m.delivery.Ack(false); err != nil {
		return fmt.Errorf("ack message %q: %w", m.ID, err)
	}

	return nil
}

func (m Message) Reject(requeue bool) error {
	if err := m.delivery.Reject(requeue); err != nil {
		return fmt.Errorf(
			"reject message %q: %w",
			m.ID,
			err,
		)
	}

	return nil
}
