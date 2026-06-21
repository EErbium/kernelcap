package policy

import (
	"sync"
)

type RuleEvaluator struct {
	blacklistNames  map[string]bool
	whitelistNames  map[string]bool
	whitelistIDs    map[string]bool
	pidThreshold    int
	maxActionsPerPID int
	velocityWindow  int64

	mu sync.RWMutex
}

func NewRuleEvaluator(cfg Config) *RuleEvaluator {
	re := &RuleEvaluator{
		blacklistNames:   make(map[string]bool),
		whitelistNames:   make(map[string]bool),
		whitelistIDs:     make(map[string]bool),
		pidThreshold:     cfg.PIDThreshold,
		maxActionsPerPID: cfg.MaxActionsPerPID,
		velocityWindow:   int64(cfg.VelocityWindow.Seconds()),
	}
	if re.pidThreshold <= 0 {
		re.pidThreshold = 100
	}
	if re.maxActionsPerPID <= 0 {
		re.maxActionsPerPID = 5
	}

	for _, name := range cfg.BlacklistNames {
		if name != "" {
			re.blacklistNames[name] = true
		}
	}
	for _, name := range cfg.WhitelistNames {
		if name != "" {
			re.whitelistNames[name] = true
		}
	}
	for _, id := range cfg.WhitelistIDs {
		if id != "" {
			re.whitelistIDs[id] = true
		}
	}

	return re
}

func (re *RuleEvaluator) Evaluate(intent MitigationIntent, historicalCount int) (authorized bool, matchedRules []RuleName, rejectionRule RuleName) {
	matchedRules = make([]RuleName, 0, 4)

	if intent.TargetPID <= 0 {
		matchedRules = append(matchedRules, RulePIDBoundary)
		return false, matchedRules, RulePIDBoundary
	}

	re.mu.RLock()
	defer re.mu.RUnlock()

	if intent.TargetPID < int64(re.pidThreshold) {
		matchedRules = append(matchedRules, RuleSystemBlacklist)
		return false, matchedRules, RuleSystemBlacklist
	}

	pn := intent.ProcessName
	if pn != "" && re.blacklistNames[pn] {
		matchedRules = append(matchedRules, RuleSystemBlacklist)
		return false, matchedRules, RuleSystemBlacklist
	}

	if pn != "" && re.whitelistNames[pn] {
		matchedRules = append(matchedRules, RuleUserWhitelist)
		return false, matchedRules, RuleUserWhitelist
	}
	if intent.ContainerID != "" && re.whitelistIDs[intent.ContainerID] {
		matchedRules = append(matchedRules, RuleUserWhitelist)
		return false, matchedRules, RuleUserWhitelist
	}

	matchedRules = append(matchedRules, RulePIDBoundary)

	if historicalCount >= re.maxActionsPerPID {
		matchedRules = append(matchedRules, RuleVelocityCap)
		return false, matchedRules, RuleVelocityCap
	}

	matchedRules = append(matchedRules, RuleSystemBlacklist)

	return true, matchedRules, ""
}

func (re *RuleEvaluator) RiskScore(processName string, historicalCount int) float64 {
	re.mu.RLock()
	isBlacklisted := re.blacklistNames[processName]
	re.mu.RUnlock()

	score := 0.0
	if isBlacklisted {
		score += 0.9
	}
	if re.maxActionsPerPID > 0 {
		ratio := float64(historicalCount) / float64(re.maxActionsPerPID)
		if ratio > 1.0 {
			ratio = 1.0
		}
		score += ratio * 0.3
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func (re *RuleEvaluator) UpdateWhitelist(names, ids []string) {
	re.mu.Lock()
	defer re.mu.Unlock()

	re.whitelistNames = make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" {
			re.whitelistNames[n] = true
		}
	}
	re.whitelistIDs = make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			re.whitelistIDs[id] = true
		}
	}
}

func (re *RuleEvaluator) MaxActionsPerPID() int {
	re.mu.RLock()
	defer re.mu.RUnlock()
	return re.maxActionsPerPID
}

func (re *RuleEvaluator) IsWhitelisted(processName, containerID string) bool {
	re.mu.RLock()
	defer re.mu.RUnlock()
	if processName != "" && re.whitelistNames[processName] {
		return true
	}
	if containerID != "" && re.whitelistIDs[containerID] {
		return true
	}
	return false
}
