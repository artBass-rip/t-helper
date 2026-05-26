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
		case *JobsHandler:
			r.Get("/api/jobs", h.List)
			r.Get("/api/jobs/{id}", h.Get)
		case *StatusHandler:
			r.Get("/api/status", h.Runtime)
			r.Get("/api/status/workflows", h.Workflows)
			r.Get("/api/status/workflows/{job_group_id}", h.Workflow)
			r.Get("/api/status/jobs/{job_id}", h.Job)
			r.Get("/api/status/workers", h.Workers)
		case *ScannerHandler:
			r.Get("/api/root-paths", h.ListRootPaths)
			r.Put("/api/root-paths", h.PutRootPaths)
			r.Post("/api/scans", h.CreateScan)
			r.Get("/api/scans/{job_id}", h.GetScan)
			r.Get("/api/projects", h.ListProjects)
			r.Get("/api/projects/{id}", h.GetProject)
			r.Get("/api/projects/{id}/links", h.ListProjectLinks)
			r.Post("/api/project-scans", h.CreateProjectScan)
			r.Get("/api/repos", h.ListRepositories)
			r.Get("/api/repos/{id}", h.GetRepository)
			r.Get("/api/ignore-rules", h.ListIgnoreRules)
			r.Put("/api/ignore-rules", h.PutIgnoreRules)
			r.Get("/api/environments", h.ListEnvironments)
			r.Get("/api/environments/{id}", h.GetEnvironment)
			r.Get("/api/workspaces", h.ListWorkspaces)
			r.Get("/api/workspaces/{id}", h.GetWorkspace)
		case *RepositoryHandler:
			r.Get("/api/repo-provider-instances", h.ListProviderInstances)
			r.Put("/api/repo-provider-instances", h.PutProviderInstances)
			r.Get("/api/repo-credentials", h.ListCredentials)
			r.Put("/api/repo-credentials", h.PutCredentials)
			r.Post("/api/repos/clone", h.Clone)
			r.Post("/api/repos/pull", h.Pull)
			r.Post("/api/repos/sync", h.Sync)
		}
	}
	return &Server{router: r}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
