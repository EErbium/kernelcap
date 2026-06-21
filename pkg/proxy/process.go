package proxy

import (
	"fmt"
	"sync"
	"time"
)

type ProcResolver struct {
	impl procResolverImpl
	mu   sync.RWMutex
	age  time.Time
	ttl  time.Duration
}

type procResolverImpl interface {
	start() error
	resolveFromPort(localPort uint16, proxyPort int) (int, error)
	refresh() error
}

var ErrProcResolverUnavailable = fmt.Errorf("process resolver: not available on this platform")

func NewProcResolver() *ProcResolver {
	return &ProcResolver{
		ttl: 2 * time.Second,
	}
}

func (r *ProcResolver) Start() error {
	impl, err := newPlatformProcResolver()
	if err != nil {
		return err
	}
	r.impl = impl
	return nil
}

func (r *ProcResolver) ResolvePID(localPort uint16, proxyPort int) (int, error) {
	if r.impl == nil {
		return 0, ErrProcResolverUnavailable
	}

	r.mu.RLock()
	expired := time.Since(r.age) > r.ttl
	r.mu.RUnlock()

	if expired {
		if err := r.impl.refresh(); err != nil {
			return 0, fmt.Errorf("refresh: %w", err)
		}
		r.mu.Lock()
		r.age = time.Now()
		r.mu.Unlock()
	}

	return r.impl.resolveFromPort(localPort, proxyPort)
}
