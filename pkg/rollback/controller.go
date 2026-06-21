package rollback

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/mitigator"
	"github.com/anomalyco/ai-compute-profiler/pkg/policy"
	"github.com/anomalyco/ai-compute-profiler/pkg/router"
)

type Controller struct {
	cfg    Config
	mit    *mitigator.Mitigator
	rreg   *router.Registry
	pol    *policy.Engine
	events chan SyncEvent
	console io.Writer
	logf   func(string, ...any)
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func New(cfg Config, mit *mitigator.Mitigator, rreg *router.Registry, pol *policy.Engine, logf func(string, ...any)) *Controller {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64
	}
	if cfg.ReconciliationInterval <= 0 {
		cfg.ReconciliationInterval = 5 * time.Second
	}
	if cfg.RollbackCheckInterval <= 0 {
		cfg.RollbackCheckInterval = 1 * time.Second
	}
	if cfg.ProcRoot == "" {
		cfg.ProcRoot = "/proc"
	}

	return &Controller{
		cfg:    cfg,
		mit:    mit,
		rreg:   rreg,
		pol:    pol,
		events: make(chan SyncEvent, cfg.BufferSize),
		console: os.Stdout,
		logf:   logf,
	}
}

func (ctl *Controller) Events() <-chan SyncEvent {
	return ctl.events
}

func (ctl *Controller) SetConsoleOutput(w io.Writer) {
	if w != nil {
		ctl.console = w
	}
}

func (ctl *Controller) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	ctl.cancel = cancel

	ctl.wg.Add(1)
	go ctl.rollbackLoop(ctx)

	ctl.wg.Add(1)
	go ctl.reconcileLoop(ctx)

	ctl.logf("rollback: started (check=%s reconcile=%s)",
		ctl.cfg.RollbackCheckInterval, ctl.cfg.ReconciliationInterval)
}

func (ctl *Controller) Stop() {
	if ctl.cancel != nil {
		ctl.cancel()
	}
	ctl.wg.Wait()
}

func (ctl *Controller) rollbackLoop(ctx context.Context) {
	defer ctl.wg.Done()
	ticker := time.NewTicker(ctl.cfg.RollbackCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ctl.checkExpired()
		}
	}
}

func (ctl *Controller) reconcileLoop(ctx context.Context) {
	defer ctl.wg.Done()
	ticker := time.NewTicker(ctl.cfg.ReconciliationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ctl.reconcileOnce()
		}
	}
}

func (ctl *Controller) emitEvent(evt SyncEvent) {
	select {
	case ctl.events <- evt:
	default:
		ctl.logf("rollback: event channel full, dropping %s", evt.SyncEventID)
	}

	data, err := json.Marshal(evt)
	if err != nil {
		ctl.logf("rollback: marshal event: %v", err)
		return
	}
	ctl.console.Write(data)
	ctl.console.Write([]byte("\n"))
}
