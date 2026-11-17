package opentelemetry

import (
	"context"
	"fmt"
	"strings"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
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

			// Create span for the HTTP request. The name never contains the raw
			// path: it is "{method} {template}" with ID-like segments replaced,
			// and is refined to the route template after routing (see below).
			spanName := string(ctx.Method())
			if tmpl := templatePath(ctx); tmpl != "" {
				spanName += " " + tmpl
			}
			reqCtx, span := tracer.Start(reqCtx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(string(ctx.Method())),
					semconv.URLPath(string(ctx.URI().Path())),
					semconv.URLScheme(string(ctx.URI().Scheme())),
					semconv.URLDomain(string(ctx.URI().Host())),
					semconv.UserAgentOriginal(string(ctx.UserAgent())),
				),
			)
			defer span.End()

			if q := ctx.URI().QueryString(); len(q) > 0 {
				span.SetAttributes(semconv.URLQuery(string(q)))
			}

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
			// Only set when span has valid trace context (avoids zero-value headers with noop provider)
			if span.SpanContext().IsValid() {
				ctx.Response.Header.Set("X-Trace-ID", span.SpanContext().TraceID().String())
				// W3C traceresponse header for end-to-end trace correlation
				ctx.Response.Header.Set("traceresponse", fmt.Sprintf("00-%s-%s-%s",
					span.SpanContext().TraceID().String(),
					span.SpanContext().SpanID().String(),
					span.SpanContext().TraceFlags().String()))
			}

			// Call the next handler
			next(ctx)

			// Name the span after the route template once routing has run, so
			// span names and http.route stay low-cardinality.
			if route := routeFromCtx(ctx); route != "" {
				span.SetName(string(ctx.Method()) + " " + route)
				span.SetAttributes(semconv.HTTPRoute(route))
			}

			// Set span status based on HTTP status code
			statusCode := ctx.Response.StatusCode()
			span.SetAttributes(
				semconv.HTTPResponseStatusCode(statusCode),
				semconv.HTTPResponseBodySize(ctx.Response.Header.ContentLength()),
			)

			// Server span status per semconv: only 5xx is an error; 4xx is the
			// client's fault and success stays Unset.
			if statusCode >= 500 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", statusCode))
				span.RecordError(fmt.Errorf("HTTP %d: %s", statusCode, fasthttp.StatusMessage(statusCode)))
			}
		}
	}
}

// httpRouteKey is the user-value key under which SetHTTPRoute stores the route template.
type httpRouteKey struct{}

// fasthttpRouterRoutePrefix is the prefix of the user-value key under which
// github.com/fasthttp/router stores the matched route when SaveMatchedRoutePath
// is enabled. The router randomizes the rest of the key at startup, so it is
// matched by prefix instead of importing the router.
const fasthttpRouterRoutePrefix = "__matchedRoutePath::"

// SetHTTPRoute records the low-cardinality route template (e.g. "/users/{id}")
// that matched the request. FastHTTPMiddleware uses it for the span name and
// the http.route attribute. Call it from the handler or router adapter before
// the handler returns.
func SetHTTPRoute(ctx *fasthttp.RequestCtx, route string) {
	ctx.SetUserValue(httpRouteKey{}, route)
}

// routeFromCtx returns the route template set by SetHTTPRoute, or else the one
// stored by github.com/fasthttp/router, or "" when neither is present.
func routeFromCtx(ctx *fasthttp.RequestCtx) string {
	if route, ok := ctx.UserValue(httpRouteKey{}).(string); ok && route != "" {
		return route
	}
	var route string
	ctx.VisitUserValuesAll(func(key, value any) {
		if route != "" {
			return
		}
		if k, ok := key.(string); ok && strings.HasPrefix(k, fasthttpRouterRoutePrefix) {
			if v, ok := value.(string); ok {
				route = v
			}
		}
	})
	return route
}

// templatePath derives a low-cardinality span-name target from the request path
// when no route template is known: path segments that are all digits, a UUID or
// 16+ hex characters become "{id}"; other segments are kept. It returns "" when
// the request target is not a path (e.g. "OPTIONS *").
func templatePath(ctx *fasthttp.RequestCtx) string {
	if string(ctx.Request.Header.RequestURI()) == "*" {
		return ""
	}
	path := string(ctx.Path())
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if isIDSegment(seg) {
			segments[i] = "{id}"
		}
	}
	return strings.Join(segments, "/")
}

// isIDSegment reports whether a path segment looks like an identifier.
func isIDSegment(seg string) bool {
	if seg == "" {
		return false
	}
	if isAllDigits(seg) || isUUID(seg) {
		return true
	}
	return len(seg) >= 16 && isAllHex(seg)
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isAllHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// isUUID reports whether s is in the canonical 8-4-4-4-12 hex form.
func isUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	return isAllHex(s[0:8]) && isAllHex(s[9:13]) && isAllHex(s[14:18]) && isAllHex(s[19:23]) && isAllHex(s[24:36])
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
