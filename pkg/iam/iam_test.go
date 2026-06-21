package iam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/ingestion"
)

type memKeyStore struct {
	mu      sync.RWMutex
	entries map[string]*APIKeyRecord
}

func newMemKeyStore() *memKeyStore {
	return &memKeyStore{entries: make(map[string]*APIKeyRecord)}
}

func (s *memKeyStore) StoreKey(record *APIKeyRecord) error {
	s.mu.Lock()
	s.entries[record.KeyHash] = record
	s.mu.Unlock()
	return nil
}

func (s *memKeyStore) LookupKey(keyHash string) (*APIKeyRecord, bool) {
	s.mu.RLock()
	r, ok := s.entries[keyHash]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return r, true
}

func TestRoleHierarchy(t *testing.T) {
	if roleHierarchy[RoleViewer] >= roleHierarchy[RoleOperator] {
		t.Fatal("Viewer should be below Operator")
	}
	if roleHierarchy[RoleOperator] >= roleHierarchy[RoleAdmin] {
		t.Fatal("Operator should be below Admin")
	}
}

func TestKeyGenerateAndFormat(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	rawKey, record, err := km.GenerateKey("tenant-alpha", RoleAdmin, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if !strings.HasPrefix(rawKey, KeyPrefix) {
		t.Fatalf("key should start with %s, got %s", KeyPrefix, rawKey)
	}

	parts := strings.SplitN(rawKey, "_", 3)
	if len(parts) != 3 {
		t.Fatalf("unexpected key format: %s", rawKey)
	}

	if record.TenantID != "tenant-alpha" {
		t.Fatalf("expected tenant-alpha, got %s", record.TenantID)
	}
	if record.Role != RoleAdmin {
		t.Fatalf("expected Admin role, got %s", record.Role)
	}
	if record.KeyHash == "" {
		t.Fatal("key hash should not be empty")
	}
	if record.Salt == "" {
		t.Fatal("salt should not be empty")
	}
}

func TestKeyValidateValid(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	rawKey, _, err := km.GenerateKey("tenant-beta", RoleOperator, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	token, err := km.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if token.TenantID != "tenant-beta" {
		t.Fatalf("expected tenant-beta, got %s", token.TenantID)
	}
	if token.Role != RoleOperator {
		t.Fatalf("expected Operator role, got %s", token.Role)
	}
}

func TestKeyValidateWrongPrefix(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	_, err := km.ValidateKey("wrong_prefix_abc123")
	if err == nil {
		t.Fatal("expected error for wrong prefix")
	}
}

func TestKeyValidateExpired(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	rawKey, _, err := km.GenerateKey("tenant-gamma", RoleViewer, -time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	_, err = km.ValidateKey(rawKey)
	if err == nil {
		t.Fatal("expected error for expired key")
	}
}

func TestKeyValidateUnknownKey(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	_, err := km.ValidateKey("aicp_live_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestKeyStorePersistence(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	rawKey, record, err := km.GenerateKey("tenant-delta", RoleAdmin, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	fetched, ok := store.LookupKey(record.KeyHash)
	if !ok {
		t.Fatal("key should exist in store")
	}
	if fetched.TenantID != "tenant-delta" {
		t.Fatalf("expected tenant-delta, got %s", fetched.TenantID)
	}

	token, err := km.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("re-validate: %v", err)
	}
	if token.Role != RoleAdmin {
		t.Fatalf("expected Admin, got %s", token.Role)
	}
}

func TestTokenCacheBasic(t *testing.T) {
	cache := NewTokenCache(Config{CacheTTL: time.Hour, CacheMaxSize: 100})
	cache.Start()
	defer cache.Stop()

	token := &CachedToken{TenantID: "tenant-epsilon", AgentID: "agent-01", Role: RoleViewer}
	hash := "abc123hash"
	cache.Set(hash, token)

	cached, ok := cache.Get(hash)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cached.TenantID != "tenant-epsilon" {
		t.Fatalf("expected tenant-epsilon, got %s", cached.TenantID)
	}

	cache.Remove(hash)
	_, ok = cache.Get(hash)
	if ok {
		t.Fatal("expected cache miss after remove")
	}
}

func TestTokenCacheTTL(t *testing.T) {
	cache := NewTokenCache(Config{CacheTTL: 50 * time.Millisecond, CacheMaxSize: 100})
	cache.Start()
	defer cache.Stop()

	token := &CachedToken{TenantID: "tenant-zeta", AgentID: "agent-02", Role: RoleOperator}
	cache.Set("hash-ttl", token)

	_, ok := cache.Get("hash-ttl")
	if !ok {
		t.Fatal("expected hit before TTL expiry")
	}

	time.Sleep(100 * time.Millisecond)

	_, ok = cache.Get("hash-ttl")
	if ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestTokenCacheMaxSize(t *testing.T) {
	cache := NewTokenCache(Config{CacheTTL: time.Hour, CacheMaxSize: 3})
	cache.Start()
	defer cache.Stop()

	for i := 0; i < 5; i++ {
		token := &CachedToken{TenantID: "tenant-eta", AgentID: "agent", Role: RoleViewer}
		cache.Set("hash-"+itoa(i), token)
	}

	if cache.Len() > 3 {
		t.Fatalf("cache size should not exceed max 3, got %d", cache.Len())
	}
}

func TestTokenCacheConcurrent(t *testing.T) {
	cache := NewTokenCache(Config{CacheTTL: time.Minute, CacheMaxSize: 1000})
	cache.Start()
	defer cache.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			token := &CachedToken{TenantID: "tenant-theta", AgentID: "agent", Role: RoleViewer}
			cache.Set("hash-"+itoa(id), token)
		}(i)
	}
	wg.Wait()

	if cache.Len() != 50 {
		t.Fatalf("expected 50 entries, got %d", cache.Len())
	}

	var readWg sync.WaitGroup
	for i := 0; i < 50; i++ {
		readWg.Add(1)
		go func(id int) {
			defer readWg.Done()
			cache.Get("hash-" + itoa(id))
		}(i)
	}
	readWg.Wait()
}

func TestEnforcePermissionViewer(t *testing.T) {
	tests := []struct {
		name       string
		userRole   Role
		required   Role
		expectPass bool
	}{
		{"viewer-can-read", RoleViewer, RoleViewer, true},
		{"viewer-cannot-operate", RoleViewer, RoleOperator, false},
		{"viewer-cannot-admin", RoleViewer, RoleAdmin, false},
		{"operator-can-read", RoleOperator, RoleViewer, true},
		{"operator-can-operate", RoleOperator, RoleOperator, true},
		{"operator-cannot-admin", RoleOperator, RoleAdmin, false},
		{"admin-can-read", RoleAdmin, RoleViewer, true},
		{"admin-can-operate", RoleAdmin, RoleOperator, true},
		{"admin-can-admin", RoleAdmin, RoleAdmin, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := EnforcePermission(tt.required)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/", nil)
			ctx := context.WithValue(req.Context(), ctxKeyRole, tt.userRole)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if tt.expectPass && w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if !tt.expectPass && w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d", w.Code)
			}
		})
	}
}

func TestEnforcePermissionNoRole(t *testing.T) {
	handler := EnforcePermission(RoleViewer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestIAMAuthMiddlewareCacheHit(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	rawKey, _, err := km.GenerateKey("tenant-iota", RoleAdmin, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	hash := sha256Hex(rawKey)
	cache := NewTokenCache(Config{CacheTTL: time.Hour, CacheMaxSize: 100})
	cache.Start()
	defer cache.Stop()

	cache.Set(hash, &CachedToken{
		TenantID: "tenant-iota",
		AgentID:  "agent-01",
		Role:     RoleAdmin,
	})

	var capturedTenant, capturedAgent string
	var capturedRole Role

	mw := IAMAuthMiddleware(km, cache)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTenant, _ = r.Context().Value(ingestion.CtxKeyTenantID).(string)
		capturedAgent, _ = r.Context().Value(ingestion.CtxKeyAgentID).(string)
		capturedRole, _ = r.Context().Value(ctxKeyRole).(Role)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Agent-ID", "agent-01")
	req.Header.Set("Authorization", "Bearer "+rawKey)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedTenant != "tenant-iota" {
		t.Fatalf("expected tenant-iota, got %s", capturedTenant)
	}
	if capturedAgent != "agent-01" {
		t.Fatalf("expected agent-01, got %s", capturedAgent)
	}
	if capturedRole != RoleAdmin {
		t.Fatalf("expected Admin, got %s", capturedRole)
	}
}

func TestIAMAuthMiddlewareCacheMiss(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	rawKey, _, err := km.GenerateKey("tenant-kappa", RoleOperator, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	cache := NewTokenCache(Config{CacheTTL: time.Hour, CacheMaxSize: 100})
	cache.Start()
	defer cache.Stop()

	var capturedTenant string
	mw := IAMAuthMiddleware(km, cache)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTenant, _ = r.Context().Value(ingestion.CtxKeyTenantID).(string)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Agent-ID", "agent-cache-miss")
	req.Header.Set("Authorization", "Bearer "+rawKey)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if capturedTenant != "tenant-kappa" {
		t.Fatalf("expected tenant-kappa, got %s", capturedTenant)
	}

	cached, ok := cache.Get(sha256Hex(rawKey))
	if !ok {
		t.Fatal("expected cache to be populated after miss")
	}
	if cached.TenantID != "tenant-kappa" {
		t.Fatalf("expected tenant-kappa in cache, got %s", cached.TenantID)
	}
}

func TestIAMAuthMiddlewareMissingCredentials(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	cache := NewTokenCache(Config{CacheTTL: time.Hour, CacheMaxSize: 100})
	cache.Start()
	defer cache.Stop()

	mw := IAMAuthMiddleware(km, cache)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestSecurityAuditEventJSON(t *testing.T) {
	event := SecurityAuditEvent{
		SecurityEventTimestamp: 1782352800,
		TransactionType:        "API_KEY_PROVISIONED",
		AuditDetails: AuditDetails{
			ActionTriggeredBy:   "admin@enterprise-client.com",
			TargetTenantID:      "tenant_enterprise_reliance_09",
			AssignedRoleProfile: "Admin",
			TokenMetadata: TokenMetadata{
				KeyPrefix:           "aicp_live_",
				KeyTruncatedDisplay: "aicp_live_***89ab",
				ExpirationTimestamp: 1813888800,
			},
		},
		CryptoVerification: CryptoVerification{
			StorageHashMechanism:  "SHA-256",
			CachePopulationStatus: CacheCommitted,
			AuthLatencyMs:         0.28,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed["security_event_timestamp"].(float64) != 1782352800 {
		t.Fatalf("unexpected timestamp")
	}
	if parsed["transaction_type"] != "API_KEY_PROVISIONED" {
		t.Fatalf("unexpected transaction type")
	}

	details := parsed["audit_details"].(map[string]any)
	if details["action_triggered_by_user"] != "admin@enterprise-client.com" {
		t.Fatalf("unexpected user")
	}
	if details["target_tenant_id"] != "tenant_enterprise_reliance_09" {
		t.Fatalf("unexpected tenant id")
	}
	if details["assigned_role_profile"] != "Admin" {
		t.Fatalf("unexpected role")
	}

	meta := details["token_metadata"].(map[string]any)
	if meta["key_prefix"] != "aicp_live_" {
		t.Fatalf("unexpected key prefix")
	}
	if meta["key_truncated_display"] != "aicp_live_***89ab" {
		t.Fatalf("unexpected truncated display")
	}

	crypto := parsed["cryptographic_verification"].(map[string]any)
	if crypto["storage_hash_mechanism"] != "SHA-256" {
		t.Fatalf("unexpected hash mechanism")
	}
	if crypto["cache_population_status"] != "COMMITTED_SUCCESS" {
		t.Fatalf("unexpected cache status")
	}
}

func TestTokenCacheEvictionLoop(t *testing.T) {
	cache := NewTokenCache(Config{CacheTTL: 50 * time.Millisecond, CacheMaxSize: 100})
	cache.Start()
	defer cache.Stop()

	cache.Set("evict-test", &CachedToken{TenantID: "t", AgentID: "a", Role: RoleViewer})

	time.Sleep(100 * time.Millisecond)

	_, ok := cache.Get("evict-test")
	if ok {
		t.Fatal("expected eviction after TTL")
	}
}

func TestMultipleKeyGenerationAndValidation(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	for i := 0; i < 10; i++ {
		rawKey, _, err := km.GenerateKey("tenant-multi-"+itoa(i), RoleViewer, time.Hour)
		if err != nil {
			t.Fatalf("generate %d: %v", i, err)
		}

		token, err := km.ValidateKey(rawKey)
		if err != nil {
			t.Fatalf("validate %d: %v", i, err)
		}
		if token.TenantID != "tenant-multi-"+itoa(i) {
			t.Fatalf("unexpected tenant for key %d", i)
		}
	}

	if record, ok := store.LookupKey("nonexistent"); ok {
		t.Fatalf("nonexistent key should not be found, got %+v", record)
	}
}

func TestKeyManagerConcurrentGenerate(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rawKey, _, err := km.GenerateKey("tenant-con-"+itoa(id), RoleAdmin, time.Hour)
			if err != nil {
				t.Errorf("generate: %v", err)
				return
			}
			if _, err := km.ValidateKey(rawKey); err != nil {
				t.Errorf("validate: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

func TestKeyValidateWrongKeyAfterValidKey(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	rawKey, _, err := km.GenerateKey("tenant-lambda", RoleAdmin, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	token, err := km.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("validate valid key: %v", err)
	}
	if token.TenantID != "tenant-lambda" {
		t.Fatalf("unexpected tenant")
	}

	wrongKey := "aicp_live_" + rawKey[len(KeyPrefix):] + "extra"
	_, err = km.ValidateKey(wrongKey)
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestIAMAuthMiddlewareUnauthorized(t *testing.T) {
	store := newMemKeyStore()
	km := NewKeyManager(store, t.Logf)

	cache := NewTokenCache(Config{CacheTTL: time.Hour, CacheMaxSize: 100})
	cache.Start()
	defer cache.Stop()

	mw := IAMAuthMiddleware(km, cache)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Agent-ID", "agent-unauth")
	req.Header.Set("Authorization", "Bearer invalid-token-that-is-wrong")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}


