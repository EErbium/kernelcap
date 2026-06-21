package ingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

type TenantStore struct {
	mu      sync.RWMutex
	entries map[string]string
}

func NewTenantStore() *TenantStore {
	return &TenantStore{
		entries: make(map[string]string),
	}
}

func (s *TenantStore) AddTenant(tenantID, rawToken string) {
	hash := sha256Hex(rawToken)
	s.mu.Lock()
	s.entries[hash] = tenantID
	s.mu.Unlock()
}

func (s *TenantStore) Lookup(tokenHash string) (string, bool) {
	s.mu.RLock()
	tenantID, ok := s.entries[tokenHash]
	s.mu.RUnlock()
	return tenantID, ok
}

func (s *TenantStore) Remove(tenantID string) {
	s.mu.Lock()
	for hash, id := range s.entries {
		if id == tenantID {
			delete(s.entries, hash)
		}
	}
	s.mu.Unlock()
}

func (s *TenantStore) Len() int {
	s.mu.RLock()
	n := len(s.entries)
	s.mu.RUnlock()
	return n
}

func sha256Hex(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
