package opentelemetry

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/valyala/fasthttp"
)

// AC-20: invalid UTF-8 in the decoded path or the host is replaced with U+FFFD
// before it reaches the span name, url.path or url.domain.
func TestFastHTTPMiddleware_InvalidUTF8_Replaced(t *testing.T) {
	span := serveThroughMiddleware(t, "GET", "/caf%FF/x", func(ctx *fasthttp.RequestCtx) {})
	if got, want := span.Name(), "GET /caf�/x"; got != want {
		t.Errorf("span name = %q, want %q", got, want)
	}
	if v, _ := spanAttr(span, "url.path"); v.AsString() != "/caf�/x" {
		t.Errorf("url.path = %q, want %q", v.AsString(), "/caf�/x")
	}

	host := serveThroughMiddleware(t, "GET", "http://ex\xffample.com/x", func(ctx *fasthttp.RequestCtx) {})
	if v, _ := spanAttr(host, "url.domain"); !utf8.ValidString(v.AsString()) || !strings.Contains(v.AsString(), "�") {
		t.Errorf("url.domain = %q, want invalid bytes replaced with U+FFFD", v.AsString())
	}
}

// AC-21: url.domain is cut to 255 characters and url.path (and the span name
// derived from it) to 1024, without splitting a multi-byte character.
func TestFastHTTPMiddleware_LongHostAndPath_Truncated(t *testing.T) {
	longHost := strings.Repeat("a", 300) + ".com"
	hs := serveThroughMiddleware(t, "GET", "http://"+longHost+"/x", func(ctx *fasthttp.RequestCtx) {})
	if v, _ := spanAttr(hs, "url.domain"); utf8.RuneCountInString(v.AsString()) != 255 || !strings.HasPrefix(longHost, v.AsString()) {
		t.Errorf("url.domain has %d characters, want the first 255", utf8.RuneCountInString(v.AsString()))
	}

	// 2-byte characters straddle the byte limit, so a byte cut would split one.
	longPath := "/" + strings.Repeat("é", 2000)
	ps := serveThroughMiddleware(t, "GET", longPath, func(ctx *fasthttp.RequestCtx) {})
	v, _ := spanAttr(ps, "url.path")
	if got := v.AsString(); utf8.RuneCountInString(got) != 1024 || !utf8.ValidString(got) || !strings.HasPrefix(longPath, got) {
		t.Errorf("url.path has %d characters (valid=%v), want the first 1024", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	name := strings.TrimPrefix(ps.Name(), "GET ")
	if utf8.RuneCountInString(name) != 1024 || !utf8.ValidString(name) {
		t.Errorf("span name path has %d characters (valid=%v), want 1024", utf8.RuneCountInString(name), utf8.ValidString(name))
	}

	short := serveThroughMiddleware(t, "GET", "/users/abc", func(ctx *fasthttp.RequestCtx) {})
	if v, _ := spanAttr(short, "url.path"); v.AsString() != "/users/abc" {
		t.Errorf("short url.path changed: %q", v.AsString())
	}
}
