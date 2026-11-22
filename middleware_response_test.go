package opentelemetry

import (
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// AC-17: http.response.body.size is the size of the body the handler set.
// For a streamed body it is the declared Content-Length, and it is left out
// when the size is unknown (chunked); the stream itself is not read.
func TestFastHTTPMiddleware_ResponseBodySize(t *testing.T) {
	setBody := serveThroughMiddleware(t, "GET", "/x", func(ctx *fasthttp.RequestCtx) {
		ctx.SetBody([]byte("hello world"))
	})
	if v, ok := spanAttr(setBody, "http.response.body.size"); !ok || v.AsInt64() != 11 {
		t.Errorf("SetBody: body.size = %d (set=%v), want 11", v.AsInt64(), ok)
	}

	empty := serveThroughMiddleware(t, "GET", "/x", func(ctx *fasthttp.RequestCtx) {})
	if v, ok := spanAttr(empty, "http.response.body.size"); !ok || v.AsInt64() != 0 {
		t.Errorf("empty body: body.size = %d (set=%v), want 0", v.AsInt64(), ok)
	}

	var sized *fasthttp.RequestCtx
	stream := serveThroughMiddleware(t, "GET", "/x", func(ctx *fasthttp.RequestCtx) {
		sized = ctx
		ctx.SetBodyStream(strings.NewReader("12345"), 5)
	})
	if v, ok := spanAttr(stream, "http.response.body.size"); !ok || v.AsInt64() != 5 {
		t.Errorf("sized stream: body.size = %d (set=%v), want 5", v.AsInt64(), ok)
	}
	if !sized.Response.IsBodyStream() {
		t.Error("the middleware must not drain a streamed body")
	}

	chunked := serveThroughMiddleware(t, "GET", "/x", func(ctx *fasthttp.RequestCtx) {
		ctx.SetBodyStream(strings.NewReader("12345"), -1)
	})
	if v, ok := spanAttr(chunked, "http.response.body.size"); ok {
		t.Errorf("chunked stream: body.size must not be set, got %d", v.AsInt64())
	}

	// No body is sent for HEAD or for 1xx/204/304, whatever the handler set.
	noBody := []struct {
		name, method string
		status       int
		skipBody     bool
	}{
		{"HEAD", "HEAD", fasthttp.StatusOK, false},
		{"204", "GET", fasthttp.StatusNoContent, false},
		{"304", "GET", fasthttp.StatusNotModified, false},
		{"1xx", "GET", fasthttp.StatusEarlyHints, false},
		{"SkipBody", "GET", fasthttp.StatusOK, true},
	}
	for _, tt := range noBody {
		span := serveThroughMiddleware(t, tt.method, "/x", func(ctx *fasthttp.RequestCtx) {
			ctx.SetStatusCode(tt.status)
			ctx.SetBody([]byte("hello world"))
			ctx.Response.SkipBody = tt.skipBody
		})
		if v, ok := spanAttr(span, "http.response.body.size"); !ok || v.AsInt64() != 0 {
			t.Errorf("%s: body.size = %d (set=%v), want 0 (no body is sent)", tt.name, v.AsInt64(), ok)
		}
	}
}

// AC-18: when the handler panics, the span still gets the route name, Error
// status and one exception event, and the panic keeps propagating.
func TestFastHTTPMiddleware_HandlerPanic_RecordedAndRepanicked(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/users/123")
	ctx.Request.Header.SetMethod("GET")
	handler := FastHTTPMiddleware("test-service")(func(ctx *fasthttp.RequestCtx) {
		SetHTTPRoute(ctx, "/users/{userID}")
		panic("boom")
	})

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler(ctx)
	}()
	if recovered != "boom" {
		t.Fatalf("the panic must propagate unchanged, recovered %v", recovered)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}
	span := spans[0]
	if span.Name() != "GET /users/{userID}" {
		t.Errorf("span name = %q, want %q", span.Name(), "GET /users/{userID}")
	}
	if v, _ := spanAttr(span, "http.route"); v.AsString() != "/users/{userID}" {
		t.Errorf("http.route = %q, want /users/{userID}", v.AsString())
	}
	if span.Status().Code != codes.Error {
		t.Errorf("status = %v, want Error", span.Status().Code)
	}
	if _, ok := spanAttr(span, "http.response.status_code"); ok {
		t.Error("http.response.status_code must not be set: no response was written")
	}
	if n := countExceptionEvents(span); n != 1 {
		t.Errorf("want exactly 1 exception event, got %d", n)
	}
	for _, ev := range span.Events() {
		for _, kv := range ev.Attributes {
			if ev.Name == "exception" && kv.Key == "exception.message" && kv.Value.AsString() != "boom" {
				t.Errorf("exception.message = %q, want boom", kv.Value.AsString())
			}
		}
	}
}

func countExceptionEvents(span sdktrace.ReadOnlySpan) int {
	n := 0
	for _, ev := range span.Events() {
		if ev.Name == "exception" {
			n++
		}
	}
	return n
}
