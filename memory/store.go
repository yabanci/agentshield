package memory

// Store bundles all memory subsystems behind a single field on the Agent.
// Reduces the Agent struct's field count and centralises memory lifecycle.
type Store struct {
	Sessions     *SessionStore
	Traces       *TraceStore
	ScoreHistory *ScoreHistory
}

// NewStore wires up sessions, traces, and score-history with the given history capacity.
func NewStore(scoreHistorySize int) *Store {
	return &Store{
		Sessions:     NewSessionStore(),
		Traces:       NewTraceStore(),
		ScoreHistory: NewScoreHistory(scoreHistorySize),
	}
}

// NewTestStore builds a Store that does not start cleanup goroutines.
// For use in tests where deterministic teardown matters.
func NewTestStore(scoreHistorySize int) *Store {
	return &Store{
		Sessions:     NewTestSessionStore(),
		Traces:       NewTestTraceStore(),
		ScoreHistory: NewScoreHistory(scoreHistorySize),
	}
}

// Stop terminates all background goroutines (cleanup tickers).
func (s *Store) Stop() {
	s.Traces.Stop()
	s.Sessions.Stop()
}
