package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type IngestionServer struct {
	config      Config
	httpServer  *http.Server
	dispatcher  *IngestionDispatcher
	tenantStore *TenantStore
	logf        func(string, ...any)
	routeFns    []func(chi.Router)
}

func (s *IngestionServer) RegisterRoutes(fn func(chi.Router)) {
	s.routeFns = append(s.routeFns, fn)
}

func NewIngestionServer(cfg Config, downstream DownstreamHandler, logf func(string, ...any)) *IngestionServer {
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = DefaultConfig().WorkerCount
	}
	if cfg.JobQueueSize <= 0 {
		cfg.JobQueueSize = DefaultConfig().JobQueueSize
	}
	if cfg.MaxPayloadSize <= 0 {
		cfg.MaxPayloadSize = DefaultConfig().MaxPayloadSize
	}

	return &IngestionServer{
		config:      cfg,
		dispatcher:  NewIngestionDispatcher(cfg, downstream, logf),
		tenantStore: NewTenantStore(),
		logf:        logf,
	}
}

func (s *IngestionServer) Start() error {
	s.dispatcher.Start()

	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(chimw.Logger)
	r.Use(chimw.RealIP)
	r.Use(TenantAuthMiddleware(s.tenantStore))
	r.Post("/api/v2/telemetry/submit", s.handleSubmit)

	for _, fn := range s.routeFns {
		fn(r)
	}

	h2s := &http2.Server{}
	handler := h2c.NewHandler(r, h2s)

	s.httpServer = &http.Server{
		Addr:         s.config.ListenAddr,
		Handler:      handler,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	listener, err := net.Listen("tcp", s.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("ingestion: listen on %s: %w", s.config.ListenAddr, err)
	}

	s.logf("ingestion: listening on %s (workers=%d queue=%d max_payload=%d)",
		listener.Addr().String(), s.config.WorkerCount, s.config.JobQueueSize, s.config.MaxPayloadSize)

	if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *IngestionServer) Stop() error {
	s.logf("ingestion: shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logf("ingestion: HTTP Shutdown: %v", err)
	}

	s.dispatcher.Stop()
}


func (s *IngestionServer) TenantStore() *TenantStore {
	return s.tenantStore
}

type agentSubmitPayload struct {
	AgentID       string          `json:"agent_id"`
	AuthTokenHash string          `json:"auth_token_hash,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

func (s *IngestionServer) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		w.Write([]byte(`{"error":"content-type must be application/json"}`))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.config.MaxPayloadSize)

	var body agentSubmitPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			w.Write([]byte(`{"error":"payload exceeds maximum allowed size"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid JSON payload"}`))
		return
	}

	if body.AgentID == "" || len(body.Payload) == 0 || string(body.Payload) == "null" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":"missing required fields: agent_id and payload"}`))
		return
	}

	tenantID, _ := r.Context().Value(ctxKeyTenantID).(string)
	agentID, _ := r.Context().Value(ctxKeyAgentID).(string)

	if tenantID == "" || agentID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthenticated request"}`))
		return
	}

	if body.AgentID != agentID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"agent_id mismatch between header and body"}`))
		return
	}

	originIP := extractOriginIP(r)

	s.dispatcher.Dispatch(tenantID, agentID, originIP, body.Payload, time.Now())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"accepted"}`))
}

func extractOriginIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if idx := strings.Index(fwd, ","); idx != -1 {
			return strings.TrimSpace(fwd[:idx])
		}
		return strings.TrimSpace(fwd)
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	if host := r.RemoteAddr; host != "" {
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			return host[:idx]
		}
		return host
	}
	return ""
}
