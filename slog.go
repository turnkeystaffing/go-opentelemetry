package opentelemetry

import (
	"log/slog"
	"os"
	"strings"

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

// SlogConfig configures the multi-handler logger setup.
type SlogConfig struct {
	// ServiceName is used for the OTel log handler scope (required when OTelEnabled).
	ServiceName string
	// Level is the minimum log level for all handlers.
	Level slog.Level
	// Format selects the stdout handler: "json" (default) or "text".
	Format string
	// OTelEnabled enables fan-out to both stdout and the OTel pipeline.
	// When false, logs go to stdout only.
	OTelEnabled bool
}

// NewMultiSlogHandler creates a production-ready slog.Handler that always writes
// to stdout and optionally fans out to the OTel log pipeline.
//
// When OTelEnabled is true, returns a MultiHandler that writes to:
//   - stdout: JSON/text handler with AddSource, suitable for wrapping with trace context injection
//   - OTel:   otelslog handler filtered by Level (otelslog emits all levels natively)
//
// When OTelEnabled is false, returns the stdout handler only.
//
// Callers can wrap the returned handler with additional middleware (PII masking,
// trace context injection, etc.) before passing to slog.New().
//
// Usage:
//
//	handler := opentelemetry.NewMultiSlogHandler(opentelemetry.SlogConfig{
//	    ServiceName: "email-service",
//	    Level:       slog.LevelInfo,
//	    Format:      "json",
//	    OTelEnabled: true,
//	})
//	logger := slog.New(handler)
func NewMultiSlogHandler(cfg SlogConfig) slog.Handler {
	opts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: true,
	}

	var stdoutHandler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		stdoutHandler = slog.NewTextHandler(os.Stdout, opts)
	default:
		stdoutHandler = slog.NewJSONHandler(os.Stdout, opts)
	}

	if !cfg.OTelEnabled {
		return stdoutHandler
	}

	otelBase := otelslog.NewHandler(cfg.ServiceName)
	otelFiltered := NewLevelFilterHandler(cfg.Level, otelBase)
	return NewMultiHandler(stdoutHandler, otelFiltered)
}
