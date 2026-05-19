package agent

import (
	"container/list"
	"strings"
	"sync"
)

// toolCache is a per-session LRU cache for tool results.
// Its purpose is to eliminate redundant round-trips when the ReAct loop calls
// the same tool with the same (normalized) input more than once in a single chat
// turn — a common thrash pattern in long reasoning chains.
//
// sync.Mutex is used instead of sync.Map because LRU eviction requires atomic
// reads and writes to both the map and the doubly-linked list; sync.Map's
// per-key granularity does not protect the list side.
type toolCache struct {
	mu      sync.Mutex
	max     int
	items   map[string]*list.Element // key → list element
	order   *list.List               // front = MRU, back = LRU
	enabled bool
}

type cacheEntry struct {
	key   string
	value string
}

// newToolCache creates a per-session tool result cache.
// When enabled is false, every call to Get is a miss and Set is a no-op —
// the cache is transparent and costs only a flag check.
func newToolCache(maxEntries int, enabled bool) *toolCache {
	return &toolCache{
		max:     maxEntries,
		items:   make(map[string]*list.Element, maxEntries),
		order:   list.New(),
		enabled: enabled,
	}
}

// normKey produces a stable cache key from a tool name and its raw input.
// Normalisation: lowercase + collapse internal whitespace + trim.
// This is intentional: "Calc(2+2)" and "calc(2 + 2)" share a result because
// the tool's actual execution is deterministic for equivalent inputs.
func normKey(tool, input string) string {
	tool = strings.ToLower(strings.TrimSpace(tool))
	input = strings.ToLower(strings.TrimSpace(input))
	// Collapse runs of whitespace to a single space.
	fields := strings.Fields(input)
	input = strings.Join(fields, " ")
	return tool + "\x00" + input
}

// Get returns the cached observation for (tool, input) and a hit flag.
// Returns ("", false) when disabled or on a cache miss.
func (c *toolCache) Get(tool, input string) (string, bool) {
	if !c.enabled {
		return "", false
	}
	k := normKey(tool, input)
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return "", false
	}
	// Move to front (most-recently-used).
	c.order.MoveToFront(el)
	return el.Value.(*cacheEntry).value, true //nolint:forcetypeassert // list contains only *cacheEntry
}

// Set stores an observation for (tool, input).
// When the cache is at capacity, the least-recently-used entry is evicted first.
// No-op when disabled.
func (c *toolCache) Set(tool, input, result string) {
	if !c.enabled {
		return
	}
	k := normKey(tool, input)
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[k]; ok {
		// Update existing entry and promote to MRU.
		el.Value.(*cacheEntry).value = result //nolint:forcetypeassert
		c.order.MoveToFront(el)
		return
	}

	// Evict LRU when full.
	if c.order.Len() >= c.max {
		lru := c.order.Back()
		if lru != nil {
			c.order.Remove(lru)
			delete(c.items, lru.Value.(*cacheEntry).key) //nolint:forcetypeassert
		}
	}

	el := c.order.PushFront(&cacheEntry{key: k, value: result})
	c.items[k] = el
}
