package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	router chi.Router
}

func New(health *HealthHandler, optionalHandlers ...any) *Server {
	r := chi.NewRouter()
	r.Use(CorrelationID)
	r.Get("/api/health", health.ServeHTTP)
	for _, handler := range optionalHandlers {
		switch h := handler.(type) {
		case *ConfigHandler:
			r.Get("/api/config", h.Get)
			r.Put("/api/config", h.Put)
		case *ModulesHandler:
			r.Get("/api/modules", h.List)
			r.Post("/api/modules/reload", h.Reload)
			r.Post("/api/modules/restart", h.Restart)
		}
	}
	return &Server{router: r}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
