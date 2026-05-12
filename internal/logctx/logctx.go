// Package logctx attaches a *slog.Logger to a context.Context.
// Use With at request entry to bind trace_id; use From inside any function
// on the request path to retrieve the contextualised logger.
package logctx

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

// With returns a derived context that carries log. Idempotent: calling With
// twice with the same logger does not duplicate entries.
func With(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, log)
}

// From retrieves the *slog.Logger previously attached with With.
// Returns slog.Default() if none is attached — so call sites never need
// a nil check.
func From(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && log != nil {
		return log
	}
	return slog.Default()
}
