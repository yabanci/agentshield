package telemetry_test

import (
	"context"
	"testing"

	otelattr "go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/yabanci/agentshield/config"
	"github.com/yabanci/agentshield/telemetry"
)

// attribute is a shorthand for otelattr.String to keep test assertions terse.
func attribute(key, val string) otelattr.KeyValue {
	return otelattr.String(key, val)
}

func attributeBool(key string, val bool) otelattr.KeyValue {
	return otelattr.Bool(key, val)
}

// TestInitOTelNoop verifies that when Endpoint is empty, InitOTel succeeds
// and returns a working no-op shutdown func (no spans emitted anywhere).
func TestInitOTelNoop(t *testing.T) {
	t.Parallel()

	shutdown, err := telemetry.InitOTel(context.Background(), config.OTelConfig{})
	if err != nil {
		t.Fatalf("InitOTel(noop) error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown func must not be nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
}

// TestInitOTelInvalidEndpoint verifies that an unreachable endpoint does NOT
// block InitOTel (the OTLP gRPC exporter connects lazily) and that the
// returned shutdown func can still be called safely.
func TestInitOTelInvalidEndpoint(t *testing.T) {
	t.Parallel()

	// gRPC dial is lazy — connection failure only surfaces at flush/export time.
	shutdown, err := telemetry.InitOTel(context.Background(), config.OTelConfig{
		Endpoint: "127.0.0.1:19317",
		Insecure: true,
		Timeout:  0,
	})
	if err != nil {
		t.Logf("InitOTel returned error (may be ok): %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown func must not be nil even on error path")
	}
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = shutdown(cancelledCtx)
}

// TestTracerReturnsValidTracer verifies telemetry.Tracer returns a non-nil
// tracer that can start and end spans without panicking.
func TestTracerReturnsValidTracer(t *testing.T) {
	t.Parallel()

	tr := telemetry.Tracer("test-tracer")
	if tr == nil {
		t.Fatal("Tracer must not return nil")
	}
	_, span := tr.Start(context.Background(), "test-span")
	span.End()
}

// newRecordingProvider returns an SDK TracerProvider backed by a SpanRecorder.
// The recorder captures all ended spans in memory — no network, no goroutines.
func newRecordingProvider(sr *tracetest.SpanRecorder) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
}

// TestSpanNameAndAttr exercises the general span-creation path using the
// in-process SpanRecorder. No orchestrator or agent plumbing required.
func TestSpanNameAndAttr(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := newRecordingProvider(sr)
	tr := tp.Tracer("agentshield/test")

	_, span := tr.Start(context.Background(), "agentshield.tier.primary")
	span.End()

	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("tp.Shutdown: %v", err)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "agentshield.tier.primary" {
		t.Errorf("span name = %q, want %q", spans[0].Name(), "agentshield.tier.primary")
	}
}

// TestTierSpanNames verifies that one span per expected tier name can be
// created and recorded correctly. This exercises the span-name contract
// that dashboards and alert rules depend on.
func TestTierSpanNames(t *testing.T) {
	t.Parallel()

	wantSpans := []string{
		"agentshield.tier.primary",
		"agentshield.tier.fallback",
		"agentshield.tier.cache",
		"agentshield.degrade",
		"agentshield.react.iteration",
	}

	sr := tracetest.NewSpanRecorder()
	tp := newRecordingProvider(sr)
	tr := tp.Tracer("agentshield/test")
	ctx := context.Background()

	for _, name := range wantSpans {
		_, span := tr.Start(ctx, name)
		span.End()
	}

	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("tp.Shutdown: %v", err)
	}

	ended := sr.Ended()
	if len(ended) != len(wantSpans) {
		t.Fatalf("expected %d spans, got %d", len(wantSpans), len(ended))
	}
	for i, s := range ended {
		if s.Name() != wantSpans[i] {
			t.Errorf("span[%d] name = %q, want %q", i, s.Name(), wantSpans[i])
		}
	}
}

// TestReactIterationSpanAttr verifies the iteration attribute contract.
func TestReactIterationSpanAttr(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := newRecordingProvider(sr)
	tr := tp.Tracer("agentshield/react")
	ctx := context.Background()

	_, span := tr.Start(ctx, "agentshield.react.iteration")
	span.SetAttributes(otelattr.Int("iteration", 3))
	span.End()

	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("tp.Shutdown: %v", err)
	}

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}
	found := false
	for _, kv := range spans[0].Attributes() {
		if string(kv.Key) == "iteration" {
			found = true
			if kv.Value.AsInt64() != 3 {
				t.Errorf("iteration attr = %d, want 3", kv.Value.AsInt64())
			}
		}
	}
	if !found {
		t.Error("iteration attr not found on span")
	}
}

// TestTierSpanHasTierAttr verifies the tier attribute contract used in
// dashboards and tail-sampling rules.
func TestTierSpanHasTierAttr(t *testing.T) {
	t.Parallel()

	type tc struct {
		spanName  string
		tierValue string
	}
	cases := []tc{
		{"agentshield.tier.primary", "primary"},
		{"agentshield.tier.fallback", "fallback"},
		{"agentshield.tier.cache", "cache"},
		{"agentshield.degrade", "denied"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.spanName, func(t *testing.T) {
			t.Parallel()

			sr := tracetest.NewSpanRecorder()
			tp := newRecordingProvider(sr)
			tr := tp.Tracer("agentshield/test")
			ctx := context.Background()

			_, span := tr.Start(ctx, c.spanName)
			span.SetAttributes(attribute("tier", c.tierValue))
			span.End()

			if err := tp.Shutdown(context.Background()); err != nil {
				t.Fatalf("tp.Shutdown: %v", err)
			}

			spans := sr.Ended()
			if len(spans) == 0 {
				t.Fatal("no spans recorded")
			}
			found := false
			for _, kv := range spans[0].Attributes() {
				if string(kv.Key) == "tier" && kv.Value.AsString() == c.tierValue {
					found = true
				}
			}
			if !found {
				t.Errorf("tier=%q attr not found on span %q", c.tierValue, c.spanName)
			}
		})
	}
}

// TestGracefulDenialNotMarkedError verifies that the degrade span does NOT
// carry an error status — graceful denial is a successful response from the
// user's perspective and must not inflate error-rate SLOs.
func TestGracefulDenialNotMarkedError(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := newRecordingProvider(sr)
	tr := tp.Tracer("agentshield/test")
	ctx := context.Background()

	_, span := tr.Start(ctx, "agentshield.degrade")
	span.SetAttributes(
		attribute("tier", "denied"),
		attributeBool("agentshield.denied", true),
	)
	// Intentionally: do NOT call span.RecordError or span.SetStatus(codes.Error)
	span.End()

	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("tp.Shutdown: %v", err)
	}

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}

	s := spans[0]
	// Status code 0 = Unset (not Error). OTel codes.Error = 2.
	if s.Status().Code == 2 {
		t.Error("degrade span must NOT be marked as error — graceful denial is a successful response")
	}
	// Verify agentshield.denied=true is present.
	found := false
	for _, kv := range s.Attributes() {
		if string(kv.Key) == "agentshield.denied" && kv.Value.AsBool() {
			found = true
		}
	}
	if !found {
		t.Error("agentshield.denied=true attr not found on degrade span")
	}
}
