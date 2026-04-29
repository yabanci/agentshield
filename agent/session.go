package agent

import (
	"sync"
	"time"
)

const (
	sessionTTL     = 30 * time.Minute
	maxHistory     = 20 // max messages per session
	sessionCleanup = 5 * time.Minute
)

// Message is a single turn in a conversation.
type Message struct {
	Role    string    `json:"role"` // "user" | "assistant" | "tool"
	Content string    `json:"content"`
	Tier    Tier      `json:"tier,omitempty"`
	At      time.Time `json:"at"`
}

// Session is a stateful conversation with a user.
type Session struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
	LastUsed time.Time `json:"last_used"`
}

// SessionStore is an in-memory session store with TTL eviction.
// Call Stop() to terminate the background cleanup goroutine.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	done     chan struct{}
}

func newSessionStore() *SessionStore {
	return newSessionStoreInternal(true)
}

// NewTestSessionStore creates a session store without the background cleanup
// goroutine — safe for use in tests.
func NewTestSessionStore() *SessionStore {
	return newSessionStoreInternal(false)
}

// Stop terminates the background cleanup goroutine.
func (s *SessionStore) Stop() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func newSessionStoreInternal(startCleanup bool) *SessionStore {
	s := &SessionStore{
		sessions: make(map[string]*Session),
		done:     make(chan struct{}),
	}
	if startCleanup {
		go s.cleanup()
	}
	return s
}

// GetOrCreate returns an existing session or creates a new one.
func (s *SessionStore) GetOrCreate(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.LastUsed = time.Now()
		return sess
	}
	sess := &Session{
		ID:       id,
		Messages: make([]Message, 0, 8),
		LastUsed: time.Now(),
	}
	s.sessions[id] = sess
	return sess
}

// Get returns a session by ID, or nil.
func (s *SessionStore) Get(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

// Add appends a message to a session, trimming if over limit.
func (s *SessionStore) Add(sessionID string, msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return
	}
	sess.Messages = append(sess.Messages, msg)
	if len(sess.Messages) > maxHistory {
		sess.Messages = sess.Messages[len(sess.Messages)-maxHistory:]
	}
	sess.LastUsed = time.Now()
}

// List returns all active sessions (copy).
func (s *SessionStore) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, *sess)
	}
	return out
}

// Delete removes a session.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(sessionCleanup)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-sessionTTL)
			s.mu.Lock()
			for id, sess := range s.sessions {
				if sess.LastUsed.Before(cutoff) {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}
