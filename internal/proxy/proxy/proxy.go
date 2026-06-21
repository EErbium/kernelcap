package proxy

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/router"
)

type ProxyConfig struct {
	ListenAddr   string
	LogWriter    io.Writer
	ProcResolver *ProcResolver
	TokenOutput  chan<- TokenUsageEvent
	Router       *router.Router
}

type Proxy struct {
	cfg      ProxyConfig
	logger   *MetricsLogger
	srv      *http.Server
	routes   []UpstreamRoute
}

func NewProxy(cfg ProxyConfig) *Proxy {
	routes := DefaultRoutes()
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":9999"
	}
	if cfg.LogWriter == nil {
		cfg.LogWriter = io.Discard
	}
	return &Proxy{
		cfg:    cfg,
		logger: NewMetricsLogger(cfg.LogWriter),
		routes: routes,
	}
}

func (p *Proxy) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleProxy)

	p.srv = &http.Server{
		Addr:    p.cfg.ListenAddr,
		Handler: mux,
	}

	return p.srv.ListenAndServe()
}

func (p *Proxy) Stop() error {
	if p.srv != nil {
		return p.srv.Close()
	}
	return nil
}

func (p *Proxy) handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		http.Error(w, "CONNECT not supported, use plain HTTP proxy", http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Host == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	route := p.matchRoute(r.URL.Host)
	if route == nil {
		http.Error(w, "No matching route for "+r.URL.Host, http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	r.Body.Close()

	reqMeta := extractRequestMeta(body, route.Provider)
	if reqMeta.Model == "" {
		reqMeta.Model = "unknown"
	}

	var clientPID int
	if p.cfg.ProcResolver != nil {
		pid, err := p.cfg.ProcResolver.ResolvePID(0, 0)
		if err == nil {
			clientPID = pid
		}
	}

	proxyReq, err := http.NewRequest(r.Method, route.BaseURL+r.URL.Path, strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}

	for key, vals := range r.Header {
		if key == "Proxy-Connection" || key == "Proxy-Authorization" {
			continue
		}
		for _, v := range vals {
			proxyReq.Header.Add(key, v)
		}
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	if p.cfg.Router != nil {
		modifiedBody, modified, _ := p.cfg.Router.InterceptRequest(body, proxyReq, clientPID, reqMeta.Model)
		if modified {
			body = modifiedBody
			proxyReq.Body = io.NopCloser(strings.NewReader(string(body)))
			proxyReq.ContentLength = int64(len(body))
			reqMeta.Model = router.ExtractFallbackModelName(body, reqMeta.Model)
		}
	}

	resp, err := http.DefaultTransport.RoundTrip(proxyReq)
	if err != nil {
		http.Error(w, "Upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if reqMeta.Stream {
		p.handleStreamingResponse(resp.Body, w, route.Provider, reqMeta, clientPID)
	} else {
		p.handleNonStreamingResponse(resp.Body, w, route.Provider, reqMeta, clientPID)
	}
}

func (p *Proxy) handleStreamingResponse(respBody io.ReadCloser, w io.Writer, provider AIProvider, meta RequestMeta, pid int) {
	flusher, ok := w.(writeFlusher)
	if !ok {
		io.Copy(w, respBody)
		return
	}

	var finalMetrics TokenMetrics
	interceptor := newInterceptReader(respBody, provider, func(tm TokenMetrics) {
		finalMetrics = tm
	})

	buf := make([]byte, 4096)
	for {
		n, err := interceptor.Read(buf)
		if n > 0 {
			flusher.Write(buf[:n])
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}

	if finalMetrics.TotalTokens > 0 || finalMetrics.PromptTokens > 0 {
		p.logEvent(TokenUsageEvent{
			Timestamp:   time.Now().Unix(),
			ClientPID:   pid,
			Provider:    provider,
			Model:       meta.Model,
			IsStreaming: true,
			Metrics:     finalMetrics,
		})
	}
}

func (p *Proxy) handleNonStreamingResponse(respBody io.ReadCloser, w io.Writer, provider AIProvider, meta RequestMeta, pid int) {
	extractor := newTokenExtractor(provider)

	_, err := io.Copy(extractor, respBody)
	if err != nil && err != io.EOF {
		return
	}

	responseBody := extractor.bodyCopy.Bytes()

	if p.cfg.Router != nil {
		rewritten, changed := p.cfg.Router.RecordResponse(responseBody, pid, meta.Model)
		if changed {
			responseBody = rewritten
		}
	}

	w.Write(responseBody)

	metrics := extractor.extractNonStreaming()

	if metrics.TotalTokens > 0 || metrics.PromptTokens > 0 {
		p.logEvent(TokenUsageEvent{
			Timestamp:   time.Now().Unix(),
			ClientPID:   pid,
			Provider:    provider,
			Model:       meta.Model,
			IsStreaming: false,
			Metrics:     metrics,
		})
	}
}

func (p *Proxy) matchRoute(host string) *UpstreamRoute {
	host = strings.ToLower(host)
	host = strings.Split(host, ":")[0]

	for i := range p.routes {
		pattern := strings.ToLower(p.routes[i].HostPattern)
		pattern = strings.Split(pattern, ":")[0]
		if strings.Contains(host, pattern) || strings.Contains(pattern, host) {
			return &p.routes[i]
		}
	}
	return nil
}

func (p *Proxy) logEvent(ev TokenUsageEvent) {
	p.logger.LogEvent(ev)
	if p.cfg.TokenOutput != nil {
		select {
		case p.cfg.TokenOutput <- ev:
		default:
		}
	}
}

type writeFlusher interface {
	io.Writer
	Flush() error
}
