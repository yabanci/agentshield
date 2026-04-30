package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"
)

const defaultSimilarityThreshold = 0.92

type cacheEntry struct {
	prompt    string
	response  string
	embedding []float64
	expiresAt time.Time
}

// Embedder generates a vector embedding for a text.
type Embedder func(ctx context.Context, text string) ([]float64, error)

// semanticCache stores responses and retrieves them by semantic similarity.
// Falls back to exact SHA-256 match when the embedder is unavailable.
type semanticCache struct {
	mu        sync.RWMutex
	entries   []cacheEntry
	ttl       time.Duration
	embedder  Embedder
	threshold float64
}

func newSemanticCache(ttl time.Duration, embedder Embedder) *semanticCache {
	return &semanticCache{
		ttl:       ttl,
		embedder:  embedder,
		threshold: defaultSimilarityThreshold,
	}
}

// get returns a cached response for the prompt.
// Uses semantic similarity if embeddings are available, exact match otherwise.
func (c *semanticCache) get(ctx context.Context, prompt string) (string, bool) {
	c.mu.RLock()
	entries := make([]cacheEntry, len(c.entries))
	copy(entries, c.entries)
	c.mu.RUnlock()

	now := time.Now()

	// Try semantic match first
	if c.embedder != nil {
		if vec, err := c.embedder(ctx, prompt); err == nil {
			best, bestSim := "", 0.0
			for _, e := range entries {
				if now.After(e.expiresAt) || len(e.embedding) == 0 {
					continue
				}
				if sim := cosineSimilarity(vec, e.embedding); sim > bestSim {
					bestSim = sim
					best = e.response
				}
			}
			if bestSim >= c.threshold {
				cacheHitsTotal.Inc()
				return best, true
			}
		}
	}

	// Exact match fallback
	k := exactKey(prompt)
	for _, e := range entries {
		if now.After(e.expiresAt) {
			continue
		}
		if exactKey(e.prompt) == k {
			cacheHitsTotal.Inc()
			return e.response, true
		}
	}
	return "", false
}

// set stores a prompt+response pair. Embedding is computed asynchronously.
func (c *semanticCache) set(ctx context.Context, prompt, response string) {
	entry := cacheEntry{
		prompt:    prompt,
		response:  response,
		expiresAt: time.Now().Add(c.ttl),
	}

	if c.embedder != nil {
		if vec, err := c.embedder(ctx, prompt); err == nil {
			entry.embedding = vec
		}
	}

	c.mu.Lock()
	c.prune()
	c.entries = append(c.entries, entry)
	cacheSizeGauge.Set(float64(len(c.entries)))
	c.mu.Unlock()
}

func (c *semanticCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// prune removes expired entries. Must be called with lock held.
// Zeros the tail so GC can collect strings and embedding slices in
// pruned entries (filter-in-place otherwise keeps them reachable).
func (c *semanticCache) prune() {
	now := time.Now()
	live := c.entries[:0]
	for _, e := range c.entries {
		if !now.After(e.expiresAt) {
			live = append(live, e)
		}
	}
	for i := len(live); i < len(c.entries); i++ {
		c.entries[i] = cacheEntry{} // allow GC of strings and embedding slices
	}
	c.entries = live
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func exactKey(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return fmt.Sprintf("%x", h)
}
