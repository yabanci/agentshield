package provider_test

import (
	"context"
	"testing"

	"github.com/yabanci/agentshield/provider"
)

// stubProvider asserts that an arbitrary type can satisfy LLMProvider.
type stubProvider struct{}

func (stubProvider) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{Text: "stub"}, nil
}
func (stubProvider) Stream(ctx context.Context, req provider.Request, out chan<- string) error {
	close(out)
	return nil
}
func (stubProvider) Embed(ctx context.Context, text string) ([]float64, error) { return nil, nil }
func (stubProvider) Ping(ctx context.Context) error                            { return nil }
func (stubProvider) Name() string                                              { return "stub" }

// stubEmbedder asserts that Embedder is satisfiable independently.
type stubEmbedder struct{}

func (stubEmbedder) Embed(ctx context.Context, text string) ([]float64, error) { return nil, nil }

func TestInterfaceShape(t *testing.T) {
	var _ provider.LLMProvider = stubProvider{}
	var _ provider.Embedder = stubEmbedder{}
	var _ provider.Embedder = stubProvider{} // LLMProvider should also satisfy Embedder
}
