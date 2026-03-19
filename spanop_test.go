package opentelemetry

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

// --- SpanOp tests ---

func TestSpanOp_Success(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	result, err := SpanOp[string](context.Background(), tracer, "test.op",
		func(ctx context.Context, span trace.Span) (string, error) {
			return "hello", nil
		},
		WithAttributes(attribute.String("key", "value")),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Fatalf("expected 'hello', got %q", result)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name() != "test.op" {
		t.Errorf("expected span name 'test.op', got %q", span.Name())
	}
	if span.Status().Code != codes.Ok {
		t.Errorf("expected status Ok, got %v", span.Status().Code)
	}

	attrMap := make(map[string]interface{})
	for _, a := range span.Attributes() {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}
	if attrMap["key"] != "value" {
		t.Errorf("expected attribute key=value, got %v", attrMap["key"])
	}
}

func TestSpanOp_Error(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	testErr := errors.New("something failed")
	result, err := SpanOp[string](context.Background(), tracer, "test.op",
		func(ctx context.Context, span trace.Span) (string, error) {
			return "", testErr
		},
	)

	if !errors.Is(err, testErr) {
		t.Fatalf("expected testErr, got %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("expected status Error, got %v", span.Status().Code)
	}
	if len(span.Events()) == 0 {
		t.Error("expected error event to be recorded")
	}
}

func TestSpanOp_WithDurationHistogram(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	h := NewFloat64Histogram(meter, "test.duration", "test duration", "ms", testLogger())

	_, err := SpanOp[string](context.Background(), tracer, "test.op",
		func(ctx context.Context, span trace.Span) (string, error) {
			return "ok", nil
		},
		WithDurationHistogram(h),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "test.duration" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected test.duration histogram to be recorded")
	}
}

func TestSpanOp_CallbackReceivesSpanEnrichedContext(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	var capturedCtx context.Context
	_, _ = SpanOp[struct{}](context.Background(), tracer, "test.op",
		func(ctx context.Context, span trace.Span) (struct{}, error) {
			capturedCtx = ctx
			return struct{}{}, nil
		},
	)

	// The context passed to the callback should contain the span
	span := trace.SpanFromContext(capturedCtx)
	if !span.SpanContext().IsValid() {
		t.Error("expected callback context to contain a valid span")
	}
}

func TestSpanOp_PostExecutionAttributes(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	_, _ = SpanOp[string](context.Background(), tracer, "test.op",
		func(ctx context.Context, span trace.Span) (string, error) {
			span.SetAttributes(attribute.String("post.key", "post.value"))
			return "ok", nil
		},
		WithAttributes(attribute.String("pre.key", "pre.value")),
	)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	attrMap := make(map[string]interface{})
	for _, a := range spans[0].Attributes() {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}
	if attrMap["pre.key"] != "pre.value" {
		t.Error("expected pre-execution attribute")
	}
	if attrMap["post.key"] != "post.value" {
		t.Error("expected post-execution attribute set inside callback")
	}
}

// --- SpanOpVoid tests ---

func TestSpanOpVoid_Success(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	called := false
	err := SpanOpVoid(context.Background(), tracer, "test.void",
		func(ctx context.Context, span trace.Span) error {
			called = true
			return nil
		},
		WithAttributes(attribute.String("op", "void")),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected callback to be called")
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "test.void" {
		t.Errorf("expected span name 'test.void', got %q", spans[0].Name())
	}
	if spans[0].Status().Code != codes.Ok {
		t.Errorf("expected status Ok, got %v", spans[0].Status().Code)
	}
}

func TestSpanOpVoid_Error(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	testErr := errors.New("void failed")
	err := SpanOpVoid(context.Background(), tracer, "test.void",
		func(ctx context.Context, span trace.Span) error {
			return testErr
		},
	)

	if !errors.Is(err, testErr) {
		t.Fatalf("expected testErr, got %v", err)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Errorf("expected status Error, got %v", spans[0].Status().Code)
	}
}

// --- SanitizeSpanAttribute tests ---

func TestSanitizeSpanAttribute_NoTruncation(t *testing.T) {
	result := SanitizeSpanAttribute("short", 10)
	if result != "short" {
		t.Errorf("expected 'short', got %q", result)
	}
}

func TestSanitizeSpanAttribute_ExactLength(t *testing.T) {
	result := SanitizeSpanAttribute("12345", 5)
	if result != "12345" {
		t.Errorf("expected '12345', got %q", result)
	}
}

func TestSanitizeSpanAttribute_Truncated(t *testing.T) {
	result := SanitizeSpanAttribute("this is a long string", 7)
	if result != "this is" {
		t.Errorf("expected 'this is', got %q", result)
	}
	if len(result) != 7 {
		t.Errorf("expected length 7, got %d", len(result))
	}
}

func TestSanitizeSpanAttribute_Empty(t *testing.T) {
	result := SanitizeSpanAttribute("", 10)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// --- NewFloat64Histogram tests ---

func TestNewFloat64Histogram_Success(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	h := NewFloat64Histogram(meter, "test.hist", "a test histogram", "ms", testLogger())
	if h == nil {
		t.Fatal("expected histogram to be created")
	}

	h.Record(context.Background(), 42.0)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "test.hist" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected test.hist metric to be recorded")
	}
}

// --- WithAttributes tests ---

func TestWithAttributes_Multiple(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	_, _ = SpanOp[struct{}](context.Background(), tracer, "test.op",
		func(ctx context.Context, span trace.Span) (struct{}, error) {
			return struct{}{}, nil
		},
		WithAttributes(attribute.String("a", "1")),
		WithAttributes(attribute.String("b", "2"), attribute.Int("c", 3)),
	)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	attrMap := make(map[string]interface{})
	for _, a := range spans[0].Attributes() {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}

	if attrMap["a"] != "1" {
		t.Error("expected attribute a=1")
	}
	if attrMap["b"] != "2" {
		t.Error("expected attribute b=2")
	}
	if attrMap["c"] != int64(3) {
		t.Error("expected attribute c=3")
	}
}

func TestSpanOp_NoOptions(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	result, err := SpanOp[int](context.Background(), tracer, "test.bare",
		func(ctx context.Context, span trace.Span) (int, error) {
			return 42, nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "test.bare" {
		t.Errorf("expected span name 'test.bare', got %q", spans[0].Name())
	}
}
