package http

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"go-avatar-service/internal/observability"
)

func MetricsMiddleware(metrics *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unknown"
			}

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}

			logger := observability.LoggerFromContext(r.Context(), slog.Default())
			logger.Info(
				"HTTP request completed",
				"method", r.Method,
				"route", route,
				"status", status,
			)

			statusCode := strconv.Itoa(status)

			metrics.HTTPRequestsTotal.WithLabelValues(
				r.Method,
				route,
				statusCode,
			).Inc()

			metrics.HTTPRequestDuration.WithLabelValues(
				r.Method,
				route,
				statusCode,
			).Observe(time.Since(start).Seconds())
		})
	}
}
