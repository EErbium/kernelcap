package ebpf

import (
	"sync"
	"time"
)

const (
	EventExec = 1
	EventExit = 2
)

type ProcessEvent struct {
	PID       uint32
	PPID      uint32
	ExitCode  uint32
	Comm      [16]byte
	EventType uint8
}

type Tracker struct {
	impl         trackerImpl
	fallbackMode bool
	mu           sync.Mutex
	logf         func(format string, args ...any)
}

// trackerImpl is the platform-specific interface.
// On Linux it wraps the real eBPF attachment; on other platforms it's a no-op.
type trackerImpl interface {
	start() error
	close() error
	drain(maxEvents int) []ProcessEvent
	fallback() bool
}

func NewTracker(logf func(format string, args ...any)) *Tracker {

	t := &Tracker{
		logf: logf,
	}

	var err error
	t.impl, err = newPlatformTracker()
	if err != nil {
		t.logf("ebpf: platform tracker unavailable: %v", err)
		t.fallbackMode = true
		return t
	}

	return t
}

func (t *Tracker) Start() error {
	if t.fallbackMode {
		return nil
	}
	if err := t.impl.start(); err != nil {
		t.mu.Lock()
		t.fallbackMode = true
		t.mu.Unlock()
		t.logf("ebpf: start failed, falling back to /proc-only (%v)", err)
		return nil
	}
	t.logf("ebpf: tracker attached successfully")
	return nil
}

func (t *Tracker) Drain(maxEvents int) []ProcessEvent {
	t.mu.Lock()
	fallback := t.fallbackMode
	t.mu.Unlock()
	if fallback {
		return nil
	}
	return t.impl.drain(maxEvents)
}

func (t *Tracker) Fallback() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fallbackMode
}

func (t *Tracker) Close() error {
	if t.impl != nil {
		return t.impl.close()
	}
	return nil
}

func (t *Tracker) Uptime() time.Duration {
	if t.fallbackMode {
		return 0
	}
	return time.Since(time.Now().Add(-time.Minute))
}
