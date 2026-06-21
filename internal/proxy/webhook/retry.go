package webhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type RetryQueue struct {
	cfg        Config
	httpClient *http.Client
	tasks      chan *RetryTask
	logf       func(string, ...any)
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	diagFn     func(WebhookDispatchTelemetry)
}

func NewRetryQueue(cfg Config, taskCh chan *RetryTask, logf func(string, ...any)) *RetryQueue {
	if taskCh == nil {
		taskCh = make(chan *RetryTask, cfg.JobQueueSize)
	}
	return &RetryQueue{
		cfg:   cfg,
		tasks: taskCh,
		httpClient: &http.Client{
			Timeout: cfg.DispatchTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		logf: logf,
	}
}

func (rq *RetryQueue) Start(ctx context.Context) {
	rq.ctx, rq.cancel = context.WithCancel(ctx)
	rq.wg.Add(1)
	go rq.loop()
	rq.logf("webhook: retry queue started (max_attempts=%d base_delay=%v max_delay=%v)",
		rq.cfg.RetryMaxAttempts, rq.cfg.RetryBaseDelay, rq.cfg.RetryMaxDelay)
}

func (rq *RetryQueue) Stop() {
	if rq.cancel != nil {
		rq.cancel()
	}
	rq.wg.Wait()
}

func (rq *RetryQueue) SetDiagnosticCallback(fn func(WebhookDispatchTelemetry)) {
	rq.diagFn = fn
}

func (rq *RetryQueue) loop() {
	defer rq.wg.Done()

	for {
		select {
		case <-rq.ctx.Done():
			return
		case task := <-rq.tasks:
			rq.process(task)
		}
	}
}

func (rq *RetryQueue) process(task *RetryTask) {
	delay := rq.backoff(task.Attempt)
	task.NextRetryAt = time.Now().Add(delay)

	timer := time.NewTimer(delay)
	select {
	case <-rq.ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
	}
	timer.Stop()

	rq.retry(task)
}

func (rq *RetryQueue) retry(task *RetryTask) {
	start := time.Now()

	req, err := http.NewRequestWithContext(rq.ctx, http.MethodPost, task.TargetURL, bytes.NewReader(task.Body))
	if err != nil {
		rq.handleFailure(task, 0, err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}

	if task.Secret != "" {
		sig := signPayload(task.Body, task.Secret)
		req.Header.Set("X-AICP-Signature", formatSignature(sig))
	}

	resp, err := rq.httpClient.Do(req)
	latency := time.Since(start).Seconds() * 1000

	if err != nil {
		rq.handleFailure(task, 0, err.Error())
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rq.handleFailure(task, resp.StatusCode, fmt.Sprintf("HTTP %d", resp.StatusCode))
		return
	}

	rq.logf("webhook: retry delivered to %s (tenant=%s attempt=%d latency=%.2fms)",
		task.TargetURL, task.TenantID, task.Attempt, latency)

	rq.emitDiag(WebhookDispatchTelemetry{
		DispatchTimestamp: time.Now().Unix(),
		TargetTenantID:    task.TenantID,
		TargetURL:         task.TargetURL,
		HTTPStatusCode:    resp.StatusCode,
		DeliveryLatencyMs: latency,
		DeliveryStatus:    DeliveryStatusDelivered,
		RetryAttempt:      task.Attempt,
		SignaturePresent:  task.Secret != "",
	})
}

func (rq *RetryQueue) handleFailure(task *RetryTask, statusCode int, errMsg string) {
	task.Attempt++
	task.LastError = errMsg
	task.LastStatus = statusCode

	if task.Attempt > rq.cfg.RetryMaxAttempts {
		rq.logf("webhook: dead letter after %d attempts for %s (tenant=%s last_error=%s)",
			task.Attempt-1, task.TargetURL, task.TenantID, errMsg)

		rq.emitDiag(WebhookDispatchTelemetry{
			DispatchTimestamp: time.Now().Unix(),
			TargetTenantID:    task.TenantID,
			TargetURL:         task.TargetURL,
			HTTPStatusCode:    statusCode,
			DeliveryLatencyMs: 0,
			DeliveryStatus:    DeliveryStatusDead,
			RetryAttempt:      task.Attempt - 1,
			SignaturePresent:  task.Secret != "",
		})
		return
	}

	delay := rq.backoff(task.Attempt)
	task.NextRetryAt = time.Now().Add(delay)

	rq.logf("webhook: retry %d/%d for %s in %v (last_error=%s)",
		task.Attempt, rq.cfg.RetryMaxAttempts, task.TargetURL, delay, errMsg)

	rq.emitDiag(WebhookDispatchTelemetry{
		DispatchTimestamp: time.Now().Unix(),
		TargetTenantID:    task.TenantID,
		TargetURL:         task.TargetURL,
		HTTPStatusCode:    statusCode,
		DeliveryLatencyMs: 0,
		DeliveryStatus:    DeliveryStatusRetrying,
		RetryAttempt:      task.Attempt,
		SignaturePresent:  task.Secret != "",
	})

	select {
	case rq.tasks <- task:
	case <-rq.ctx.Done():
	}
}

func (rq *RetryQueue) backoff(attempt int) time.Duration {
	b := float64(rq.cfg.RetryBaseDelay.Milliseconds())
	m := float64(rq.cfg.RetryMaxDelay.Milliseconds())

	e := b * math.Pow(2, float64(attempt-1))
	c := math.Min(e, m)

	if c <= 0 {
		c = 1
	}

	j := rand.Float64() * c

	return time.Duration(c+j) * time.Millisecond
}

func (rq *RetryQueue) emitDiag(diag WebhookDispatchTelemetry) {
	if rq.diagFn != nil {
		rq.diagFn(diag)
	}
}
