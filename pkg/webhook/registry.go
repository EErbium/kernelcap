package webhook

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

type WebhookRegistry struct {
	mu    sync.RWMutex
	store map[string]*WebhookConfig
}

func NewWebhookRegistry() *WebhookRegistry {
	return &WebhookRegistry{
		store: make(map[string]*WebhookConfig),
	}
}

func (r *WebhookRegistry) Add(cfg *WebhookConfig) error {
	r.mu.Lock()
	r.store[cfg.TenantID] = cfg
	r.mu.Unlock()
	return nil
}

func (r *WebhookRegistry) Get(tenantID string) (*WebhookConfig, bool) {
	r.mu.RLock()
	cfg, ok := r.store[tenantID]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return cfg, true
}

func (r *WebhookRegistry) Update(cfg *WebhookConfig) error {
	r.mu.Lock()
	r.store[cfg.TenantID] = cfg
	r.mu.Unlock()
	return nil
}

func (r *WebhookRegistry) Remove(tenantID string) {
	r.mu.Lock()
	delete(r.store, tenantID)
	r.mu.Unlock()
}

func (r *WebhookRegistry) List() []*WebhookConfig {
	r.mu.RLock()
	out := make([]*WebhookConfig, 0, len(r.store))
	for _, cfg := range r.store {
		cp := *cfg
		cp.Secret = ""
		out = append(out, &cp)
	}
	r.mu.RUnlock()
	return out
}

func (r *WebhookRegistry) ListActive() []*WebhookConfig {
	r.mu.RLock()
	out := make([]*WebhookConfig, 0, len(r.store))
	for _, cfg := range r.store {
		if !cfg.Active {
			continue
		}
		cp := *cfg
		out = append(out, &cp)
	}
	r.mu.RUnlock()
	return out
}

func (r *WebhookRegistry) MountCRUDRoutes(router chi.Router) {
	router.Get("/config", r.handleList)
	router.Post("/config", r.handleCreate)
	router.Get("/config/{tenantID}", r.handleGet)
	router.Put("/config/{tenantID}", r.handleUpdate)
	router.Delete("/config/{tenantID}", r.handleDelete)
}

func (r *WebhookRegistry) handleList(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, r.List())
}

func (r *WebhookRegistry) handleCreate(w http.ResponseWriter, req *http.Request) {
	var cfg WebhookConfig
	if err := json.NewDecoder(req.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if cfg.TenantID == "" || cfg.URL == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "tenant_id and url are required"})
		return
	}
	r.Add(&cfg)
	writeJSON(w, http.StatusCreated, &cfg)
}

func (r *WebhookRegistry) handleGet(w http.ResponseWriter, req *http.Request) {
	tenantID := chi.URLParam(req, "tenantID")
	cfg, ok := r.Get(tenantID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (r *WebhookRegistry) handleUpdate(w http.ResponseWriter, req *http.Request) {
	tenantID := chi.URLParam(req, "tenantID")
	if _, ok := r.Get(tenantID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var cfg WebhookConfig
	if err := json.NewDecoder(req.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	cfg.TenantID = tenantID
	r.Update(&cfg)
	writeJSON(w, http.StatusOK, &cfg)
}

func (r *WebhookRegistry) handleDelete(w http.ResponseWriter, req *http.Request) {
	tenantID := chi.URLParam(req, "tenantID")
	r.Remove(tenantID)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
