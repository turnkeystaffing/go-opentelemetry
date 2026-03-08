package opentelemetry

import (
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// NewSlogHandler creates an OTel-instrumented slog.Handler that automatically
// correlates log records with active trace spans. Requires InitializeProvider()
// to have been called first so the global LoggerProvider is set.
//
// Usage:
//
//	logger := slog.New(opentelemetry.NewSlogHandler("my-service"))
//	logger.InfoContext(ctx, "processing", "key", "value")
func NewSlogHandler(serviceName string) slog.Handler {
	return otelslog.NewHandler(serviceName)
}
