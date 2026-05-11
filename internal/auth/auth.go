// Package auth provides simple Bearer-token middleware for protecting
// gateway endpoints. A single shared token covers all callers; pass-through
// when the token is empty so existing test setups keep working.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// RequireBearer wraps next with a Bearer-token check.
//
// If token is empty the wrapper is a no-op (auth disabled). Otherwise:
//   - Missing or non-Bearer Authorization header → 401
//   - Wrong token → 401 (constant-time compare so attackers can't time-guess)
//   - Right token → next.ServeHTTP
func RequireBearer(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		expected := []byte(token)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(h, prefix) {
				writeUnauthorized(w)
				return
			}
			provided := []byte(strings.TrimPrefix(h, prefix))
			// ConstantTimeCompare returns 1 only when the slices are equal length
			// AND identical, so we don't need a separate length check.
			if subtle.ConstantTimeCompare(provided, expected) != 1 {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="mini-llm-gateway"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"type":"unauthorized","message":"missing or invalid Authorization header"}}` + "\n"))
}
