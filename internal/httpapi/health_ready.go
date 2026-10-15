package httpapi

import (
	"context"
	"net/http"
	"time"
)

type readinessResult struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (h *handler) healthReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := make(map[string]string)
	ready := true

	if err := h.deps.DB.PingContext(ctx); err != nil {
		checks["postgres"] = err.Error()
		ready = false
	} else {
		checks["postgres"] = "ok"
	}

	for _, checker := range h.deps.ReadinessCheckers {
		if err := checker.Ready(ctx); err != nil {
			checks[checker.Name()] = err.Error()
			ready = false
		} else {
			checks[checker.Name()] = "ok"
		}
	}

	status := http.StatusOK
	result := readinessResult{Status: "ok", Checks: checks}
	if !ready {
		status = http.StatusServiceUnavailable
		result.Status = "unavailable"
	}

	writeJSON(w, status, result)
}
