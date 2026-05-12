// auth.go — bearer-token authentication for sensitive endpoints.
//
// When cfg.AuthToken is set, /demo/* and /config/* require the token.
// When unset, those endpoints are open (preserves dev experience).
package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// authToken returns the configured token, or "" if auth is disabled.
func (h *Handler) authToken() string {
	return h.cfg.AuthToken
}

// AuthEnabled reports whether bearer-token auth is required.
func (h *Handler) AuthEnabled() bool {
	return h.authToken() != ""
}

// requireAuth wraps a handler with bearer-token validation when auth is on.
// When cfg.AuthToken is empty, it acts as a no-op (open access).
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := h.authToken()
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
