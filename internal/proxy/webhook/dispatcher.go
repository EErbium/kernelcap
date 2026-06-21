package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type WebhookDispatcher struct {
	cfg       Config
	registry  *WebhookRegistry
	retryCh   chan *RetryTask
	jobCh     chan *DispatchJob
	httpClient *http.Client
	logf      func(string, ...any)
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	workerID  atomic.Uint64
	diagFn    func(WebhookDispatchTelemetry)
}

func NewWebhookDispatcher(cfg Config, registry *WebhookRegistry, retryCh chan *RetryTask, logf func(string, ...any)) *WebhookDispatcher {
	if retryCh == nil {
		retryCh = make(chan *RetryTask, cfg.JobQueueSize)
	}
	return &WebhookDispatcher{
		cfg:      cfg,
		registry: registry,
		retryCh:  retryCh,
		jobCh:    make(chan *DispatchJob, cfg.JobQueueSize),
		httpClient: &http.Client{
			Timeout: cfg.DispatchTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logf:   logf,
	}
}

func (d *WebhookDispatcher) Start(ctx context.Context) {
	d.ctx, d.cancel = context.WithCancel(ctx)
	for i := 0; i < d.cfg.WorkerCount; i++ {
		d.wg.Add(1)
		go d.worker()
	}
	d.logf("webhook: dispatcher started workers=%d queue=%d", d.cfg.WorkerCount, d.cfg.JobQueueSize)
}

func (d *WebhookDispatcher) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
}

func (d *WebhookDispatcher) SetDiagnosticCallback(fn func(WebhookDispatchTelemetry)) {
	d.diagFn = fn
}

func (d *WebhookDispatcher) Enqueue(job *DispatchJob) {
	select {
	case d.jobCh <- job:
	case <-d.ctx.Done():
	default:
		d.logf("webhook: dispatcher queue full, dropping alert %s for tenant %s", job.AlertID, job.TenantID)
	}
}

func (d *WebhookDispatcher) worker() {
	workerID := d.workerID.Add(1)
	defer d.wg.Done()

	for {
		select {
		case <-d.ctx.Done():
			return
		case job, ok := <-d.jobCh:
			if !ok {
				return
			}
			d.dispatch(workerID, job)
		}
	}
}

func (d *WebhookDispatcher) dispatch(wid uint64, job *DispatchJob) {
	start := time.Now()

	body, err := json.Marshal(job.Body)
	if err != nil {
		d.logf("webhook: worker %d marshal body: %v", wid, err)
		return
	}

	req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, job.Config.URL, bytes.NewReader(body))
	if err != nil {
		d.logf("webhook: worker %d create request: %v", wid, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range job.Config.Headers {
		req.Header.Set(k, v)
	}

	if job.Config.Secret != "" {
		sig := signPayload(body, job.Config.Secret)
		req.Header.Set("X-AICP-Signature", formatSignature(sig))
	}

	resp, err := d.httpClient.Do(req)
	latency := time.Since(start).Seconds() * 1000

	if err != nil {
		d.logf("webhook: worker %d request error for %s: %v", wid, job.Config.URL, err)
		d.enqueueRetry(job, 0, err.Error(), latency)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		d.logf("webhook: worker %d non-2xx %d for %s", wid, resp.StatusCode, job.Config.URL)
		d.enqueueRetry(job, resp.StatusCode, fmt.Sprintf("HTTP %d", resp.StatusCode), latency)
		return
	}

	d.logf("webhook: worker %d delivered to %s (tenant=%s alert=%s status=%d latency=%.2fms)",
		wid, job.Config.URL, job.TenantID, job.AlertID, resp.StatusCode, latency)

	d.emitDiag(WebhookDispatchTelemetry{
		DispatchTimestamp: time.Now().Unix(),
		TargetTenantID:    job.TenantID,
		TargetURL:         job.Config.URL,
		HTTPStatusCode:    resp.StatusCode,
		DeliveryLatencyMs: latency,
		DeliveryStatus:    DeliveryStatusDelivered,
		RetryAttempt:      0,
		SignaturePresent:  job.Config.Secret != "",
	})
}

func (d *WebhookDispatcher) enqueueRetry(job *DispatchJob, statusCode int, errMsg string, latency float64) {
	d.emitDiag(WebhookDispatchTelemetry{
		DispatchTimestamp: time.Now().Unix(),
		TargetTenantID:    job.TenantID,
		TargetURL:         job.Config.URL,
		HTTPStatusCode:    statusCode,
		DeliveryLatencyMs: latency,
		DeliveryStatus:    DeliveryStatusRetrying,
		RetryAttempt:      0,
		SignaturePresent:  job.Config.Secret != "",
	})

	task := &RetryTask{
		TenantID:   job.TenantID,
		TargetURL:  job.Config.URL,
		Secret:     job.Config.Secret,
		Headers:    job.Config.Headers,
		Body:       job.Body,
		Attempt:    1,
		LastError:  errMsg,
		LastStatus: statusCode,
	}

	select {
	case d.retryCh <- task:
	case <-d.ctx.Done():
	default:
		d.logf("webhook: retry queue full, dropping retry task for %s", job.Config.URL)
	}
}

func (d *WebhookDispatcher) emitDiag(diag WebhookDispatchTelemetry) {
	if d.diagFn != nil {
		d.diagFn(diag)
	}
}
