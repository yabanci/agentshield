package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yabanci/agentshield/config"
)

// testHandler builds a Handler with a Config whose AuthToken is set as given.
// agent is nil — the auth middleware doesn't dereference it.
func testHandler(token string) *Handler {
	cfg := config.Defaults()
	cfg.AuthToken = token
	return &Handler{cfg: cfg, ipLimiter: newIPLimiter()}
}

func TestAuth_DisabledAllowsAll(t *testing.T) {
	h := testHandler("")
	called := false
	wrapped := h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	wrapped(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Error("handler should be called when auth is disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuth_RequiresHeader(t *testing.T) {
	h := testHandler("secret-token")
	wrapped := h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	wrapped(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without Authorization header, got %d", w.Code)
	}
}

func TestAuth_RejectsWrongToken(t *testing.T) {
	h := testHandler("secret-token")
	wrapped := h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	wrapped(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", w.Code)
	}
}

func TestAuth_AcceptsCorrectToken(t *testing.T) {
	h := testHandler("secret-token")
	called := false
	wrapped := h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()
	wrapped(w, r)

	if !called {
		t.Error("handler should be called with correct token")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuth_RejectsMalformedHeader(t *testing.T) {
	h := testHandler("secret")
	wrapped := h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []string{
		"secret",             // missing "Bearer "
		"Token secret",       // wrong scheme
		"Basic dXNlcjpwd2Q=", // basic auth
		"",                   // empty
	}
	for _, hv := range cases {
		t.Run(hv, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if hv != "" {
				r.Header.Set("Authorization", hv)
			}
			w := httptest.NewRecorder()
			wrapped(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

func TestAuth_AuthEnabledFollowsConfig(t *testing.T) {
	h := testHandler("")
	if h.AuthEnabled() {
		t.Error("AuthEnabled should be false when cfg.AuthToken is empty")
	}
	h2 := testHandler("x")
	if !h2.AuthEnabled() {
		t.Error("AuthEnabled should be true when cfg.AuthToken is set")
	}
}
