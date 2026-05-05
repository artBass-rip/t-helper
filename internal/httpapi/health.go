package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/artBass-rip/t-helper/internal/runtime"
)

type HealthHandler struct {
	service *runtime.HealthService
}

func NewHealthHandler(service *runtime.HealthService) *HealthHandler {
	return &HealthHandler{service: service}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status := h.service.Status(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if status.Readiness != runtime.ReadinessReady {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(status)
}
