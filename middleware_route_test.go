package opentelemetry

import (
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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

// AC-4: url.path carries only the path; the query string (which may hold
// tokens) goes to url.query, and only when present.
func TestFastHTTPMiddleware_URLPathExcludesQuery(t *testing.T) {
	span := serveThroughMiddleware(t, "GET", "/users/123?token=x", func(ctx *fasthttp.RequestCtx) {})
	if v, _ := spanAttr(span, "url.path"); v.AsString() != "/users/123" {
		t.Errorf("url.path = %q, want /users/123", v.AsString())
	}
	if v, ok := spanAttr(span, "url.query"); !ok || v.AsString() != "token=REDACTED" {
		t.Errorf("url.query = %q (set=%v), want token=REDACTED", v.AsString(), ok)
	}

	noQuery := serveThroughMiddleware(t, "GET", "/users/123", func(ctx *fasthttp.RequestCtx) {})
	if _, ok := spanAttr(noQuery, "url.query"); ok {
		t.Error("url.query must not be set when the request has no query")
	}
}

// AC-5: server spans are Error only for 5xx; 4xx is the client's problem and
// 2xx/3xx are success, so both leave the status Unset per semconv.
func TestFastHTTPMiddleware_StatusFollowsServerSemconv(t *testing.T) {
	tests := []struct {
		status int
		want   codes.Code
	}{
		{fasthttp.StatusOK, codes.Unset},
		{fasthttp.StatusFound, codes.Unset},
		{fasthttp.StatusNotFound, codes.Unset},
		{fasthttp.StatusBadRequest, codes.Unset},
		{fasthttp.StatusInternalServerError, codes.Error},
		{fasthttp.StatusServiceUnavailable, codes.Error},
	}
	for _, tt := range tests {
		t.Run(fasthttp.StatusMessage(tt.status), func(t *testing.T) {
			span := serveThroughMiddleware(t, "GET", "/x", func(ctx *fasthttp.RequestCtx) {
				ctx.SetStatusCode(tt.status)
			})
			if span.Status().Code != tt.want {
				t.Errorf("status %d: span status = %v, want %v", tt.status, span.Status().Code, tt.want)
			}
			wantEvents := 0
			if tt.want == codes.Error {
				wantEvents = 1 // the 5xx is recorded as an exception event
			}
			if got := len(span.Events()); got != wantEvents || (got == 1 && span.Events()[0].Name != "exception") {
				t.Errorf("status %d: events = %v, want %d exception event(s)", tt.status, span.Events(), wantEvents)
			}
		})
	}
}

// AC-14: a 404 with no route template (scanners, typos, unknown endpoints) is
// named after the method only, so unmatched paths can't create one span name
// per path or put path tokens into span names.
func TestFastHTTPMiddleware_NotFoundWithoutRoute_SpanNameIsMethod(t *testing.T) {
	span := serveThroughMiddleware(t, "GET", "/wp-admin/x7f9q/setup.php", func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusNotFound)
	})
	if span.Name() != "GET" {
		t.Errorf("span name = %q, want %q", span.Name(), "GET")
	}

	// A 404 from a matched route (e.g. "user not found") keeps the route name.
	routed := serveThroughMiddleware(t, "GET", "/users/123", func(ctx *fasthttp.RequestCtx) {
		SetHTTPRoute(ctx, "/users/{id}")
		ctx.SetStatusCode(fasthttp.StatusNotFound)
	})
	if routed.Name() != "GET /users/{id}" {
		t.Errorf("routed 404 span name = %q, want %q", routed.Name(), "GET /users/{id}")
	}
}

// AC-15: values of sensitive query parameters are replaced with REDACTED in
// url.query (keys matched case-insensitively); other parameters stay readable.
func TestFastHTTPMiddleware_URLQueryRedactsSensitiveValues(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"plain params kept", "page=2&sort=name", "page=2&sort=name"},
		{"token", "token=abc&page=2", "token=REDACTED&page=2"},
		{"oauth callback", "code=xyz&state=s1", "code=REDACTED&state=s1"},
		{"case-insensitive key", "Access_Token=abc", "Access_Token=REDACTED"},
		{"aws presigned", "X-Amz-Signature=deadbeef&X-Amz-Date=20260101", "X-Amz-Signature=REDACTED&X-Amz-Date=20260101"},
		{"gcs and azure sig", "X-Goog-Signature=a&sig=b&Signature=c&AWSAccessKeyId=d", "X-Goog-Signature=REDACTED&sig=REDACTED&Signature=REDACTED&AWSAccessKeyId=REDACTED"},
		{"api key and password", "api_key=k&apikey=k2&password=p&client_secret=s", "api_key=REDACTED&apikey=REDACTED&password=REDACTED&client_secret=REDACTED"},
		{"percent-encoded key", "acc%65ss_token=abc", "acc%65ss_token=REDACTED"},
		{"key without value kept", "token&debug", "token&debug"},
		{"empty value kept", "token=", "token="},
		{"repeated sensitive key", "token=a&token=b", "token=REDACTED&token=REDACTED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := serveThroughMiddleware(t, "GET", "/cb?"+tt.query, func(ctx *fasthttp.RequestCtx) {})
			if v, _ := spanAttr(span, "url.query"); v.AsString() != tt.want {
				t.Errorf("url.query = %q, want %q", v.AsString(), tt.want)
			}
		})
	}
}

// AC-1: an explicit SetHTTPRoute wins over fasthttp/router's matched route, and
// an empty SetHTTPRoute falls back to the router's route.
func TestFastHTTPMiddleware_RouteSourcePrecedence(t *testing.T) {
	const routerKey = "__matchedRoutePath::k3yR4nd0m__"

	explicit := serveThroughMiddleware(t, "GET", "/users/123", func(ctx *fasthttp.RequestCtx) {
		ctx.SetUserValue(routerKey, "/users/{userID}")
		SetHTTPRoute(ctx, "/users/{id}")
	})
	if explicit.Name() != "GET /users/{id}" {
		t.Errorf("SetHTTPRoute must win: span name = %q, want %q", explicit.Name(), "GET /users/{id}")
	}

	empty := serveThroughMiddleware(t, "GET", "/users/123", func(ctx *fasthttp.RequestCtx) {
		ctx.SetUserValue(routerKey, "/users/{userID}")
		SetHTTPRoute(ctx, "")
	})
	if empty.Name() != "GET /users/{userID}" {
		t.Errorf("empty SetHTTPRoute must fall back to the router route: span name = %q", empty.Name())
	}
}
