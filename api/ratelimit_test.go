package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPLimiter_AllowsUnderLimit(t *testing.T) {
	l := newIPLimiter()
	for i := 0; i < 10; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed (under 60/min limit)", i)
		}
	}
}

func TestIPLimiter_BlocksOverLimit(t *testing.T) {
	l := newIPLimiter()
	// Hit the limit (60/min by default).
	for i := 0; i < defaultIPRateLimit; i++ {
		l.allow("1.2.3.4")
	}
	if l.allow("1.2.3.4") {
		t.Error("request 61 should be blocked")
	}
}

func TestIPLimiter_DifferentIPsAreIndependent(t *testing.T) {
	l := newIPLimiter()
	for i := 0; i < defaultIPRateLimit; i++ {
		l.allow("1.1.1.1")
	}
	// 1.1.1.1 is now at limit, but 2.2.2.2 should be fresh.
	if !l.allow("2.2.2.2") {
		t.Error("different IP should not share the limit")
	}
}

func TestIPLimiter_ClientIPParses(t *testing.T) {
	cases := []struct {
		name string
		req  func() *http.Request
		want string
	}{
		{
			name: "RemoteAddr",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.RemoteAddr = "203.0.113.5:54321"
				return r
			},
			want: "203.0.113.5",
		},
		{
			name: "X-Real-IP wins over RemoteAddr",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.RemoteAddr = "10.0.0.1:80"
				r.Header.Set("X-Real-IP", "1.2.3.4")
				return r
			},
			want: "1.2.3.4",
		},
		{
			name: "X-Forwarded-For first IP",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1, 198.51.100.10")
				return r
			},
			want: "203.0.113.5",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := clientIP(c.req())
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestIPLimiter_Middleware429sOverLimit(t *testing.T) {
	l := newIPLimiter()
	hits := 0
	h := l.middleware(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < defaultIPRateLimit+5; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "1.2.3.4:1000"
		w := httptest.NewRecorder()
		h(w, r)
	}
	// Should have served exactly 60 then started returning 429.
	if hits != defaultIPRateLimit {
		t.Errorf("expected %d hits, got %d", defaultIPRateLimit, hits)
	}
}

func TestIPLimiter_LRUEvictsWhenFull(t *testing.T) {
	l := newIPLimiter()
	// Fill up to capacity.
	for i := 0; i < maxTrackedIPs; i++ {
		l.allow(fmtIP(i))
	}
	if len(l.limiters) != maxTrackedIPs {
		t.Fatalf("expected %d entries, got %d", maxTrackedIPs, len(l.limiters))
	}
	// Adding one more should evict (not grow past cap).
	l.allow("new.ip.999.999")
	if len(l.limiters) > maxTrackedIPs {
		t.Errorf("limiter map exceeded cap: %d", len(l.limiters))
	}
}

func fmtIP(i int) string {
	// Generate distinct strings cheaply.
	return "ip-" + itoaSimple(i)
}

func itoaSimple(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
