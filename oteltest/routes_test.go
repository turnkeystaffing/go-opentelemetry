package oteltest_test

import (
	"fmt"
	"strings"
	"testing"

	opentelemetry "github.com/turnkeystaffing/go-opentelemetry"
	"github.com/turnkeystaffing/go-opentelemetry/oteltest"
	"github.com/valyala/fasthttp"
)

// recordingTB captures Errorf so the check's own reports can be asserted.
type recordingTB struct {
	testing.TB
	errs []string
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Helper() {}

var routes = map[string][]string{
	"GET":  {"/health", "/users/{id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}"},
	"POST": {"/r/{token}"},
}

// fakeRouter stands in for github.com/fasthttp/router: it records the
// registered pattern of the URL it matches, unless record is false.
func fakeRouter(record bool) fasthttp.RequestHandler {
	byURL := map[string]string{
		"/health": "/health",
		"/users/3f2b8c1e-9a4d-4e2b-8c3a-1f2e3d4c5b6a": routes["GET"][1],
		"/r/x": "/r/{token}",
	}
	return func(ctx *fasthttp.RequestCtx) {
		if record {
			opentelemetry.SetHTTPRoute(ctx, byURL[string(ctx.Path())])
		}
	}
}

func TestRouteSpanCheck_AllRoutesNamed(t *testing.T) {
	check := oteltest.NewRouteSpanCheck(t)
	handler := opentelemetry.FastHTTPMiddleware("svc")(fakeRouter(true))
	check.Run(routes, handler)
}

func TestRouteSpanCheck_ReportsProblems(t *testing.T) {
	tests := []struct {
		name    string
		record  bool
		traced  bool
		opts    []oteltest.RouteOption
		wantErr []string
	}{
		{"route not recorded", false, true, nil, []string{"GET /health: span \"GET /health\" has no http.route", "SaveMatchedRoutePath"}},
		{"no middleware", true, false, nil, []string{"produced no server span"}},
		{"WithoutSpan route that is traced", true, true, []oteltest.RouteOption{oteltest.WithoutSpan("POST", "/r/{token}")}, []string{"marked WithoutSpan"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingTB{TB: t}
			check := oteltest.NewRouteSpanCheck(rec)
			handler := fakeRouter(tt.record)
			if tt.traced {
				handler = opentelemetry.FastHTTPMiddleware("svc")(handler)
			}
			check.Run(routes, handler, tt.opts...)
			all := strings.Join(rec.errs, "\n")
			for _, w := range tt.wantErr {
				if !strings.Contains(all, w) {
					t.Errorf("want a report containing %q, got:\n%s", w, all)
				}
			}
		})
	}
}

func TestRouteSpanCheck_WithoutSpanUntracedRoute(t *testing.T) {
	rec := &recordingTB{TB: t}
	check := oteltest.NewRouteSpanCheck(rec)
	traced := opentelemetry.FastHTTPMiddleware("svc")(fakeRouter(true))
	untraced := fakeRouter(true)
	handler := func(ctx *fasthttp.RequestCtx) {
		if strings.HasPrefix(string(ctx.Path()), "/r/") {
			untraced(ctx)
			return
		}
		traced(ctx)
	}
	check.Run(routes, handler, oteltest.WithoutSpan("POST", "/r/{token}"))
	if len(rec.errs) != 0 {
		t.Errorf("want no reports, got %v", rec.errs)
	}
}

func TestRouteSpanCheck_UnfillableParameter(t *testing.T) {
	rec := &recordingTB{TB: t}
	check := oteltest.NewRouteSpanCheck(rec)
	check.Run(map[string][]string{"GET": {"/hooks/{provider:github|gitlab}"}}, func(*fasthttp.RequestCtx) {})
	if len(rec.errs) != 1 || !strings.Contains(rec.errs[0], `WithParam("provider"`) {
		t.Errorf("want one report pointing at WithParam, got %v", rec.errs)
	}
}

// A ":name" or "*name" segment is literal text in github.com/fasthttp/router
// (parameters are "{name}"), so such a route never matches a real value.
func TestRouteSpanCheck_LiteralColonSegment(t *testing.T) {
	routes := map[string][]string{"GET": {"/events/:id", "/files/*rest"}}
	// Each check installs its own tracer provider, so the handler (which takes
	// its tracer when built) is built after the check.
	newHandler := func() fasthttp.RequestHandler {
		return opentelemetry.FastHTTPMiddleware("svc")(func(ctx *fasthttp.RequestCtx) {
			opentelemetry.SetHTTPRoute(ctx, string(ctx.Path()))
		})
	}

	rec := &recordingTB{TB: t}
	check := oteltest.NewRouteSpanCheck(rec)
	check.Run(routes, newHandler())
	all := strings.Join(rec.errs, "\n")
	for _, w := range []string{`GET /events/:id: segment ":id" is literal text`, `{id}`, `GET /files/*rest: segment "*rest" is literal text`, `{rest:*}`} {
		if !strings.Contains(all, w) {
			t.Errorf("want a report containing %q, got:\n%s", w, all)
		}
	}

	allowed := &recordingTB{TB: t}
	allowedCheck := oteltest.NewRouteSpanCheck(allowed)
	allowedCheck.Run(routes, newHandler(),
		oteltest.AllowLiteralSegment("GET", "/events/:id"), oteltest.AllowLiteralSegment("GET", "/files/*rest"))
	if len(allowed.errs) != 0 {
		t.Errorf("AllowLiteralSegment: want no reports, got %v", allowed.errs)
	}
}

// A router that records a different template than the registered one, or an
// empty route list, is reported.
func TestRouteSpanCheck_WrongRouteAndNoRoutes(t *testing.T) {
	rec := &recordingTB{TB: t}
	check := oteltest.NewRouteSpanCheck(rec)
	handler := opentelemetry.FastHTTPMiddleware("svc")(func(ctx *fasthttp.RequestCtx) {
		opentelemetry.SetHTTPRoute(ctx, "/other/{id}")
	})
	check.Run(map[string][]string{"GET": {"/users/{id}"}}, handler)
	check.Run(map[string][]string{}, handler)
	all := strings.Join(rec.errs, "\n")
	for _, w := range []string{`GET /users/{id}: http.route = "/other/{id}", want "/users/{id}"`, `span name = "GET /other/{id}", want "GET /users/{id}"`, "no routes given"} {
		if !strings.Contains(all, w) {
			t.Errorf("want a report containing %q, got:\n%s", w, all)
		}
	}
}

// A handler that panics before the router runs is reported as a panic, not
// only as a missing http.route, so the cause isn't mistaken for the router.
func TestRouteSpanCheck_ReportsPanic(t *testing.T) {
	rec := &recordingTB{TB: t}
	check := oteltest.NewRouteSpanCheck(rec)
	handler := opentelemetry.FastHTTPMiddleware("svc")(func(*fasthttp.RequestCtx) {
		var m map[string]int
		m["boom"]++ // nil map write: a middleware missing a test dependency
	})
	check.Run(map[string][]string{"GET": {"/health"}}, handler)
	all := strings.Join(rec.errs, "\n")
	if !strings.Contains(all, "GET /health: the handler panicked: assignment to entry in nil map") {
		t.Errorf("want the panic reported, got:\n%s", all)
	}
}
