package httpapi

import (
	"encoding/json"
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
		_ = json.NewDecoder(r.Body).Decode(&req)
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
	result, err := h.moduleStore.Restart(r.Context(), req.ModuleName, req.Reason)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "module_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
