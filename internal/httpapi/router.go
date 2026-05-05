package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	router chi.Router
}

func New(health *HealthHandler) *Server {
	r := chi.NewRouter()
	r.Use(CorrelationID)
	r.Get("/api/health", health.ServeHTTP)
	return &Server{router: r}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
