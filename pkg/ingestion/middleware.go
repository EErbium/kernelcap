package ingestion

import (
	"context"
	"net/http"
	"strings"
)

func TenantAuthMiddleware(store *TenantStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agentID := r.Header.Get("X-Agent-ID")
			authHeader := r.Header.Get("Authorization")

			if agentID == "" || authHeader == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"missing credentials"}`))
				return
			}

			token, ok := strings.CutPrefix(authHeader, "Bearer ")
			if !ok || token == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"invalid authorization header"}`))
				return
			}

			hash := sha256Hex(token)
			tenantID, found := store.Lookup(hash)
			if !found {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unknown or mismatched tenant token"}`))
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyTenantID, tenantID)
			ctx = context.WithValue(ctx, ctxKeyAgentID, agentID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
