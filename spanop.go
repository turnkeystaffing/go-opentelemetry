package opentelemetry

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// SpanOption configures the behavior of SpanOp and SpanOpVoid.
type SpanOption func(*spanConfig)

type spanConfig struct {
	attrs             []attribute.KeyValue
	durationHistogram metric.Float64Histogram
}

// WithAttributes sets pre-execution span attributes.
func WithAttributes(attrs ...attribute.KeyValue) SpanOption {
	return func(cfg *spanConfig) {
		cfg.attrs = append(cfg.attrs, attrs...)
	}
}

// WithDurationHistogram records the operation duration to the given histogram in milliseconds.
func WithDurationHistogram(h metric.Float64Histogram) SpanOption {
	return func(cfg *spanConfig) {
		cfg.durationHistogram = h
	}
}

// SpanOp executes fn within a new span and returns the result.
// It sets pre-execution attributes, records errors, sets span status, and optionally records duration.
func SpanOp[T any](ctx context.Context, tracer trace.Tracer, spanName string, fn func(context.Context, trace.Span) (T, error), opts ...SpanOption) (T, error) {
	cfg := &spanConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	ctx, span := tracer.Start(ctx, spanName)
	defer span.End()

	if len(cfg.attrs) > 0 {
		span.SetAttributes(cfg.attrs...)
	}

	start := time.Now()
	result, err := fn(ctx, span)
	elapsed := float64(time.Since(start).Milliseconds())

	if cfg.durationHistogram != nil {
		cfg.durationHistogram.Record(ctx, elapsed)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return result, err
}

// SpanOpVoid executes fn within a new span for operations that return only an error.
// It sets pre-execution attributes, records errors, sets span status, and optionally records duration.
func SpanOpVoid(ctx context.Context, tracer trace.Tracer, spanName string, fn func(context.Context, trace.Span) error, opts ...SpanOption) error {
	_, err := SpanOp[struct{}](ctx, tracer, spanName, func(ctx context.Context, span trace.Span) (struct{}, error) {
		return struct{}{}, fn(ctx, span)
	}, opts...)
	return err
}

// SanitizeSpanAttribute truncates a string value to maxLen to prevent bloated span attributes.
func SanitizeSpanAttribute(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}

// NewFloat64Histogram creates a Float64Histogram with the given parameters.
// Logs errors instead of panicking to allow graceful degradation.
func NewFloat64Histogram(meter metric.Meter, name string, description string, unit string, logger *slog.Logger) metric.Float64Histogram {
	h, err := meter.Float64Histogram(name,
		metric.WithDescription(description),
		metric.WithUnit(unit),
	)
	if err != nil {
		logger.Error("failed to create histogram", "name", name, "error", err)
	}
	return h
}
