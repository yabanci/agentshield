package agent

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

type cacheEntry struct {
	response  string
	expiresAt time.Time
}

type responseCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

func newResponseCache(ttl time.Duration) *responseCache {
	return &responseCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

func (c *responseCache) get(prompt string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key(prompt)]
	if !ok || time.Now().After(e.expiresAt) {
		return "", false
	}
	return e.response, true
}

func (c *responseCache) set(prompt, response string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key(prompt)] = cacheEntry{
		response:  response,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *responseCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func key(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", h)
}
