package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestNewRegistersExpectedRoutes(t *testing.T) {
	server := New(
		&HealthHandler{},
		&ConfigHandler{},
		&ModulesHandler{},
		&JobsHandler{},
		&StatusHandler{},
		&ScannerHandler{},
		&RepositoryHandler{},
	)
	expected := map[string]bool{
		"GET /api/health":                                   true,
		"GET /api/config":                                   true,
		"PUT /api/config":                                   true,
		"GET /api/modules":                                  true,
		"POST /api/modules/reload":                          true,
		"POST /api/modules/restart":                         true,
		"GET /api/jobs":                                     true,
		"GET /api/jobs/{id}":                                true,
		"GET /api/status":                                   true,
		"GET /api/status/workflows":                         true,
		"GET /api/status/workflows/{job_group_id}":          true,
		"GET /api/status/jobs/{job_id}":                     true,
		"GET /api/status/workers":                           true,
		"GET /api/root-paths":                               true,
		"PUT /api/root-paths":                               true,
		"POST /api/scans":                                   true,
		"GET /api/scans/{job_id}":                           true,
		"GET /api/projects":                                 true,
		"GET /api/projects/{id}":                            true,
		"GET /api/projects/{id}/links":                      true,
		"GET /api/projects/{id}/scan-settings":              true,
		"PUT /api/projects/{id}/scan-settings":              true,
		"GET /api/project-scans":                            true,
		"POST /api/project-scans":                           true,
		"GET /api/project-scans/{project_scan_id}":          true,
		"GET /api/project-scans/{project_scan_id}/findings": true,
		"GET /api/repos":                                    true,
		"GET /api/repos/{id}":                               true,
		"GET /api/ignore-rules":                             true,
		"PUT /api/ignore-rules":                             true,
		"GET /api/environments":                             true,
		"GET /api/environments/{id}":                        true,
		"GET /api/workspaces":                               true,
		"GET /api/workspaces/{id}":                          true,
		"GET /api/security/findings":                        true,
		"GET /api/security/findings/{id}":                   true,
		"GET /api/security/rule-sets":                       true,
		"PUT /api/security/rule-sets":                       true,
		"GET /api/tool-profiles":                            true,
		"POST /api/tool-profiles/validate":                  true,
		"POST /api/tool-profiles/import":                    true,
		"POST /api/tool-profiles/activate":                  true,
		"POST /api/tool-profiles/analyze":                   true,
		"GET /api/repo-provider-instances":                  true,
		"PUT /api/repo-provider-instances":                  true,
		"GET /api/repo-credentials":                         true,
		"PUT /api/repo-credentials":                         true,
		"POST /api/repos/clone":                             true,
		"POST /api/repos/pull":                              true,
		"POST /api/repos/sync":                              true,
	}

	seen := map[string]bool{}
	err := chi.Walk(server.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		seen[fmt.Sprintf("%s %s", method, route)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	for route := range expected {
		if !seen[route] {
			t.Fatalf("route %s was not registered", route)
		}
	}
	for route := range seen {
		if !expected[route] {
			t.Fatalf("unexpected route %s", route)
		}
	}
}
