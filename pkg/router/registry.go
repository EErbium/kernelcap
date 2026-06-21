package router

import (
	"sync"
	"time"
)

type Registry struct {
	mu            sync.RWMutex
	entries       map[int64]*MitigationState
	coolingOffDur time.Duration
}

func NewRegistry(coolingOff time.Duration) *Registry {
	return &Registry{
		entries:       make(map[int64]*MitigationState),
		coolingOffDur: coolingOff,
	}
}

func (r *Registry) Activate(pid int64, remedy RemedyType, model string) *MitigationState {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixNano()
	coolUntil := now + r.coolingOffDur.Nanoseconds()
	state, exists := r.entries[pid]
	if exists {
		state.Remedy = remedy
		state.LastAlertAt = now
		state.CoolingUntil = coolUntil
		state.Active = true
		if state.FallbackModel == "" {
			state.FallbackModel = model
		}
		return state
	}

	state = &MitigationState{
		PID:           pid,
		Remedy:        remedy,
		OriginalModel: model,
		FallbackModel: model,
		ActivatedAt:   now,
		LastAlertAt:   now,
		CoolingUntil:  coolUntil,
		Active:        true,
	}
	r.entries[pid] = state
	return state
}

func (r *Registry) Deactivate(pid int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, pid)
}

func (r *Registry) Lookup(pid int64) *MitigationState {
	r.mu.RLock()
	state, exists := r.entries[pid]
	r.mu.RUnlock()

	if !exists {
		return nil
	}

	if !state.Active {
		return nil
	}

	if time.Now().UnixNano() > state.CoolingUntil {
		r.mu.Lock()
		state.Active = false
		r.mu.Unlock()
		return nil
	}

	return state
}

func (r *Registry) RefreshAlert(pid int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, exists := r.entries[pid]
	if !exists {
		return
	}
	now := time.Now().UnixNano()
	state.LastAlertAt = now
	state.CoolingUntil = now + r.coolingOffDur.Nanoseconds()
	state.Active = true
}

func (r *Registry) RecordTokensSaved(pid int64, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, exists := r.entries[pid]
	if !exists {
		return
	}
	state.TokensSaved += count
	state.RequestsProcessed++
}

func (r *Registry) ActiveCount() (chopCount int, fallbackCount int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now().UnixNano()
	for _, state := range r.entries {
		if !state.Active || now > state.CoolingUntil {
			continue
		}
		if state.Remedy == RemedyTokenChop || state.Remedy == RemedyBoth {
			chopCount++
		}
		if state.Remedy == RemedyFallbackRoute || state.Remedy == RemedyBoth {
			fallbackCount++
		}
	}
	return
}

func (r *Registry) TotalTokensSaved() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := 0
	for _, state := range r.entries {
		total += state.TokensSaved
	}
	return total
}

func (r *Registry) Snapshot() []MitigationState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now().UnixNano()
	result := make([]MitigationState, 0, len(r.entries))
	for _, state := range r.entries {
		if state.Active && now <= state.CoolingUntil {
			result = append(result, *state)
		}
	}
	return result
}
