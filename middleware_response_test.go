package opentelemetry

import (
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
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
}
