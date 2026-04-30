// auth.go — bearer-token authentication for sensitive endpoints.
//
// If AGENTSHIELD_AUTH_TOKEN is set, /demo/* and /config/* require the token.
// If unset, those endpoints are open (preserves dev experience).
package api

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

const authEnvVar = "AGENTSHIELD_AUTH_TOKEN"

// authToken returns the configured token, or "" if auth is disabled.
func authToken() string {
	return os.Getenv(authEnvVar)
}

// AuthEnabled reports whether bearer-token auth is required.
func AuthEnabled() bool {
	return authToken() != ""
}

// requireAuth wraps a handler with bearer-token validation when auth is on.
// When AGENTSHIELD_AUTH_TOKEN is unset, it acts as a no-op (open access).
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := authToken()
		if token == "" {
			next(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		got := auth[len(prefix):]
		// Constant-time compare to prevent timing attacks.
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
