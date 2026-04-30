// ratelimit.go — per-IP sliding-window rate limiter middleware.
//
// Independent from the agent's own bulkhead — that limits concurrent
// requests in flight, this limits requests per IP per minute to prevent
// one client from saturating all bulkhead slots.
package api

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/yabanci/flowguard/ratelimit"
)

const (
	defaultIPRateLimit  = 60              // requests per minute per IP
	defaultIPRateWindow = time.Minute
	maxTrackedIPs       = 10_000          // LRU cap to bound memory
)

// ipLimiter tracks per-IP limiters with LRU eviction.
type ipLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipEntry
	limit    int
	window   time.Duration
}

type ipEntry struct {
	limiter  *ratelimit.Limiter
	lastUsed time.Time
}

func newIPLimiter() *ipLimiter {
	return &ipLimiter{
		limiters: make(map[string]*ipEntry),
		limit:    defaultIPRateLimit,
		window:   defaultIPRateWindow,
	}
}

// allow returns true if the IP is under the limit.
func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if e, ok := l.limiters[ip]; ok {
		e.lastUsed = time.Now()
		return e.limiter.Allow()
	}
	// LRU eviction once cap is hit.
	if len(l.limiters) >= maxTrackedIPs {
		l.evictOldestLocked()
	}
	rl := ratelimit.NewSlidingWindow(l.limit, l.window)
	l.limiters[ip] = &ipEntry{limiter: rl, lastUsed: time.Now()}
	return rl.Allow()
}

func (l *ipLimiter) evictOldestLocked() {
	var oldestIP string
	var oldestTime time.Time
	for ip, e := range l.limiters {
		if oldestIP == "" || e.lastUsed.Before(oldestTime) {
			oldestIP = ip
			oldestTime = e.lastUsed
		}
	}
	if oldestIP != "" {
		delete(l.limiters, oldestIP)
	}
}

// withIPRateLimit wraps a handler with per-source-IP rate limiting.
func (l *ipLimiter) middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !l.allow(ip) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First IP in the list is the originating client.
		if comma := indexByte(xff, ','); comma > 0 {
			return trimSpace(xff[:comma])
		}
		return trimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
