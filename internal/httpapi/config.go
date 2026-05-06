package httpapi

import (
	"encoding/json"
	"net/http"

	appconfig "github.com/artBass-rip/t-helper/internal/config"
)

type ConfigHandler struct {
	store *appconfig.Store
}

func NewConfigHandler(store *appconfig.Store) *ConfigHandler {
	return &ConfigHandler{store: store}
}

func (h *ConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.store.ActiveConfig(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *ConfigHandler) Put(w http.ResponseWriter, r *http.Request) {
	cfg, err := appconfig.Decode(r.Body)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := h.store.Import(r.Context(), cfg, nil, "api")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type ReloadHandler interface {
	Reload(r *http.Request) (any, error)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	correlationID := CorrelationIDFromContext(r.Context())
	if correlationID == "" {
		correlationID = r.Header.Get(CorrelationIDHeader)
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":           code,
			"message":        message,
			"details":        map[string]any{},
			"correlation_id": correlationID,
		},
	})
}
