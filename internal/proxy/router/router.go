package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/policy"
)

type Router struct {
	cfg    Config
	reg    *Registry
	fr     *FallbackRouter
	events chan RouterEvent
	console io.Writer

	logf   func(string, ...any)
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func New(cfg Config, logf func(string, ...any)) *Router {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64
	}
	if cfg.MaxMessagesBeforeChop == 0 {
		cfg.MaxMessagesBeforeChop = 20
	}
	if cfg.KeepRecentMessages <= 0 {
		cfg.KeepRecentMessages = 10
	}
	if cfg.TokenEstimateDivisor <= 0 {
		cfg.TokenEstimateDivisor = 4
	}

	return &Router{
		cfg:     cfg,
		reg:     NewRegistry(cfg.CoolingOffDuration),
		fr:      NewFallbackRouter(cfg.FallbackEndpoint, cfg.FallbackModel, cfg.FallbackAuthToken),
		events:  make(chan RouterEvent, cfg.BufferSize),
		console: os.Stdout,
		logf:    logf,
	}
}

func (r *Router) SetConsoleOutput(w io.Writer) {
	if w != nil {
		r.console = w
	}
}

func (r *Router) Events() <-chan RouterEvent {
	return r.events
}

func (r *Router) Registry() *Registry {
	return r.reg
}

func (r *Router) Fallback() *FallbackRouter {
	return r.fr
}

func (r *Router) Start(ctx context.Context, alertCh <-chan AlertTrigger) {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.wg.Add(1)
	go r.alertListener(ctx, alertCh)
	r.logf("router: started (chop=%d fallback=%s)", r.cfg.MaxMessagesBeforeChop, r.cfg.FallbackEndpoint)
}

func (r *Router) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Router) alertListener(ctx context.Context, alertCh <-chan AlertTrigger) {
	defer r.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case alert, ok := <-alertCh:
			if !ok {
				return
			}
			r.handleAlert(alert)
		}
	}
}

func (r *Router) handleAlert(alert AlertTrigger) {
	p := alert.PID
	at := alert.AnomalyType

	rm := r.remedyForAnomaly(at)
	if rm == RemedyNone {
		return
	}

	if r.cfg.Policy != nil {
		act := policy.ActionTokenChop
		if rm == RemedyFallbackRoute || rm == RemedyBoth {
			act = policy.ActionAPIRouteSwap
		}

		authorized, txID, polEvt := r.cfg.Policy.EvaluateAndRegister(policy.MitigationIntent{
			TargetPID:    p,
			ProcessName:  "",
			ActionType:   act,
			AnomalyType:  at,
			SourceModule: "router",
		})

		if !authorized {
			r.logf("router: policy rejected pid=%d remedy=%s reason=%s", p, rm, polEvt.PolicyEvaluation.RejectionReason)
			return
		}
		r.cfg.Policy.ConfirmExecution(txID)
	}

	st := r.reg.Lookup(p)
	if st == nil {
		r.reg.Activate(p, rm, "")
		r.logf("router: activated pid=%d remedy=%s type=%s", p, rm, at)
	} else {
		r.reg.RefreshAlert(p)
		r.logf("router: refreshed pid=%d remedy=%s", p, st.Remedy)
	}
}

func (r *Router) remedyForAnomaly(anomalyType string) RemedyType {
	switch anomalyType {
	case "SEMANTIC_REPETITION_LOOP":
		return RemedyTokenChop
	case "HOST_MEMORY_LEAK":
		return RemedyFallbackRoute
	case "IDLE_GPU_HOG":
		return RemedyFallbackRoute
	default:
		return RemedyTokenChop
	}
}

func (r *Router) InterceptRequest(body []byte, proxyReq *http.Request, clientPID int, model string) ([]byte, bool, *RouterEvent) {
	if clientPID <= 0 {
		return body, false, nil
	}

	state := r.reg.Lookup(int64(clientPID))
	if state == nil {
		return body, false, nil
	}

	start := time.Now()
	modifiedBody := body
	tokensSaved := 0
	handshake := HandshakeNoAction
	var appliedType string

	switch state.Remedy {
	case RemedyTokenChop:
		var err error
		modifiedBody, tokensSaved, err = ChopMessages(
			body,
			r.cfg.MaxMessagesBeforeChop,
			r.cfg.KeepRecentMessages,
			r.cfg.TokenEstimateDivisor,
		)
		if err != nil {
			handshake = HandshakeFailed
			return body, false, nil
		}
		appliedType = string(RemedyTokenChop)
		handshake = HandshakeChoppedOnly

		if tokensSaved > 0 {
			r.reg.RecordTokensSaved(int64(clientPID), tokensSaved)
		}

	case RemedyFallbackRoute:
		var err error
		modifiedBody, err = r.fr.RewriteRequest(proxyReq, body)
		if err != nil {
			handshake = HandshakeFailed
			return body, false, nil
		}
		appliedType = string(RemedyFallbackRoute)
		handshake = HandshakeRoutedAndVerified
		proxyReq.ContentLength = int64(len(modifiedBody))
		proxyReq.Header.Set("Content-Type", "application/json")

	case RemedyBoth:
		var err error
		modifiedBody, tokensSaved, err = ChopMessages(
			body,
			r.cfg.MaxMessagesBeforeChop,
			r.cfg.KeepRecentMessages,
			r.cfg.TokenEstimateDivisor,
		)
		if err == nil && tokensSaved > 0 {
			r.reg.RecordTokensSaved(int64(clientPID), tokensSaved)
		}

		modifiedBody, err = r.fr.RewriteRequest(proxyReq, modifiedBody)
		if err != nil {
			handshake = HandshakeFailed
			return body, false, nil
		}
		appliedType = string(RemedyBoth)
		handshake = HandshakeRoutedAndVerified
		proxyReq.ContentLength = int64(len(modifiedBody))
		proxyReq.Header.Set("Content-Type", "application/json")
	}

	elapsed := time.Since(start).Seconds() * 1000

	evt := RouterEvent{
		MitigationTimestamp: time.Now().Unix(),
		InterceptedProcess: InterceptedProcess{
			PID:                clientPID,
			OriginalTargetModel: model,
		},
		AppliedRemedy: AppliedRemedy{
			Type: appliedType,
			Details: RemedyDetails{
				TokensSavedByChopper: tokensSaved,
			},
		},
		ExecutionTelemetry: ExecutionTelemetry{
			ProcessingOverheadMs:   elapsed,
			RoutingHandshakeStatus: handshake,
		},
	}

	if state.Remedy == RemedyFallbackRoute || state.Remedy == RemedyBoth {
		ep, fbModel, _ := r.fr.Config()
		evt.AppliedRemedy.Details.ReroutedToLocalEndpoint = ep
		evt.AppliedRemedy.Details.SubstitutedModelString = fbModel
	}

	r.emitEvent(evt)

	proxyReq.ContentLength = int64(len(modifiedBody))

	return modifiedBody, true, &evt
}

func (r *Router) RecordResponse(body []byte, clientPID int, originalModel string) ([]byte, bool) {
	if clientPID <= 0 {
		return body, false
	}

	state := r.reg.Lookup(int64(clientPID))
	if state == nil {
		return body, false
	}

	if state.Remedy != RemedyFallbackRoute && state.Remedy != RemedyBoth {
		return body, false
	}

	if !responseIsOpenAICompatible(body) {
		return body, false
	}

	rewritten, err := r.fr.RewriteResponse(body, originalModel)
	if err != nil {
		return body, false
	}

	return rewritten, true
}

func (r *Router) emitEvent(evt RouterEvent) {
	select {
	case r.events <- evt:
	default:
		r.logf("router: event channel full, dropping event")
	}

	if err := json.NewEncoder(r.console).Encode(evt); err != nil {
		r.logf("router: encode console event: %v", err)
	}
}
