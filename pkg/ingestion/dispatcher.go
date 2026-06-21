package ingestion

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

type IngestionDispatcher struct {
	jobCh       chan *IngestionJob
	workerWg    sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	logf        func(string, ...any)
	workerCount int
	nextID      atomic.Uint64
	downstream  DownstreamHandler
	jobPool     sync.Pool
}

func NewIngestionDispatcher(cfg Config, downstream DownstreamHandler, logf func(string, ...any)) *IngestionDispatcher {
	if downstream == nil {
		downstream = func(_ context.Context, _ *IngestionPayload) error { return nil }
	}
	return &IngestionDispatcher{
		jobCh:       make(chan *IngestionJob, cfg.JobQueueSize),
		workerCount: cfg.WorkerCount,
		downstream:  downstream,
		logf:        logf,
		jobPool: sync.Pool{
			New: func() any { return &IngestionJob{} },
		},
	}
}

func (d *IngestionDispatcher) Start() {
	d.ctx, d.cancel = context.WithCancel(context.Background())
	for i := 0; i < d.workerCount; i++ {
		d.workerWg.Add(1)
		go d.worker()
	}
}

func (d *IngestionDispatcher) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.workerWg.Wait()
	for {
		select {
		case job := <-d.jobCh:
			d.recycleJob(job)
		default:
			return
		}
	}
}

func (d *IngestionDispatcher) Dispatch(tenantID, agentID, originIP string, payload json.RawMessage, receivedAt time.Time) {
	job := d.jobPool.Get().(*IngestionJob)
	job.TenantID = tenantID
	job.AgentID = agentID
	job.OriginIP = originIP
	job.Payload = payload
	job.ReceivedAt = receivedAt

	select {
	case d.jobCh <- job:
	case <-time.After(100 * time.Millisecond):
		d.recycleJob(job)
		d.logf("dispatcher: queue full, dropped job for agent=%s tenant=%s", agentID, tenantID)
	case <-d.ctx.Done():
		d.recycleJob(job)
	}
}

func (d *IngestionDispatcher) worker() {
	workerID := d.nextID.Add(1)
	defer d.workerWg.Done()

	for {
		select {
		case <-d.ctx.Done():
			return
		case job, ok := <-d.jobCh:
			if !ok {
				return
			}
			d.process(workerID, job)
		}
	}
}

func (d *IngestionDispatcher) process(workerID uint64, job *IngestionJob) {
	p := IngestionPayload{
		IngestionMetadata: IngestionMetadata{
			ReceivedTimestamp:  job.ReceivedAt.Unix(),
			ResolvedTenantID:   job.TenantID,
			OriginIPAddress:    job.OriginIP,
			ProcessingWorkerID: workerID,
		},
		AgentPayload: AgentPayload{
			AgentID: job.AgentID,
			Payload: job.Payload,
		},
	}

	if err := d.downstream(d.ctx, &p); err != nil {
		d.logf("dispatcher: worker %d downstream error: %v", workerID, err)
	}

	d.recycleJob(job)
}

func (d *IngestionDispatcher) recycleJob(job *IngestionJob) {
	job.TenantID = ""
	job.AgentID = ""
	job.Payload = nil
	job.OriginIP = ""
	d.jobPool.Put(job)
}
