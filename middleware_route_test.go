package opentelemetry

import (
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// serveThroughMiddleware runs one request through FastHTTPMiddleware with an
// in-memory span recorder and returns the single ended span.
func serveThroughMiddleware(t *testing.T, method, uri string, handler fasthttp.RequestHandler) sdktrace.ReadOnlySpan {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI(uri)
	ctx.Request.Header.SetMethod(method)
	FastHTTPMiddleware("test-service")(handler)(ctx)

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	return spans[0]
}

func spanAttr(span sdktrace.ReadOnlySpan, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// AC-1: a route template set by the handler names the span and sets http.route,
// so all requests to one endpoint group under one low-cardinality name.
func TestFastHTTPMiddleware_RouteFromHandlerNamesSpan(t *testing.T) {
	span := serveThroughMiddleware(t, "GET", "/users/123", func(ctx *fasthttp.RequestCtx) {
		SetHTTPRoute(ctx, "/users/{id}")
	})

	if span.Name() != "GET /users/{id}" {
		t.Errorf("span name = %q, want %q", span.Name(), "GET /users/{id}")
	}
	if v, ok := spanAttr(span, "http.route"); !ok || v.AsString() != "/users/{id}" {
		t.Errorf("http.route = %v (set=%v), want /users/{id}", v.AsString(), ok)
	}
}

// AC-1: fasthttp/router stores the matched route under a randomized
// "__matchedRoutePath::<rand>__" user-value key when SaveMatchedRoutePath is on;
// the middleware picks it up without importing the router.
func TestFastHTTPMiddleware_RouteFromFastHTTPRouterNamesSpan(t *testing.T) {
	span := serveThroughMiddleware(t, "POST", "/orders/42/items", func(ctx *fasthttp.RequestCtx) {
		ctx.SetUserValue("__matchedRoutePath::k3yR4nd0m__", "/orders/{orderID}/items")
	})

	if span.Name() != "POST /orders/{orderID}/items" {
		t.Errorf("span name = %q, want %q", span.Name(), "POST /orders/{orderID}/items")
	}
	if v, ok := spanAttr(span, "http.route"); !ok || v.AsString() != "/orders/{orderID}/items" {
		t.Errorf("http.route = %v (set=%v), want /orders/{orderID}/items", v.AsString(), ok)
	}
}

// AC-2: without a route template, ID-like path segments are replaced with {id}
// in the span name only; http.route stays unset because it is not a real template.
func TestFastHTTPMiddleware_NoRoute_TemplatesIDSegmentsInSpanName(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{"numeric id", "/users/123", "GET /users/{id}"},
		{"uuid", "/orders/3f2b8c1e-9a4d-4e2b-8c3a-1f2e3d4c5b6a/items", "GET /orders/{id}/items"},
		{"uppercase uuid", "/orders/3F2B8C1E-9A4D-4E2B-8C3A-1F2E3D4C5B6A", "GET /orders/{id}"},
		{"16+ hex chars", "/blobs/deadbeefcafebabe0123", "GET /blobs/{id}"},
		{"several ids", "/users/7/posts/99", "GET /users/{id}/posts/{id}"},
		{"slug kept", "/users/john-smith", "GET /users/john-smith"},
		{"short hex word kept", "/api/v1/facade", "GET /api/v1/facade"},
		{"15 hex chars kept", "/blobs/deadbeefcafebab", "GET /blobs/deadbeefcafebab"},
		{"query ignored", "/users/123?token=x", "GET /users/{id}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := serveThroughMiddleware(t, "GET", tt.uri, func(ctx *fasthttp.RequestCtx) {})
			if span.Name() != tt.want {
				t.Errorf("span name = %q, want %q", span.Name(), tt.want)
			}
			if v, ok := spanAttr(span, "http.route"); ok {
				t.Errorf("http.route must not be set without a route template, got %q", v.AsString())
			}
		})
	}
}

// AC-3: when the request target is not a path that can be templated (e.g. the
// "OPTIONS *" server-wide form), the span is named after the method only.
func TestFastHTTPMiddleware_NoTemplatablePath_SpanNameIsMethod(t *testing.T) {
	span := serveThroughMiddleware(t, "OPTIONS", "*", func(ctx *fasthttp.RequestCtx) {})
	if span.Name() != "OPTIONS" {
		t.Errorf("span name = %q, want %q", span.Name(), "OPTIONS")
	}
}
