package engine

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

type apiServer struct {
	addr       string
	metricsPtr *atomic.Pointer[UnifiedMetrics]
	srv        *http.Server
	logf       func(string, ...any)
}

func newAPIServer(addr string, metricsPtr *atomic.Pointer[UnifiedMetrics], logf func(string, ...any)) *apiServer {
	if addr == "" {
		addr = "127.0.0.1:8088"
	}
	return &apiServer{
		addr:       addr,
		metricsPtr: metricsPtr,
		logf:       logf,
	}
}

func (as *apiServer) start(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/metrics", as.handleMetrics)

	as.srv = &http.Server{
		Addr:    as.addr,
		Handler: mux,
	}

	listener, err := net.Listen("tcp", as.addr)
	if err != nil {
		as.logf("engine: dashboard API listen on %s: %v", as.addr, err)
		return
	}

	as.addr = listener.Addr().String()
	as.logf("engine: dashboard API listening on http://%s/api/v1/metrics", as.addr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		as.srv.Shutdown(shutdownCtx)
	}()

	if err := as.srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		as.logf("engine: dashboard API serve: %v", err)
	}
}

func (as *apiServer) stop() {
	if as.srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		as.srv.Shutdown(shutdownCtx)
	}
}

func (as *apiServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ptr := as.metricsPtr.Load()
	if ptr == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"engine_status":"STARTING","uptime_seconds":0,"local_node_id":"","active_monitored_pids_count":0,"active_system_anomalies":[],"system_performance_self_check":{"profiler_memory_rss_bytes":0,"profiler_cpu_utilization_pct":0}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ptr)
}
