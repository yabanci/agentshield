package provider_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yabanci/agentshield/provider"
)

type passthrough struct{ text string }

func (p passthrough) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{Text: p.text}, nil
}
func (p passthrough) Stream(ctx context.Context, req provider.Request, out chan<- string) error {
	out <- p.text
	close(out)
	return nil
}
func (p passthrough) Embed(ctx context.Context, text string) ([]float64, error) {
	return []float64{0}, nil
}
func (p passthrough) Ping(ctx context.Context) error { return nil }
func (p passthrough) Name() string                   { return "passthrough" }

func TestDegradedWrapper_PassthroughWhenDisabled(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{text: "real answer"})
	r, _ := w.Generate(context.Background(), provider.Request{Prompt: "x"})
	if r.Text != "real answer" {
		t.Errorf("Text = %q, want real answer", r.Text)
	}
}

func TestDegradedWrapper_GarbageWhenEnabled(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{text: "real answer"})
	w.Enable()
	r, _ := w.Generate(context.Background(), provider.Request{Prompt: "x"})
	if r.Text == "real answer" {
		t.Errorf("expected degraded text, got real")
	}
	// Degraded responses must contain hallucination markers so the semantic CB trips.
	lower := strings.ToLower(r.Text)
	if !strings.Contains(lower, "as an ai") &&
		!strings.Contains(lower, "i cannot") &&
		!strings.Contains(lower, "i'm just an ai") {
		t.Errorf("degraded text lacks hallucination marker: %q", r.Text)
	}
}

func TestDegradedWrapper_DisableRestores(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{text: "real answer"})
	w.Enable()
	w.Disable()
	r, _ := w.Generate(context.Background(), provider.Request{Prompt: "x"})
	if r.Text != "real answer" {
		t.Errorf("Text = %q after Disable, want real answer", r.Text)
	}
}

func TestDegradedWrapper_StreamPassthroughClosesChannel(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{text: "tok"})
	out := make(chan string, 4)
	if err := w.Stream(context.Background(), provider.Request{}, out); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collected := []string{}
	for s := range out {
		collected = append(collected, s)
	}
	if len(collected) != 1 || collected[0] != "tok" {
		t.Errorf("collected = %v, want [tok]", collected)
	}
}

func TestDegradedWrapper_NameDelegatesToInner(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{})
	if w.Name() != "passthrough" {
		t.Errorf("Name = %q, want passthrough", w.Name())
	}
}
