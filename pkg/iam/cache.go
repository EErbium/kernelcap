package iam

import (
	"sync"
	"time"
)

type cacheEntry struct {
	token     *CachedToken
	expiresAt time.Time
}

type TokenCache struct {
	mu       sync.RWMutex
	entries  map[string]*cacheEntry
	ttl      time.Duration
	maxSize  int
	stopCh   chan struct{}
	stopped  bool
}

func NewTokenCache(cfg Config) *TokenCache {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	if cfg.CacheMaxSize <= 0 {
		cfg.CacheMaxSize = 10000
	}
	return &TokenCache{
		entries: make(map[string]*cacheEntry),
		ttl:     cfg.CacheTTL,
		maxSize: cfg.CacheMaxSize,
		stopCh:  make(chan struct{}),
	}
}

func (c *TokenCache) Start() {
	go c.evictionLoop()
}

func (c *TokenCache) Stop() {
	c.mu.Lock()
	c.stopped = true
	c.mu.Unlock()
	close(c.stopCh)
}

func (c *TokenCache) Get(keyHash string) (*CachedToken, bool) {
	c.mu.RLock()
	entry, ok := c.entries[keyHash]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, keyHash)
		c.mu.Unlock()
		return nil, false
	}

	return entry.token, true
}

func (c *TokenCache) Set(keyHash string, token *CachedToken) {
	expires := time.Now().Add(c.ttl)
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(expires) {
		expires = token.ExpiresAt
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxSize {
		c.evictLocked()
	}

	c.entries[keyHash] = &cacheEntry{
		token:     token,
		expiresAt: expires,
	}
}

func (c *TokenCache) Remove(keyHash string) {
	c.mu.Lock()
	delete(c.entries, keyHash)
	c.mu.Unlock()
}

func (c *TokenCache) Len() int {
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	return n
}

func (c *TokenCache) evictionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.evictExpired()
		}
	}
}

func (c *TokenCache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, k)
		}
	}
}

func (c *TokenCache) evictLocked() {
	var oldestKey string
	var oldestTime time.Time
	for k, entry := range c.entries {
		if oldestKey == "" || entry.expiresAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = entry.expiresAt
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
