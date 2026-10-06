package http

import (
	"context"
	"net/http"
	"sync/atomic"

	"go-avatar-service/internal/health"
)

type HealthChecker interface {
	Check(ctx context.Context) health.Result
}

type HealthHandler struct {
	checker HealthChecker
	ready   atomic.Bool
}

func NewHealthHandler(checker HealthChecker) *HealthHandler {
	h := &HealthHandler{
		checker: checker,
	}
	h.ready.Store(true)
	return h
}

func (h *HealthHandler) SetReady(ready bool) {
	h.ready.Store(ready)
}

func (h *HealthHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.HandleReadiness(w, r)
}

func (h *HealthHandler) HandleLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *HealthHandler) HandleReadiness(w http.ResponseWriter, r *http.Request) {
	if !h.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "draining",
			"checks": map[string]string{},
		})
		return
	}

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
