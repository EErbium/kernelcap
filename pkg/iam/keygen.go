package iam

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	keyBytes     = 32
	saltBytes    = 16
	hashAlgo     = "SHA-256"
)

type KeyManager struct {
	store KeyStore
	logf  func(string, ...any)
}

type KeyStore interface {
	StoreKey(record *APIKeyRecord) error
	LookupKey(keyHash string) (*APIKeyRecord, bool)
}

func NewKeyManager(store KeyStore, logf func(string, ...any)) *KeyManager {
	return &KeyManager{store: store, logf: logf}
}

func (km *KeyManager) GenerateKey(tenantID string, role Role, expiry time.Duration) (rawKey string, record *APIKeyRecord, err error) {
	raw := make([]byte, keyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("iam: generate random: %w", err)
	}

	rawKey = KeyPrefix + base64.RawURLEncoding.EncodeToString(raw)

	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", nil, fmt.Errorf("iam: generate salt: %w", err)
	}

	keyHash := sha256.Sum256(raw)

	now := time.Now()
	record = &APIKeyRecord{
		TenantID:  tenantID,
		KeyHash:   hex.EncodeToString(keyHash[:]),
		Salt:      hex.EncodeToString(salt),
		Role:      role,
		CreatedAt: now,
		ExpiresAt: now.Add(expiry),
	}

	if err := km.store.StoreKey(record); err != nil {
		return "", nil, fmt.Errorf("iam: store key: %w", err)
	}

	return rawKey, record, nil
}

func (km *KeyManager) ValidateKey(rawKey string) (*CachedToken, error) {
	start := time.Now()

	if !strings.HasPrefix(rawKey, KeyPrefix) {
		return nil, fmt.Errorf("iam: invalid key prefix")
	}

	encoded := strings.TrimPrefix(rawKey, KeyPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("iam: decode key: %w", err)
	}

	if len(raw) != keyBytes {
		return nil, fmt.Errorf("iam: unexpected key length %d", len(raw))
	}

	hash := sha256.Sum256(raw)
	hashHex := hex.EncodeToString(hash[:])

	record, ok := km.store.LookupKey(hashHex)
	if !ok {
		return nil, fmt.Errorf("iam: unknown key")
	}

	if time.Now().After(record.ExpiresAt) {
		return nil, fmt.Errorf("iam: key expired")
	}

	token := &CachedToken{
		TenantID:  record.TenantID,
		AgentID:   "",
		Role:      record.Role,
		ExpiresAt: record.ExpiresAt,
	}

	km.logf("iam: validated key tenant=%s role=%s latency=%.2fms",
		record.TenantID, record.Role, time.Since(start).Seconds()*1000)

	return token, nil
}


