package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/anomalyco/ai-compute-profiler/pkg/ingestion"
)

type ctxKey string

const (
	ctxKeyRole ctxKey = "iam_role"
)

func CtxRole() any {
	return ctxKeyRole
}

func IAMAuthMiddleware(km *KeyManager, cache *TokenCache) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agentID := r.Header.Get("X-Agent-ID")
			authHeader := r.Header.Get("Authorization")

			if agentID == "" || authHeader == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing credentials"})
				return
			}

			token, ok := strings.CutPrefix(authHeader, "Bearer ")
			if !ok || token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid authorization header"})
				return
			}

			hash := sha256Hex(token)
			cached, hit := cache.Get(hash)
			if hit && time.Now().Before(cached.ExpiresAt) {
				setContextClaims(r, cached.TenantID, agentID, cached.Role)
				next.ServeHTTP(w, r)
				return
			}

			cached, err := km.ValidateKey(token)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or unknown token"})
				return
			}

			cached.AgentID = agentID
			cache.Set(hash, cached)

			setContextClaims(r, cached.TenantID, agentID, cached.Role)
			next.ServeHTTP(w, r)
		})
	}
}

func EnforcePermission(required Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roleVal := r.Context().Value(ctxKeyRole)
			if roleVal == nil {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "no role assigned"})
				return
			}

			userRole, ok := roleVal.(Role)
			if !ok {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid role"})
				return
			}

			userLevel, userExists := roleHierarchy[userRole]
			reqLevel, reqExists := roleHierarchy[required]
			if !userExists || !reqExists {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "undefined role"})
				return
			}

			if userLevel < reqLevel {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error":         "insufficient permissions",
					"required_role": string(required),
					"assigned_role": string(userRole),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func setContextClaims(r *http.Request, tenantID, agentID string, role Role) {
	ctx := context.WithValue(r.Context(), ingestion.CtxKeyTenantID, tenantID)
	ctx = context.WithValue(ctx, ingestion.CtxKeyAgentID, agentID)
	ctx = context.WithValue(ctx, ctxKeyRole, role)
	*r = *r.WithContext(ctx)
}

func writeJSON(w http.ResponseWriter, status int, data map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func sha256Hex(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
