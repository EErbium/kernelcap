package policy

import (
	"context"
	"sync"
	"time"
)

type TransactionLedger struct {
	mu           sync.RWMutex
	records      map[string]*MitigationRecord
	pidIndex     map[int64][]string
	maxEntries   int
	sweepInterval time.Duration
	cooldownSecs int
	logf         func(string, ...any)
}

func NewTransactionLedger(cfg Config, logf func(string, ...any)) *TransactionLedger {
	maxEnt := cfg.LedgerMaxEntries
	if maxEnt <= 0 {
		maxEnt = 10000
	}
	si := cfg.SweepInterval
	if si <= 0 {
		si = 5 * time.Minute
	}
	cd := cfg.CooldownSeconds
	if cd < 0 {
		cd = 60
	}
	return &TransactionLedger{
		records:       make(map[string]*MitigationRecord),
		pidIndex:      make(map[int64][]string),
		maxEntries:    maxEnt,
		sweepInterval: si,
		cooldownSecs:  cd,
		logf:          logf,
	}
}

func (tl *TransactionLedger) Register(intent MitigationIntent, status MitigationStatus, evaluatedRules []RuleName, riskScore float64, cooldownSecs int, rejectionReason string) *MitigationRecord {
	now := time.Now().UnixNano()
	txID := generateTxID()
	expiry := now + int64(cooldownSecs)*1e9

	record := &MitigationRecord{
		TransactionID:    txID,
		Timestamp:        now,
		Expiry:           expiry,
		TargetPID:        intent.TargetPID,
		ContainerID:      intent.ContainerID,
		ProcessName:      intent.ProcessName,
		ActionType:       intent.ActionType,
		Status:           status,
		EvaluatedRules:   len(evaluatedRules),
		MatchingPolicies: evaluatedRules,
		RiskScore:        riskScore,
		HistoricalCount:  0,
		CooldownSeconds:  cooldownSecs,
		RejectionReason:  rejectionReason,
		Metadata:         intent.Metadata,
	}

	tl.mu.Lock()
	record.HistoricalCount = len(tl.pidIndex[intent.TargetPID])
	tl.records[txID] = record
	tl.pidIndex[intent.TargetPID] = append(tl.pidIndex[intent.TargetPID], txID)
	tl.mu.Unlock()

	return record
}

func (tl *TransactionLedger) UpdateStatus(txID string, status MitigationStatus) bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	record, ok := tl.records[txID]
	if !ok {
		return false
	}
	record.Status = status
	return true
}

func (tl *TransactionLedger) Lookup(txID string) *MitigationRecord {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	return tl.records[txID]
}

func (tl *TransactionLedger) HistoricalCount(pid int64) int {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	txIDs := tl.pidIndex[pid]
	if len(txIDs) == 0 {
		return 0
	}
	now := time.Now().UnixNano()
	count := 0
	for _, id := range txIDs {
		rec := tl.records[id]
		if rec != nil && now <= rec.Expiry {
			count++
		}
	}
	return count
}

func (tl *TransactionLedger) ListExecutedExpired() []MitigationRecord {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	now := time.Now().UnixNano()
	result := make([]MitigationRecord, 0)
	for _, rec := range tl.records {
		if rec.Status == StatusExecuted && now > rec.Expiry {
			result = append(result, *rec)
		}
	}
	return result
}

func (tl *TransactionLedger) ListActive() []MitigationRecord {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	now := time.Now().UnixNano()
	result := make([]MitigationRecord, 0)
	for _, rec := range tl.records {
		if rec.Status == StatusAuthorizedPending || rec.Status == StatusExecuted {
			if now <= rec.Expiry {
				result = append(result, *rec)
			}
		}
	}
	return result
}

func (tl *TransactionLedger) Count() int {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	return len(tl.records)
}

func (tl *TransactionLedger) StartSweeper(ctx context.Context) {
	go tl.sweepLoop(ctx)
}

func (tl *TransactionLedger) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(tl.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tl.sweep()
		}
	}
}

func (tl *TransactionLedger) sweep() {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	now := time.Now().UnixNano()

	for txID, rec := range tl.records {
		if now > rec.Expiry {
			delete(tl.records, txID)
		}
	}

	for len(tl.records) > tl.maxEntries {
		oldest := ""
		var oldestTs int64 = now
		for txID, rec := range tl.records {
			if rec.Timestamp <= oldestTs {
				oldestTs = rec.Timestamp
				oldest = txID
			}
		}
		if oldest == "" {
			break
		}
		delete(tl.records, oldest)
	}

	tl.rebuildPIDIndex()
	tl.logf("policy: ledger sweep removed records (total=%d)", len(tl.records))
}

func (tl *TransactionLedger) rebuildPIDIndex() {
	newIndex := make(map[int64][]string)
	for txID, rec := range tl.records {
		pid := rec.TargetPID
		newIndex[pid] = append(newIndex[pid], txID)
	}
	tl.pidIndex = newIndex
}

func (tl *TransactionLedger) CooldownSeconds() int {
	return tl.cooldownSecs
}
