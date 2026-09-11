package observability

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

func NewLogger(serviceName string, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: level,
		},
	)

	return slog.New(handler).With(
		"service", serviceName,
	)
}

func LoggerFromContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return nil
	}

	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return logger
	}

	return logger.With(
		"trace_id", spanContext.TraceID().String(),
		"span_id", spanContext.SpanID().String(),
	)
}
