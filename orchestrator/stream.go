package orchestrator

import (
	"context"
	"strings"

	"github.com/yabanci/agentshield/memory"
	"github.com/yabanci/agentshield/provider"
	"github.com/yabanci/agentshield/quality"
)

// StreamToken is a single event in the quality-gated stream.
//
// Switched=true means the quality gate tripped mid-stream and the orchestrator
// has switched to the fallback model. The accompanying Reason explains why.
type StreamToken struct {
	Token    string      `json:"token,omitempty"`
	Done     bool        `json:"done,omitempty"`
	Tier     memory.Tier `json:"tier"`
	Switched bool        `json:"switched,omitempty"`
	Reason   string      `json:"reason,omitempty"`
}

// StreamWithQualityGate streams tokens with an inline quality gate.
// If hallucination markers are detected in the first 120 tokens, the stream
// aborts and continues from the fallback model. The caller receives a
// StreamToken{Switched: true} event at the switch point.
func (o *Orchestrator) StreamWithQualityGate(ctx context.Context, prompt string, out chan<- StreamToken) (memory.Tier, error) {
	canUsePrimary := !o.chaos.IsPrimaryKilled() &&
		!o.breakers.PrimarySemantic.ShouldBlock() &&
		o.breakers.PrimaryTransport.State().String() == "closed"

	if canUsePrimary {
		// Use a cancellable child context so we can abort the primary stream
		// when the quality gate trips without leaking the stream goroutine.
		streamCtx, cancelStream := context.WithCancel(ctx)
		rawTokens := make(chan string, 64)
		// errCh creates a happens-before edge between the streamer goroutine
		// finishing and the consumer reading streamErr. Without this, the race
		// detector flags a read/write race on a plain `streamErr` variable.
		errCh := make(chan error, 1)

		// provider.LLMProvider.Stream owns closing rawTokens (per its contract).
		go func() {
			errCh <- o.primary.Stream(streamCtx, provider.Request{Model: o.primaryModel, Prompt: prompt}, rawTokens)
		}()

		var buf strings.Builder
		tokenCount := 0
		tripped := false

		for token := range rawTokens {
			buf.WriteString(token)
			tokenCount++

			// Quality gate: check every 30 tokens for hallucination markers.
			if tokenCount%30 == 0 && tokenCount <= 120 {
				hallScore, reason := quality.HallucinationScore(buf.String())
				if hallScore < 0.5 {
					tripped = true
					cancelStream() // abort primary stream
					for range rawTokens {
					} // drain so goroutine can exit
					out <- StreamToken{Switched: true, Tier: memory.TierFallback,
						Reason: "quality gate: " + reason}
					break
				}
			}
			out <- StreamToken{Token: token, Tier: memory.TierPrimary}
		}
		cancelStream() // no-op if already cancelled; always call to free resources

		streamErr := <-errCh
		if !tripped && streamErr == nil {
			return memory.TierPrimary, nil
		}
	}

	// Fallback stream (no quality gate — fallback must always complete).
	// Provider closes rawTokens per LLMProvider contract.
	rawTokens := make(chan string, 64)
	go func() {
		_ = o.fallback.Stream(ctx, provider.Request{Model: o.fallbackModel, Prompt: prompt}, rawTokens)
	}()
	for token := range rawTokens {
		out <- StreamToken{Token: token, Tier: memory.TierFallback}
	}
	return memory.TierFallback, nil
}
