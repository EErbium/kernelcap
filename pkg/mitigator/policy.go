package mitigator

import (
	"sync"

	"github.com/anomalyco/ai-compute-profiler/pkg/policy"
)

type PolicyEvaluator struct {
	engine *policy.Engine
}

func NewPolicyEvaluator(engine *policy.Engine) *PolicyEvaluator {
	return &PolicyEvaluator{engine: engine}
}

func (p *PolicyEvaluator) IsWhitelisted(pid int, containerID, processName string) bool {
	if p.engine == nil {
		return false
	}
	return p.engine.Evaluator().IsWhitelisted(processName, containerID)
}

type Ledger struct {
	mu      sync.RWMutex
	entries map[string]*LedgerEntry
}

func NewLedger() *Ledger {
	return &Ledger{
		entries: make(map[string]*LedgerEntry),
	}
}

func (l *Ledger) Record(entry LedgerEntry) string {
	l.mu.Lock()
	defer l.mu.Unlock()

	token := generateRevToken()
	entry.ReversibilityToken = token
	entry.Timestamp = timeNowUnix()
	entry.State = ActionPaused
	cp := entry
	l.entries[token] = &cp
	return token
}

func (l *Ledger) ResolveByToken(token string) *LedgerEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.entries[token]
}

func (l *Ledger) UpdateState(token string, state ActionState) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[token]
	if !ok {
		return false
	}
	entry.State = state
	return true
}

func (l *Ledger) ListActive() []LedgerEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var active []LedgerEntry
	for _, entry := range l.entries {
		if entry.State == ActionPaused || entry.State == ActionThrottle {
			active = append(active, *entry)
		}
	}
	return active
}

func (l *Ledger) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

func timeNowUnix() int64 {
	return int64(0)
}
