package quality

import (
	"context"
	"math"
)

// Embedder generates a vector embedding for a text. Function-type form so callers
// can pass either an interface method value (e.g. provider.Embedder.Embed) or
// an inline closure for tests.
type Embedder func(ctx context.Context, text string) ([]float64, error)

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
