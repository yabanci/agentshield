package provider

import (
	"context"
	"sync/atomic"
)

// DegradedWrapper is a decorator that, when enabled, returns intentionally
// low-quality responses. Used by the chaos demo to trigger the semantic CB
// without taking the underlying provider down.
//
// When disabled, all calls pass through unchanged.
type DegradedWrapper struct {
	inner   LLMProvider
	enabled atomic.Bool
}

func NewDegradedWrapper(inner LLMProvider) *DegradedWrapper {
	return &DegradedWrapper{inner: inner}
}

func (d *DegradedWrapper) Enable()         { d.enabled.Store(true) }
func (d *DegradedWrapper) Disable()        { d.enabled.Store(false) }
func (d *DegradedWrapper) IsEnabled() bool { return d.enabled.Load() }
func (d *DegradedWrapper) Name() string    { return d.inner.Name() }

func (d *DegradedWrapper) Generate(ctx context.Context, req Request) (Response, error) {
	if d.enabled.Load() {
		return Response{Text: degradedText(req.Prompt), FinishReason: "stop"}, nil
	}
	return d.inner.Generate(ctx, req)
}

func (d *DegradedWrapper) Stream(ctx context.Context, req Request, out chan<- string) error {
	if d.enabled.Load() {
		out <- degradedText(req.Prompt)
		close(out)
		return nil
	}
	return d.inner.Stream(ctx, req, out)
}

func (d *DegradedWrapper) Embed(ctx context.Context, text string) ([]float64, error) {
	return d.inner.Embed(ctx, text)
}

func (d *DegradedWrapper) Ping(ctx context.Context) error { return d.inner.Ping(ctx) }

// degradedText returns a low-quality response that reliably scores below
// QualityAcceptable (0.45) by combining repetition + hallucination markers
// (~score 0.10–0.15). All variants use both signals so the semantic CB trips
// consistently regardless of which branch fires.
func degradedText(prompt string) string {
	switch len(prompt) % 3 {
	case 0:
		s := "As an AI language model, I apologize but I cannot assist. "
		return s + s + s + s + s
	case 1:
		s := "I cannot and will not help. I am unable to assist with that. "
		return s + s + s + s
	default:
		s := "I'm just an AI and I cannot and will not assist with this request. "
		return s + s + s + s
	}
}
