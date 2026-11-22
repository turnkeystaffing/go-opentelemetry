package opentelemetry

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/valyala/fasthttp"
)

const uuidRegexParam = "{id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}"

// A route template's parameters are reduced to their names, whatever the
// router put inside the braces, so span names and http.route stay readable.
func TestTidyRoute(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"uuid regex with nested braces", "/checks/" + uuidRegexParam + "/reports/generate", "/checks/{id}/reports/generate"},
		{"two regex params", "/c/" + uuidRegexParam + "/r/{report_id:[0-9a-f]{8}-[0-9a-f]{4}}/share", "/c/{id}/r/{report_id}/share"},
		{"simple regex", "/users/{id:\\d+}", "/users/{id}"},
		{"regex with group", "/posts/{slug:[a-z]+(?:-[a-z]+)*}", "/posts/{slug}"},
		{"quantifier range", "/x/{v:\\d{2,3}}", "/x/{v}"},
		{"escaped brace in regex", "/e/{v:a\\}b}/z", "/e/{v}/z"},
		{"optional param", "/list/{page?}", "/list/{page}"},
		{"catch-all", "/static/{filepath:*}", "/static/{filepath}"},
		{"fmt placeholder", "/bff/proxy/{backend_id:%s}/{path:*}", "/bff/proxy/{backend_id}/{path}"},
		{"spaces around the name", "/u/{ id :\\d+}", "/u/{id}"},
		{"already clean", "/users/{id}/emails/{email_id}", "/users/{id}/emails/{email_id}"},
		{"no braces", "/health", "/health"},
		{"colon style", "/users/:id/files/*path", "/users/:id/files/*path"},
		{"unbalanced kept as written", "/broken/{id:[0-9]+", "/broken/{id:[0-9]+"},
		{"stray closing brace kept", "/a}/b/{id}", "/a}/b/{id}"},
		{"empty", "", ""},
		{"brace in the name kept as written (found by fuzzing)", "{{:}}  }", "{{:}}  }"},
		{"empty name kept as written", "/a/{}/b", "/a/{}/b"},
		{"dotted and dashed names", "/v/{api.version}/{item-id:\\d+}", "/v/{api.version}/{item-id}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tidyRoute(tt.in); got != tt.want {
				t.Errorf("tidyRoute(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Both route sources go through the tidy-up before naming the span.
func TestFastHTTPMiddleware_RouteTemplateTidied(t *testing.T) {
	const routerKey = "__matchedRoutePath::k3yR4nd0m__"
	want := "/api/v1/checks/{id}/reports/generate"

	fromRouter := serveThroughMiddleware(t, "POST", "/api/v1/checks/3f2b8c1e-9a4d-4e2b-8c3a-1f2e3d4c5b6a/reports/generate", func(ctx *fasthttp.RequestCtx) {
		ctx.SetUserValue(routerKey, "/api/v1/checks/"+uuidRegexParam+"/reports/generate")
	})
	fromHandler := serveThroughMiddleware(t, "POST", "/api/v1/checks/3f2b8c1e-9a4d-4e2b-8c3a-1f2e3d4c5b6a/reports/generate", func(ctx *fasthttp.RequestCtx) {
		SetHTTPRoute(ctx, "/api/v1/checks/"+uuidRegexParam+"/reports/generate")
	})
	for name, span := range map[string]interface {
		Name() string
	}{"fasthttp/router": fromRouter, "SetHTTPRoute": fromHandler} {
		if span.Name() != "POST "+want {
			t.Errorf("%s: span name = %q, want %q", name, span.Name(), "POST "+want)
		}
	}
	if v, _ := spanAttr(fromRouter, "http.route"); v.AsString() != want {
		t.Errorf("fasthttp/router: http.route = %q, want %q", v.AsString(), want)
	}
	if v, _ := spanAttr(fromHandler, "http.route"); v.AsString() != want {
		t.Errorf("SetHTTPRoute: http.route = %q, want %q", v.AsString(), want)
	}

	// A route is also held to the path rules: valid UTF-8, at most 1024 characters.
	long := serveThroughMiddleware(t, "GET", "/x", func(ctx *fasthttp.RequestCtx) {
		SetHTTPRoute(ctx, "/r\xff/"+strings.Repeat("a", 2000))
	})
	v, _ := spanAttr(long, "http.route")
	if r := v.AsString(); !utf8.ValidString(r) || utf8.RuneCountInString(r) != maxPathLen {
		t.Errorf("long route: valid=%v len=%d, want valid UTF-8 of %d characters", utf8.ValidString(r), utf8.RuneCountInString(r), maxPathLen)
	}
}

// Whatever the input, tidyRoute never panics, never grows the route, and is
// idempotent (tidying a tidied route changes nothing).
func FuzzTidyRoute(f *testing.F) {
	for _, s := range []string{"/checks/" + uuidRegexParam, "/a/{b:{c}}/d", "/{x?}", "{", "}", "/a\\{b", "/{a:\\}", "/{{}}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := tidyRoute(in)
		if len(out) > len(in) {
			t.Fatalf("tidyRoute(%q) grew to %q", in, out)
		}
		if again := tidyRoute(out); again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, out, again)
		}
	})
}
