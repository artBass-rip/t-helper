package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
	"github.com/artBass-rip/t-helper/internal/modules"
)

type ModulesHandler struct {
	configStore *appconfig.Store
	moduleStore *modules.Store
}

func NewModulesHandler(configStore *appconfig.Store, moduleStore *modules.Store) *ModulesHandler {
	return &ModulesHandler{configStore: configStore, moduleStore: moduleStore}
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
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			if !errors.Is(err, io.EOF) {
				writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
				return
			}
		}
	}
	if req.ModuleName != "" {
		result, err := h.moduleStore.Reload(r.Context(), req.ModuleName, req.Reason)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "module_unavailable", err.Error())
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
	if containsKey(result.AppliedKeys, "modules.enabled") {
		settings, err := h.configStore.RuntimeSettings(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if err := h.moduleStore.Seed(r.Context(), settings.EnabledModules); err != nil {
			writeError(w, r, http.StatusInternalServerError, "module_reload_failed", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *ModulesHandler) Restart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModuleName string `json:"module_name"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if req.ModuleName == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "module_name is required")
		return
	}
	result, err := h.moduleStore.Restart(r.Context(), req.ModuleName, req.Reason)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "module_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func containsKey(keys []string, wanted string) bool {
	for _, key := range keys {
		if key == wanted {
			return true
		}
	}
	return false
}
