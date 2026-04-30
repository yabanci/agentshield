package agent_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

func TestCache_SetReturnsImmediately(t *testing.T) {
	// Embedder that takes 200ms — set() must NOT block the caller.
	slow := func(ctx context.Context, text string) ([]float64, error) {
		time.Sleep(200 * time.Millisecond)
		return []float64{1, 0, 0}, nil
	}
	c := agent.NewTestSemanticCache(time.Minute, slow)

	start := time.Now()
	c.SetForTest(context.Background(), "hello", "world")
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("set() should return immediately, took %v (embedder is async)", elapsed)
	}
}

func TestCache_AsyncEmbeddingEventuallyAttached(t *testing.T) {
	// Track when the embedder is called (proves it ran in background).
	var called atomic.Bool
	embedder := func(ctx context.Context, text string) ([]float64, error) {
		called.Store(true)
		return []float64{1, 0, 0}, nil
	}
	c := agent.NewTestSemanticCache(time.Minute, embedder)
	c.SetForTest(context.Background(), "hello", "world")

	// Wait up to 1s for the goroutine to run.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if called.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !called.Load() {
		t.Error("embedder was never called — async path is broken")
	}
}

func TestCache_ExactMatchWorksImmediately(t *testing.T) {
	// Even before the async embedding completes, exact match should hit.
	embedder := func(ctx context.Context, text string) ([]float64, error) {
		time.Sleep(500 * time.Millisecond) // slow
		return []float64{1, 0, 0}, nil
	}
	c := agent.NewTestSemanticCache(time.Minute, embedder)
	c.SetForTest(context.Background(), "hello", "world")

	// Immediately: exact match should still find it (semantic match would skip
	// because embedding=nil, falling through to exact).
	got, ok := c.GetForTest(context.Background(), "hello")
	if !ok {
		t.Fatal("exact-match get should hit even before async embedding completes")
	}
	if got != "world" {
		t.Errorf("expected 'world', got %q", got)
	}
}

func TestCache_PruneFreesMemory(t *testing.T) {
	c := agent.NewTestSemanticCache(time.Millisecond, nil) // 1ms TTL — instant expiry
	c.SetForTest(context.Background(), "a", "1")
	c.SetForTest(context.Background(), "b", "2")
	time.Sleep(10 * time.Millisecond) // let entries expire

	// Adding another entry triggers prune() — the expired entries should go.
	c.SetForTest(context.Background(), "c", "3")

	// Only "c" should be findable now.
	if _, ok := c.GetForTest(context.Background(), "a"); ok {
		t.Error("expired entry 'a' should be pruned")
	}
	if got, ok := c.GetForTest(context.Background(), "c"); !ok || got != "3" {
		t.Errorf("fresh entry should still be there: got=%q ok=%v", got, ok)
	}
}
