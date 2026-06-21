package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

type WebhookEngine struct {
	cfg        Config
	registry   *WebhookRegistry
	dispatcher *WebhookDispatcher
	retryQueue *RetryQueue
	sseBroker  *SSEBroker
	alertCh    <-chan json.RawMessage
	logf       func(string, ...any)
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewWebhookEngine(cfg Config, registry *WebhookRegistry, alertCh <-chan json.RawMessage, logf func(string, ...any)) *WebhookEngine {
	if registry == nil {
		registry = NewWebhookRegistry()
	}

	retryCh := make(chan *RetryTask, cfg.JobQueueSize)
	dispatcher := NewWebhookDispatcher(cfg, registry, retryCh, logf)
	retryQueue := NewRetryQueue(cfg, retryCh, logf)
	sseBroker := NewSSEBroker(cfg, logf)

	eng := &WebhookEngine{
		cfg:        cfg,
		registry:   registry,
		dispatcher: dispatcher,
		retryQueue: retryQueue,
		sseBroker:  sseBroker,
		alertCh:    alertCh,
		logf:       logf,
	}

	return eng
}

func (e *WebhookEngine) Start(ctx context.Context) {
	e.ctx, e.cancel = context.WithCancel(ctx)

	e.dispatcher.Start(e.ctx)
	e.retryQueue.Start(e.ctx)
	e.sseBroker.Start(e.ctx)

	e.wg.Add(1)
	go e.alertLoop()

	e.logf("webhook: engine started")
}

func (e *WebhookEngine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	e.dispatcher.Stop()
	e.retryQueue.Stop()
	e.sseBroker.Stop()
}

func (e *WebhookEngine) Registry() *WebhookRegistry {
	return e.registry
}

func (e *WebhookEngine) SSEBroker() *SSEBroker {
	return e.sseBroker
}

func (e *WebhookEngine) RouteRegistrator(authMW, viewerMW, adminMW func(http.Handler) http.Handler) func(chi.Router) {
	return func(r chi.Router) {
		r.Route("/api/v2/events", func(r chi.Router) {
			r.Use(authMW)
			r.Use(viewerMW)
			r.Get("/stream", e.sseBroker.ServeHTTP)
		})

		r.Route("/api/v2/webhooks", func(r chi.Router) {
			r.Use(authMW)
			r.Use(adminMW)
			e.registry.MountCRUDRoutes(r)
		})
	}
}

func (e *WebhookEngine) SetDispatchDiagnosticCallback(fn func(WebhookDispatchTelemetry)) {
	e.dispatcher.SetDiagnosticCallback(fn)
	e.retryQueue.SetDiagnosticCallback(fn)
}

func (e *WebhookEngine) SetSSEDiagnosticCallback(fn func(SSEConnectionTelemetry)) {
	e.sseBroker.SetDiagnosticCallback(fn)
}

func (e *WebhookEngine) alertLoop() {
	defer e.wg.Done()

	for {
		select {
		case <-e.ctx.Done():
			return
		case alert, ok := <-e.alertCh:
			if !ok {
				return
			}
			e.processAlert(alert)
		}
	}
}

func (e *WebhookEngine) processAlert(alert json.RawMessage) {
	e.sseBroker.Broadcast("alert", alert)

	configs := e.registry.ListActive()
	for _, cfg := range configs {
		job := &DispatchJob{
			TenantID: cfg.TenantID,
			Config:   *cfg,
			Body:     alert,
		}
		e.dispatcher.Enqueue(job)
	}
}


