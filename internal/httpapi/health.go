package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/artBass-rip/t-helper/internal/runtime"
	"github.com/go-chi/chi/v5"
)

type HealthHandler struct {
	service *runtime.HealthService
}

func NewHealthHandler(service *runtime.HealthService) *HealthHandler {
	return &HealthHandler{service: service}
}

func (h *HealthHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/health", h.ServeHTTP)
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status := h.service.Status(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if status.Readiness != runtime.ReadinessReady {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(status)
}
