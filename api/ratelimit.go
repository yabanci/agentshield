// ratelimit.go — per-IP sliding-window rate limiter middleware.
//
// Independent from the agent's own bulkhead — that limits concurrent
// requests in flight, this limits requests per IP per minute to prevent
// one client from saturating all bulkhead slots.
package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yabanci/flowguard/ratelimit"
)

const (
	defaultIPRateLimit  = 60 // requests per minute per IP
	defaultIPRateWindow = time.Minute
	// Compare costs 2x LLM + up to 6x embedding calls. Cap it tighter than
	// /chat so an anonymous caller can't trivially drain the OpenAI quota.
	compareIPRateLimit = 10
	maxTrackedIPs      = 10_000 // LRU cap to bound memory
)

// ipLimiter tracks per-IP limiters with LRU eviction.
type ipLimiter struct {
	mu             sync.Mutex
	limiters       map[string]*ipEntry
	limit          int
	window         time.Duration
	trustedProxies []*net.IPNet
}

type ipEntry struct {
	limiter  *ratelimit.Limiter
	lastUsed time.Time
}

// newIPLimiter constructs a limiter with no trusted proxies. Use
// newIPLimiterWithProxies (or set trustedProxies after construction) when
// behind a known reverse proxy.
func newIPLimiter() *ipLimiter {
	return &ipLimiter{
		limiters: make(map[string]*ipEntry),
		limit:    defaultIPRateLimit,
		window:   defaultIPRateWindow,
	}
}

// newIPLimiterWithProxies parses a comma-separated CIDR list (typically
// from cfg.TrustedProxies) and installs the proxy allow-list. Empty
// string = same as newIPLimiter().
func newIPLimiterWithProxies(cidrList string) *ipLimiter {
	l := newIPLimiter()
	l.trustedProxies = loadTrustedProxies(cidrList)
	return l
}

// loadTrustedProxies parses a comma-separated list of CIDR ranges that the
// limiter will trust to set X-Forwarded-For / X-Real-IP. Empty list means
// untrusted — the TCP peer address is always used. Without this guard, any
// client can set X-Forwarded-For: 1.2.3.4 and cycle the value to bypass
// per-IP limits entirely.
func loadTrustedProxies(env string) []*net.IPNet {
	if env == "" {
		return nil
	}
	var nets []*net.IPNet
	for _, part := range strings.Split(env, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Allow bare IPs by promoting them to /32 (or /128 for IPv6).
		if !strings.Contains(part, "/") {
			if ip := net.ParseIP(part); ip != nil {
				if ip.To4() != nil {
					part = part + "/32"
				} else {
					part = part + "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(part); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

// compareMiddleware wraps a handler with a tighter per-IP limit suitable
// for the /demo/compare endpoint (10 req/min vs 60 for /chat).
func (l *ipLimiter) compareMiddleware(next http.HandlerFunc) http.HandlerFunc {
	// Reuse the same limiters map but check against the lower compare cap.
	return func(w http.ResponseWriter, r *http.Request) {
		ip := l.clientIPFor(r)
		l.mu.Lock()
		e, ok := l.limiters[ip+":compare"]
		if !ok {
			if len(l.limiters) >= maxTrackedIPs {
				l.evictOldestLocked()
			}
			e = &ipEntry{
				limiter:  ratelimit.NewSlidingWindow(compareIPRateLimit, l.window),
				lastUsed: time.Now(),
			}
			l.limiters[ip+":compare"] = e
		}
		e.lastUsed = time.Now()
		allowed := e.limiter.Allow()
		l.mu.Unlock()

		if !allowed {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
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

// middleware wraps a handler with per-source-IP rate limiting.
func (l *ipLimiter) middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := l.clientIPFor(r)
		if !l.allow(ip) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// clientIPFor extracts the originating IP for rate-limiting. X-Forwarded-For
// and X-Real-IP are only honored when the TCP peer is in the trusted-proxies
// allowlist (AGENTSHIELD_TRUSTED_PROXIES). Without that allowlist any
// caller could spoof these headers and cycle the value to evade limits.
func (l *ipLimiter) clientIPFor(r *http.Request) string {
	peer := remoteIP(r)
	if l.peerTrusted(peer) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if comma := strings.IndexByte(xff, ','); comma > 0 {
				return strings.TrimSpace(xff[:comma])
			}
			return strings.TrimSpace(xff)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	return peer
}

// peerTrusted returns true when the TCP peer is in the trusted-proxies set.
// Empty trust list = nothing is trusted.
func (l *ipLimiter) peerTrusted(peer string) bool {
	if len(l.trustedProxies) == 0 {
		return false
	}
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	for _, cidr := range l.trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
