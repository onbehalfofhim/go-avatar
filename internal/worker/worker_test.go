package worker

import (
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"go-avatar-service/internal/broker/rabbitmq"
)

func TestRetryAttempt(t *testing.T) {
	tests := []struct {
		name    string
		message rabbitmq.Message
		want    int
	}{
		{
			name:    "no headers",
			message: rabbitmq.Message{},
			want:    0,
		},
		{
			name: "header is missing",
			message: rabbitmq.Message{
				Headers: amqp.Table{
					"other-header": "value",
				},
			},
			want: 0,
		},
		{
			name: "int",
			message: rabbitmq.Message{
				Headers: amqp.Table{
					retryAttemptHeader: 2,
				},
			},
			want: 2,
		},
		{
			name: "int32",
			message: rabbitmq.Message{
				Headers: amqp.Table{
					retryAttemptHeader: int32(2),
				},
			},
			want: 2,
		},
		{
			name: "int64",
			message: rabbitmq.Message{
				Headers: amqp.Table{
					retryAttemptHeader: int64(2),
				},
			},
			want: 2,
		},
		{
			name: "string",
			message: rabbitmq.Message{
				Headers: amqp.Table{
					retryAttemptHeader: "2",
				},
			},
			want: 2,
		},
		{
			name: "invalid string",
			message: rabbitmq.Message{
				Headers: amqp.Table{
					retryAttemptHeader: "invalid",
				},
			},
			want: 0,
		},
		{
			name: "unsupported type",
			message: rabbitmq.Message{
				Headers: amqp.Table{
					retryAttemptHeader: true,
				},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retryAttempt(tt.message)
			if got != tt.want {
				t.Fatalf("retryAttempt() = %d, want %d", got, tt.want)
			}
		})
	}
}
