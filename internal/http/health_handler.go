package http

import (
	"context"
	"net/http"

	"go-avatar-service/internal/health"
)

type HealthChecker interface {
	Check(ctx context.Context) health.Result
}

type HealthHandler struct {
	checker HealthChecker
}

func NewHealthHandler(checker HealthChecker) *HealthHandler {
	return &HealthHandler{
		checker: checker,
	}
}

func (h *HealthHandler) Handle(w http.ResponseWriter, r *http.Request) {
	result := h.checker.Check(r.Context())

	status := http.StatusOK
	if !result.OK {
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, map[string]any{
		"status": healthStatus(result.OK),
		"checks": result.Checks,
	})
}

func healthStatus(ok bool) string {
	if ok {
		return "ok"
	}

	return "unhealthy"
}
