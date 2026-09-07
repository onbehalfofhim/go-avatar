package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go-avatar-service/internal/health"
)

type healthCheckerStub struct {
	result health.Result
}

func (s healthCheckerStub) Check(context.Context) health.Result {
	return s.result
}

func TestHealthHandlerHandleHealthy(t *testing.T) {
	handler := NewHealthHandler(healthCheckerStub{
		result: health.Result{
			OK: true,
			Checks: map[string]string{
				"postgres": "ok",
				"s3":       "ok",
				"rabbitmq": "ok",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.Handle(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{
		"status": "ok",
		"checks": {
			"postgres": "ok",
			"s3": "ok",
			"rabbitmq": "ok"
		}
	}`, rec.Body.String())
}

func TestHealthHandlerHandleUnhealthy(t *testing.T) {
	handler := NewHealthHandler(healthCheckerStub{
		result: health.Result{
			OK: false,
			Checks: map[string]string{
				"postgres": "ok",
				"s3":       "ok",
				"rabbitmq": "rabbitmq connection is closed",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.Handle(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.JSONEq(t, `{
		"status": "unhealthy",
		"checks": {
			"postgres": "ok",
			"s3": "ok",
			"rabbitmq": "rabbitmq connection is closed"
		}
	}`, rec.Body.String())
}

func TestHealthCheckerCheck(t *testing.T) {
	checker := health.NewChecker()

	checker.Add("postgres", func(context.Context) error {
		return nil
	})

	checker.Add("s3", func(context.Context) error {
		return errors.New("storage unavailable")
	})

	result := checker.Check(context.Background())

	require.False(t, result.OK)
	require.Equal(t, "ok", result.Checks["postgres"])
	require.Equal(t, "storage unavailable", result.Checks["s3"])
}
