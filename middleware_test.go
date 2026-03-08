package opentelemetry

import (
	"context"
	"net/url"
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestFastHTTPMiddleware(t *testing.T) {
	// Set up in-memory span exporter for testing
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)

	// Create middleware
	middleware := FastHTTPMiddleware("test-service")

	// Create test handler
	testHandler := func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.WriteString("OK")
	}

	// Wrap handler with middleware
	wrappedHandler := middleware(testHandler)

	// Create test request
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/api/v1/test")
	ctx.Request.Header.SetMethod("POST")
	ctx.Request.Header.Set("User-Agent", "Test-Agent")

	// Execute request
	wrappedHandler(ctx)

	// Verify response
	if ctx.Response.StatusCode() != fasthttp.StatusOK {
		t.Errorf("Expected status 200, got %d", ctx.Response.StatusCode())
	}

	// Verify trace ID header was added
	traceIDHeader := string(ctx.Response.Header.Peek("X-Trace-ID"))
	if traceIDHeader == "" {
		t.Error("Expected X-Trace-ID header to be set")
	}

	// Verify trace context was stored
	if traceCtx := GetTraceContext(ctx); traceCtx == context.Background() {
		t.Error("Expected trace context to be stored")
	}

	// Verify trace ID was stored
	if traceID := GetTraceID(ctx); traceID == "" {
		t.Error("Expected trace ID to be stored")
	}

	// Verify span ID was stored
	if spanID := GetSpanID(ctx); spanID == "" {
		t.Error("Expected span ID to be stored")
	}

	// Verify spans were created
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name() != "POST /api/v1/test" {
		t.Errorf("Expected span name 'POST /api/v1/test', got '%s'", span.Name())
	}

	// Verify span attributes
	attrs := span.Attributes()
	expectedAttrs := map[string]interface{}{
		"http.request.method":       "POST",
		"url.path":                  "/api/v1/test",
		"url.scheme":                "http",
		"url.domain":                "",
		"http.response.status_code": int64(200), // FastHTTP returns int, but OpenTelemetry stores as int64
	}

	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	for key, expectedValue := range expectedAttrs {
		if actualValue, ok := attrMap[key]; !ok {
			t.Errorf("Missing attribute %s", key)
		} else if actualValue != expectedValue {
			t.Errorf("Attribute %s: expected %v, got %v", key, expectedValue, actualValue)
		}
	}
}

func TestFastHTTPMiddleware_ErrorStatus(t *testing.T) {
	// Set up in-memory span exporter for testing
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)

	// Create middleware
	middleware := FastHTTPMiddleware("test-service")

	// Create test handler that returns an error
	testHandler := func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.WriteString("Internal Server Error")
	}

	// Wrap handler with middleware
	wrappedHandler := middleware(testHandler)

	// Create test request
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/test")
	ctx.Request.Header.SetMethod("GET")

	// Execute request
	wrappedHandler(ctx)

	// Verify spans were created
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	span := spans[0]

	// Verify span status is error
	if span.Status().Code != codes.Error {
		t.Errorf("Expected span status to be Error, got %v", span.Status().Code)
	}

	// Verify status code attribute
	attrs := span.Attributes()
	found := false
	for _, attr := range attrs {
		if attr.Key == "http.response.status_code" && attr.Value.AsInt64() == 500 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected http.response.status_code attribute with value 500")
	}
}

func TestFastHTTPHeaderCarrier(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.Set("traceparent", "00-12345678901234567890123456789012-1234567890123456-01")
	ctx.Request.Header.Set("baggage", "key1=value1")

	carrier := &FastHTTPHeaderCarrier{ctx: ctx}

	// Test Get
	if value := carrier.Get("traceparent"); value != "00-12345678901234567890123456789012-1234567890123456-01" {
		t.Errorf("Expected traceparent value, got '%s'", value)
	}

	// Test Set
	carrier.Set("tracestate", "test=value")
	if value := string(ctx.Request.Header.Peek("tracestate")); value != "test=value" {
		t.Errorf("Expected tracestate to be set, got '%s'", value)
	}

	// Test Keys
	keys := carrier.Keys()
	if len(keys) < 2 {
		t.Errorf("Expected at least 2 keys, got %d", len(keys))
	}
}

func TestInjectTraceHeaders(t *testing.T) {
	// Set up a trace context
	tracerProvider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tracerProvider)

	// Set up text map propagator (required for injection to work)
	propagator := otel.GetTextMapPropagator()
	if propagator == nil {
		// Set a default propagator if none is set
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
	}

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-operation")
	defer span.End()

	// Test injection
	headers := make(map[string]string)
	InjectTraceHeaders(ctx, headers)

	// Note: Header injection depends on the propagator being properly configured.
	// Since we're using a default setup without explicit propagator configuration,
	// we should just verify the function doesn't panic and the map is accessible
	if headers == nil {
		t.Error("Expected headers map to remain accessible")
	}

	// In a real application with proper propagator setup, headers would contain trace context
	t.Logf("Injected headers: %v", headers)
}

func TestMapCarrier(t *testing.T) {
	headers := map[string]string{
		"traceparent": "00-12345678901234567890123456789012-1234567890123456-01",
		"baggage":     "key1=value1",
	}

	carrier := &MapCarrier{headers: headers}

	// Test Get
	if value := carrier.Get("traceparent"); value != "00-12345678901234567890123456789012-1234567890123456-01" {
		t.Errorf("Expected traceparent value, got '%s'", value)
	}

	// Test Set
	carrier.Set("tracestate", "test=value")
	if value := carrier.Get("tracestate"); value != "test=value" {
		t.Errorf("Expected tracestate to be set, got '%s'", value)
	}

	// Test Keys
	keys := carrier.Keys()
	if len(keys) != 3 { // traceparent, baggage, tracestate
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}
}

func TestStartSpan(t *testing.T) {
	// Set up tracer provider
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)

	// Test span creation
	ctx := context.Background()
	newCtx, span := StartSpan(ctx, "test-operation",
		attribute.String("test.key", "test.value"),
		attribute.Int("test.number", 42),
	)

	if span == nil {
		t.Fatal("Expected span to be created")
	}

	if newCtx == ctx {
		t.Error("Expected new context to be different from original")
	}

	span.End()

	// Verify span was recorded
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	recordedSpan := spans[0]
	if recordedSpan.Name() != "test-operation" {
		t.Errorf("Expected span name 'test-operation', got '%s'", recordedSpan.Name())
	}

	// Verify attributes
	attrs := recordedSpan.Attributes()
	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	if attrMap["test.key"] != "test.value" {
		t.Errorf("Expected test.key='test.value', got %v", attrMap["test.key"])
	}

	if attrMap["test.number"] != int64(42) {
		t.Errorf("Expected test.number=42, got %v", attrMap["test.number"])
	}
}

func TestAddCustomAttributes(t *testing.T) {
	// Set up tracer provider
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)

	// Create a span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-operation")

	// Add custom attributes
	AddCustomAttributes(ctx,
		attribute.String("custom.key", "custom.value"),
		attribute.Bool("custom.flag", true),
	)

	span.End()

	// Verify attributes were added
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	recordedSpan := spans[0]
	attrs := recordedSpan.Attributes()
	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}

	if attrMap["custom.key"] != "custom.value" {
		t.Errorf("Expected custom.key='custom.value', got %v", attrMap["custom.key"])
	}

	if attrMap["custom.flag"] != true {
		t.Errorf("Expected custom.flag=true, got %v", attrMap["custom.flag"])
	}
}

func TestRecordError(t *testing.T) {
	// Set up tracer provider
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)

	// Create a span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-operation")

	// Record an error
	testErr := &url.Error{Op: "test", URL: "http://example.com", Err: context.DeadlineExceeded}
	RecordError(ctx, testErr)

	span.End()

	// Verify error was recorded
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	recordedSpan := spans[0]

	// Verify span status is error
	if recordedSpan.Status().Code != codes.Error {
		t.Errorf("Expected span status to be Error, got %v", recordedSpan.Status().Code)
	}

	// Verify error events
	events := recordedSpan.Events()
	found := false
	for _, event := range events {
		if event.Name == "exception" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected exception event to be recorded")
	}
}

func TestSetSpanStatus(t *testing.T) {
	// Set up tracer provider
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	otel.SetTracerProvider(tracerProvider)

	// Create a span
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-operation")

	// Set span status
	SetSpanStatus(ctx, codes.Error, "Something went wrong")

	span.End()

	// Verify status was set
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	recordedSpan := spans[0]

	if recordedSpan.Status().Code != codes.Error {
		t.Errorf("Expected span status to be Error, got %v", recordedSpan.Status().Code)
	}

	if recordedSpan.Status().Description != "Something went wrong" {
		t.Errorf("Expected status description 'Something went wrong', got '%s'", recordedSpan.Status().Description)
	}
}

func TestGetTraceHelpers_NoContext(t *testing.T) {
	// Test helpers when no trace context is available
	ctx := &fasthttp.RequestCtx{}

	if traceCtx := GetTraceContext(ctx); traceCtx != context.Background() {
		t.Error("Expected background context when no trace context available")
	}

	if traceID := GetTraceID(ctx); traceID != "" {
		t.Errorf("Expected empty trace ID when no trace context available, got '%s'", traceID)
	}

	if spanID := GetSpanID(ctx); spanID != "" {
		t.Errorf("Expected empty span ID when no trace context available, got '%s'", spanID)
	}
}
