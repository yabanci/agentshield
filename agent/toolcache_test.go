package agent

import (
	"fmt"
	"sync"
	"testing"
)

func TestToolCache_HitIdenticalInput(t *testing.T) {
	c := newToolCache(64, true)
	c.Set("calc", "2+2", "4")

	v, ok := c.Get("calc", "2+2")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if v != "4" {
		t.Fatalf("expected '4', got %q", v)
	}
}

func TestToolCache_HitNormalisedInput(t *testing.T) {
	// "Calc(2+2)" and "calc(2 + 2)" share a result after normalization.
	c := newToolCache(64, true)
	c.Set("Calc", "2+2", "4")

	cases := []struct {
		tool  string
		input string
	}{
		{"calc", "2+2"},
		{"CALC", "2+2"},
		{"calc", "  2+2  "},
	}
	for _, tc := range cases {
		v, ok := c.Get(tc.tool, tc.input)
		if !ok {
			t.Errorf("expected hit for (%q, %q)", tc.tool, tc.input)
			continue
		}
		if v != "4" {
			t.Errorf("expected '4' for (%q, %q), got %q", tc.tool, tc.input, v)
		}
	}
}

func TestToolCache_MissAfterLRUEviction(t *testing.T) {
	const max = 3
	c := newToolCache(max, true)

	// Fill to capacity.
	c.Set("t", "a", "va")
	c.Set("t", "b", "vb")
	c.Set("t", "c", "vc")

	// Touch "a" and "b" so "c" becomes LRU... wait, set order is a→b→c with c=MRU.
	// Access "a" to make it MRU; LRU is now "b".
	c.Get("t", "a")
	// Access "c" to make it MRU; LRU is now "b".
	c.Get("t", "c")
	// Inserting "d" should evict "b" (LRU).
	c.Set("t", "d", "vd")

	if _, ok := c.Get("t", "b"); ok {
		t.Error("expected 'b' to be evicted (LRU)")
	}
	// The other three should still be present.
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get("t", k); !ok {
			t.Errorf("expected key %q to survive eviction", k)
		}
	}
}

func TestToolCache_ConcurrentAccess(t *testing.T) {
	c := newToolCache(64, true)
	const goroutines = 10
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := fmt.Sprintf("key%d", i%20) // shared keys to cause contention
				c.Set("tool", key, fmt.Sprintf("val-%d-%d", id, i))
				c.Get("tool", key)
			}
		}(g)
	}
	wg.Wait()
	// No race detector report = pass; size should be <= max
	if c.order.Len() > 64 {
		t.Errorf("cache grew beyond max: %d", c.order.Len())
	}
}

func TestToolCache_DisabledState(t *testing.T) {
	c := newToolCache(64, false)
	c.Set("t", "x", "v")

	if _, ok := c.Get("t", "x"); ok {
		t.Error("expected miss when cache is disabled")
	}
}

// TestToolCache_DisabledMetrics verifies that a disabled cache always returns
// miss (false) for every Get — the caller is expected to execute the tool and
// increment the misses counter. This test locks in that the disabled path does
// not silently return hits (which would suppress tool execution).
func TestToolCache_DisabledMetrics(t *testing.T) {
	c := newToolCache(64, false)

	// Populate with Set (no-op when disabled, but we call it to be explicit).
	c.Set("calculate", `{"expression":"1+1"}`, "2")
	c.Set("get_time", `{"timezone":"UTC"}`, "Monday, 01 January 2024 00:00:00")

	tools := []struct{ name, input string }{
		{"calculate", `{"expression":"1+1"}`},
		{"get_time", `{"timezone":"UTC"}`},
		{"search_docs", `{"query":"circuit breaker"}`},
	}
	for _, tc := range tools {
		_, hit := c.Get(tc.name, tc.input)
		if hit {
			t.Errorf("disabled cache: Get(%q, %q) returned hit, want miss", tc.name, tc.input)
		}
	}
}
