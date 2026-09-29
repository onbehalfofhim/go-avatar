package rabbitmq

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type headerCarrier struct {
	headers amqp.Table
}

func (c headerCarrier) Get(key string) string {
	value, ok := c.headers[key]
	if !ok {
		return ""
	}

	switch value := value.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return ""
	}
}

func (c headerCarrier) Set(key string, value string) {
	c.headers[key] = value
}

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))

	for key := range c.headers {
		keys = append(keys, key)
	}

	return keys
}

func injectTraceContext(
	ctx context.Context,
	headers amqp.Table,
) {
	otel.GetTextMapPropagator().Inject(
		ctx,
		headerCarrier{headers: headers},
	)
}

func extractTraceContext(
	ctx context.Context,
	headers amqp.Table,
) context.Context {
	return otel.GetTextMapPropagator().Extract(
		ctx,
		headerCarrier{headers: headers},
	)
}

func startPublishSpan(
	ctx context.Context,
) (context.Context, func()) {
	tracer := otel.Tracer("gophprofile/rabbitmq")

	ctx, span := tracer.Start(
		ctx,
		"rabbitmq.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
	)

	return ctx, func() {
		span.End()
	}
}

func (m Message) Context(ctx context.Context) context.Context {
	return extractTraceContext(ctx, m.Headers)
}
