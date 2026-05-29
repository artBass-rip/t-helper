package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	router chi.Router
}

type RouteRegistrar interface {
	RegisterRoutes(chi.Router)
}

func New(health *HealthHandler, optionalHandlers ...RouteRegistrar) *Server {
	r := chi.NewRouter()
	r.Use(CorrelationID)
	health.RegisterRoutes(r)
	for _, handler := range optionalHandlers {
		handler.RegisterRoutes(r)
	}
	return &Server{router: r}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
