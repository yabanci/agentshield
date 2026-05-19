// Package telemetry wires OpenTelemetry tracing for AgentShield.
// All os.Getenv reads for OTel config live in config/env.go — this file is
// pure SDK plumbing; it only reads from the typed OTelConfig struct.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yabanci/agentshield/config"
)

// version is the service version emitted as a resource attribute.
// Set at build time via -ldflags "-X github.com/yabanci/agentshield/telemetry.version=<ver>"
// Default "dev" is fine for local runs and tests.
var version = "dev" //nolint:gochecknoglobals // build-time injection point

// InitOTel bootstraps the OTel SDK.
//
// When cfg.Endpoint is empty, a no-op TracerProvider is installed globally so
// all span-creation calls in the codebase compile and run without branching —
// they just produce no data. A single INFO log line confirms the mode.
//
// When cfg.Endpoint is set, a BatchSpanProcessor with an OTLP/gRPC exporter is
// installed. BSP never blocks the request path: it buffers and exports async.
//
// The returned shutdown func must be called (typically deferred in main) to
// flush buffered spans. It respects a 5-second internal timeout regardless of
// the context passed by the caller.
func InitOTel(ctx context.Context, cfg config.OTelConfig) (func(context.Context) error, error) {
	if cfg.Endpoint == "" {
		slog.Info("OTel tracing disabled — set OTEL_EXPORTER_OTLP_ENDPOINT to enable")
		// The SDK default global provider is already a no-op; nothing to set.
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("agentshield"),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return noop(), fmt.Errorf("otel resource: %w", err)
	}

	dialOpts := []grpc.DialOption{}
	if cfg.Insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithDialOption(dialOpts...),
		otlptracegrpc.WithTimeout(cfg.Timeout),
	)
	if err != nil {
		return noop(), fmt.Errorf("otel exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		// WithBatcher installs a BatchSpanProcessor — async, never blocks the request path.
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// ParentBased(AlwaysSample) preserves upstream sampling decisions
		// (e.g. a load balancer that already sampled the trace).
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)

	otel.SetTracerProvider(tp)
	slog.Info("OTel tracing enabled", "endpoint", cfg.Endpoint, "insecure", cfg.Insecure)
	if cfg.Insecure {
		slog.Warn("OTel exporter is configured INSECURE (plaintext)", slog.String("endpoint", cfg.Endpoint))
	}
	// M3: tool inputs are captured as span attributes (truncated to 2 KB).
	// Review for PII before pointing at a shared collector.
	slog.Info("OTel tracing enabled; tool inputs are captured as span attrs (truncated to 2 KB). Review for PII before pointing at a shared collector.")

	return func(ctx context.Context) error {
		// Give the BSP up to 5 seconds to flush in-flight spans.
		flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(flushCtx)
	}, nil
}

// noop returns a shutdown func that does nothing. Used when setup partially
// fails: the global provider stays as the SDK default (no-op) and the caller
// can safely defer the returned func.
func noop() func(context.Context) error {
	return func(context.Context) error { return nil }
}

// Tracer returns a named tracer from the global provider.
// All packages in AgentShield call this instead of otel.Tracer directly so
// the instrumentation name stays consistent across the service.
func Tracer(name string) trace.Tracer { //nolint:ireturn // interface is the contract
	return otel.Tracer(name)
}
