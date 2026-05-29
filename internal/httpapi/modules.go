package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
	"github.com/go-chi/chi/v5"
)

type ModulesHandler struct {
	configStore *appconfig.Store
	moduleStore *modules.Store
}

func NewModulesHandler(configStore *appconfig.Store, moduleStore *modules.Store) *ModulesHandler {
	return &ModulesHandler{configStore: configStore, moduleStore: moduleStore}
}

func (h *ModulesHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/modules", h.List)
	r.Post("/api/modules/reload", h.Reload)
	r.Post("/api/modules/restart", h.Restart)
}

func (h *ModulesHandler) List(w http.ResponseWriter, r *http.Request) {
	states, err := h.moduleStore.List(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": states, "next_cursor": nil})
}

func (h *ModulesHandler) Reload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys       []string `json:"keys"`
		ModuleName string   `json:"module_name"`
		Reason     string   `json:"reason"`
	}
	if err := decodeStrictJSON(r, &req, true); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if req.ModuleName != "" {
		result, err := h.moduleStore.Reload(r.Context(), req.ModuleName, req.Reason)
		if err != nil {
			writeModuleError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	result, err := h.configStore.Reload(r.Context(), req.Keys)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if containsKey(result.AcceptedKeys, "modules.enabled") {
		settings, err := h.configStore.RuntimeSettings(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if err := h.moduleStore.Seed(r.Context(), settings.EnabledModules); err != nil {
			writeError(w, r, http.StatusInternalServerError, "module_reload_failed", err.Error())
			return
		}
		result.AppliedKeys = append(result.AppliedKeys, "modules.enabled")
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *ModulesHandler) Restart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModuleName string `json:"module_name"`
		Reason     string `json:"reason"`
	}
	if err := decodeStrictJSON(r, &req, false); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if req.ModuleName == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "module_name is required")
		return
	}
	result, err := h.moduleStore.Restart(r.Context(), req.ModuleName, req.Reason)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeModuleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, modules.ErrModuleNotRegistered):
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, modules.ErrModuleUnavailable):
		writeError(w, r, http.StatusBadRequest, "module_unavailable", err.Error())
	case errors.Is(err, modules.ErrModuleLifecycle):
		writeError(w, r, http.StatusInternalServerError, "module_lifecycle_failed", err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
	}
}

func containsKey(keys []string, wanted string) bool {
	for _, key := range keys {
		if key == wanted {
			return true
		}
	}
	return false
}

func decodeStrictJSON(r *http.Request, dst any, allowEmpty bool) error {
	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return io.EOF
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		if allowEmpty {
			return nil
		}
		return io.EOF
	}
	if trimmed == "null" {
		return fmt.Errorf("request body must contain a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("request body must contain a single JSON object")
}
