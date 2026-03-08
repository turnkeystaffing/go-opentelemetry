package opentelemetry

import (
	"context"
	"fmt"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// FastHTTPMiddleware creates OpenTelemetry tracing middleware for FastHTTP
// It extracts trace context from incoming requests, creates spans, and injects context for downstream processing
func FastHTTPMiddleware(serviceName string) func(fasthttp.RequestHandler) fasthttp.RequestHandler {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(ctx *fasthttp.RequestCtx) {
			// Extract trace context from incoming headers
			reqCtx := propagator.Extract(context.Background(), &FastHTTPHeaderCarrier{ctx})

			// Create span for the HTTP request
			spanName := fmt.Sprintf("%s %s", string(ctx.Method()), string(ctx.Path()))
			reqCtx, span := tracer.Start(reqCtx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(string(ctx.Method())),
					semconv.URLPath(string(ctx.URI().RequestURI())),
					semconv.URLScheme(string(ctx.URI().Scheme())),
					semconv.URLDomain(string(ctx.URI().Host())),
					semconv.UserAgentOriginal(string(ctx.UserAgent())),
					semconv.HTTPRoute(string(ctx.Path())),
				),
			)
			defer span.End()

			// Store the trace context in FastHTTP user values for downstream access
			ctx.SetUserValue("traceContext", reqCtx)
			ctx.SetUserValue("traceID", span.SpanContext().TraceID().String())
			ctx.SetUserValue("spanID", span.SpanContext().SpanID().String())

			// INTERNAL TRACE HEADERS: Store trace context in custom headers for database persistence
			// These are NOT the standard W3C traceparent headers used for external service communication
			// Purpose: Workers reconstruct trace context from these stored headers when processing webhooks
			// Format: Custom x-trace-* headers that are easier to store/parse from database
			if span.SpanContext().IsValid() {
				ctx.Request.Header.Set("x-trace-trace_id", span.SpanContext().TraceID().String())
				ctx.Request.Header.Set("x-trace-span_id", span.SpanContext().SpanID().String())
				if span.SpanContext().TraceFlags().IsSampled() {
					ctx.Request.Header.Set("x-trace-trace_flags", "01")
				} else {
					ctx.Request.Header.Set("x-trace-trace_flags", "00")
				}
			}

			// Add trace ID to response headers for debugging/correlation
			ctx.Response.Header.Set("X-Trace-ID", span.SpanContext().TraceID().String())

			// Call the next handler
			next(ctx)

			// Set span status based on HTTP status code
			statusCode := ctx.Response.StatusCode()
			span.SetAttributes(
				semconv.HTTPResponseStatusCode(statusCode),
				semconv.HTTPResponseBodySize(ctx.Response.Header.ContentLength()),
			)

			// Set span status
			if statusCode >= 400 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
				if statusCode >= 500 {
					span.RecordError(fmt.Errorf("HTTP %d: %s", statusCode, fasthttp.StatusMessage(statusCode)))
				}
			} else {
				span.SetStatus(codes.Ok, "")
			}
		}
	}
}

// FastHTTPHeaderCarrier adapts FastHTTP headers for OpenTelemetry propagation
type FastHTTPHeaderCarrier struct {
	ctx *fasthttp.RequestCtx
}

// Get retrieves a value from the carrier
func (c *FastHTTPHeaderCarrier) Get(key string) string {
	return string(c.ctx.Request.Header.Peek(key))
}

// Set stores a value in the carrier
func (c *FastHTTPHeaderCarrier) Set(key, value string) {
	c.ctx.Request.Header.Set(key, value)
}

// Keys returns all keys in the carrier
func (c *FastHTTPHeaderCarrier) Keys() []string {
	keys := make([]string, 0)
	for key, _ := range c.ctx.Request.Header.All() {
		keys = append(keys, string(key))
	}
	return keys
}

// GetTraceContext retrieves the trace context from FastHTTP user values
// This helper function can be used by handlers and processors to access the trace context
func GetTraceContext(ctx *fasthttp.RequestCtx) context.Context {
	if traceCtx := ctx.UserValue("traceContext"); traceCtx != nil {
		if c, ok := traceCtx.(context.Context); ok {
			return c
		}
	}
	return context.Background()
}

// GetTraceID retrieves the trace ID from FastHTTP user values
func GetTraceID(ctx *fasthttp.RequestCtx) string {
	if traceID := ctx.UserValue("traceID"); traceID != nil {
		if id, ok := traceID.(string); ok {
			return id
		}
	}
	return ""
}

// GetSpanID retrieves the span ID from FastHTTP user values
func GetSpanID(ctx *fasthttp.RequestCtx) string {
	if spanID := ctx.UserValue("spanID"); spanID != nil {
		if id, ok := spanID.(string); ok {
			return id
		}
	}
	return ""
}

// InjectTraceHeaders injects trace context into outbound HTTP requests
// This should be used when making HTTP calls to external services
func InjectTraceHeaders(ctx context.Context, headers map[string]string) {
	propagator := otel.GetTextMapPropagator()
	carrier := &MapCarrier{headers: headers}
	propagator.Inject(ctx, carrier)
}

// MapCarrier is a simple map-based carrier for trace context propagation
type MapCarrier struct {
	headers map[string]string
}

// Get retrieves a value from the carrier
func (c *MapCarrier) Get(key string) string {
	return c.headers[key]
}

// Set stores a value in the carrier
func (c *MapCarrier) Set(key, value string) {
	c.headers[key] = value
}

// Keys returns all keys in the carrier
func (c *MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for k := range c.headers {
		keys = append(keys, k)
	}
	return keys
}

// StartSpan creates a new span from the given context
// This is a convenience function for creating spans in handlers and processors
func StartSpan(ctx context.Context, operationName string, attributes ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer("get-native-auth")
	return tracer.Start(ctx, operationName, trace.WithAttributes(attributes...))
}

// AddCustomAttributes adds custom attributes to the current span if one exists
func AddCustomAttributes(ctx context.Context, attributes ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attributes...)
	}
}

// RecordError records an error on the current span if one exists
func RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// SetSpanStatus sets the status of the current span if one exists
func SetSpanStatus(ctx context.Context, code codes.Code, description string) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetStatus(code, description)
	}
}
