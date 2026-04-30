package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth_DisabledAllowsAll(t *testing.T) {
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "")
	called := false
	h := requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Error("handler should be called when auth is disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuth_RequiresHeader(t *testing.T) {
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "secret-token")
	h := requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without Authorization header, got %d", w.Code)
	}
}

func TestAuth_RejectsWrongToken(t *testing.T) {
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "secret-token")
	h := requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", w.Code)
	}
}

func TestAuth_AcceptsCorrectToken(t *testing.T) {
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "secret-token")
	called := false
	h := requireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()
	h(w, r)

	if !called {
		t.Error("handler should be called with correct token")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuth_RejectsMalformedHeader(t *testing.T) {
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "secret")
	h := requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []string{
		"secret",            // missing "Bearer "
		"Token secret",      // wrong scheme
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
			h(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

func TestAuth_AuthEnabledFollowsEnv(t *testing.T) {
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "")
	if AuthEnabled() {
		t.Error("AuthEnabled should be false when env is empty")
	}
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "x")
	if !AuthEnabled() {
		t.Error("AuthEnabled should be true when env is set")
	}
}
