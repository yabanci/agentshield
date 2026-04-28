package agent_test

import (
	"testing"
	"time"

	"github.com/yabanci/agentshield/agent"
)

func TestSessionStore_GetOrCreate(t *testing.T) {
	store := agent.NewTestSessionStore()

	s1 := store.GetOrCreate("abc")
	if s1.ID != "abc" {
		t.Errorf("expected id=abc, got %s", s1.ID)
	}

	// Same ID returns same session
	s2 := store.GetOrCreate("abc")
	if s2.ID != s1.ID {
		t.Error("expected same session for same ID")
	}
}

func TestSessionStore_AddAndGet(t *testing.T) {
	store := agent.NewTestSessionStore()
	store.GetOrCreate("sess1")

	store.Add("sess1", agent.Message{Role: "user", Content: "hello", At: time.Now()})
	store.Add("sess1", agent.Message{Role: "assistant", Content: "hi", At: time.Now()})

	sess := store.Get("sess1")
	if sess == nil {
		t.Fatal("session not found")
	}
	if len(sess.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(sess.Messages))
	}
	if sess.Messages[0].Role != "user" {
		t.Errorf("expected role=user, got %s", sess.Messages[0].Role)
	}
}

func TestSessionStore_GetMissing(t *testing.T) {
	store := agent.NewTestSessionStore()
	if store.Get("nonexistent") != nil {
		t.Error("expected nil for missing session")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := agent.NewTestSessionStore()
	store.GetOrCreate("del1")
	store.Delete("del1")
	if store.Get("del1") != nil {
		t.Error("session should be deleted")
	}
}

func TestSessionStore_MaxHistory(t *testing.T) {
	store := agent.NewTestSessionStore()
	store.GetOrCreate("big")

	// Add more messages than maxHistory (20)
	for i := 0; i < 30; i++ {
		store.Add("big", agent.Message{Role: "user", Content: "msg", At: time.Now()})
	}

	sess := store.Get("big")
	if len(sess.Messages) > 20 {
		t.Errorf("expected max 20 messages, got %d", len(sess.Messages))
	}
}

func TestSessionStore_Count(t *testing.T) {
	store := agent.NewTestSessionStore()
	store.GetOrCreate("a")
	store.GetOrCreate("b")
	store.GetOrCreate("c")

	if store.Count() != 3 {
		t.Errorf("expected count=3, got %d", store.Count())
	}
}
