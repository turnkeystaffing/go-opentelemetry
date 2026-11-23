package oteltest

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// RouteSpanCheck verifies, in a test, that every route of a service produces a
// server span named after its route template, with http.route set. It sends
// one in-memory request per route through the service's real handler chain
// (router plus FastHTTPMiddleware); nothing goes over the network.
//
// Create it before building the handler, because FastHTTPMiddleware takes its
// tracer from the global provider when it is built:
//
//	check := oteltest.NewRouteSpanCheck(t)
//	r := gateway.NewRouter(deps)
//	check.Run(r.List(), gateway.NewHandler(deps, r))
//
// routes is what github.com/fasthttp/router's (*Router).List returns: method
// to registered route patterns.
type RouteSpanCheck struct {
	t   testing.TB
	rec *tracetest.SpanRecorder
}

// NewRouteSpanCheck installs an in-memory tracer provider as the global one
// for the duration of the test and returns a check that reads its spans.
func NewRouteSpanCheck(t testing.TB) *RouteSpanCheck {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(t.Context())
	})
	return &RouteSpanCheck{t: t, rec: rec}
}

// RouteOption adjusts one Run.
type RouteOption func(*routeOptions)

type routeOptions struct {
	params  map[string]string
	without map[string]bool
	literal map[string]bool
}

// WithParam sets the value used for the route parameter name in every route,
// for parameters whose constraint none of the built-in sample values match.
func WithParam(name, value string) RouteOption {
	return func(o *routeOptions) { o.params[name] = value }
}

// WithoutSpan marks a route that is deliberately not traced (for example one
// that carries a secret in its path). route is the template as recorded, with
// parameters by name only: WithoutSpan("GET", "/r/{token}"). Run then checks
// that the route produces no server span.
func WithoutSpan(method, route string) RouteOption {
	return func(o *routeOptions) { o.without[method+" "+route] = true }
}

// AllowLiteralSegment accepts a route whose ":name" or "*name" segment is
// meant as literal text. Without it Run reports such a segment, because
// github.com/fasthttp/router writes parameters as "{name}" and treats ":name"
// as text, so the route would never match a real value.
func AllowLiteralSegment(method, route string) RouteOption {
	return func(o *routeOptions) { o.literal[method+" "+route] = true }
}

// fasthttpRouterAnyMethod is the method key github.com/fasthttp/router uses
// in List for routes registered with ANY.
const fasthttpRouterAnyMethod = "*"

// Run sends one request per route through handler and reports, per route,
// a missing server span, a missing or wrong http.route, or a wrong span name.
// A route whose URL can't be built is reported too, never skipped.
func (c *RouteSpanCheck) Run(routes map[string][]string, handler fasthttp.RequestHandler, opts ...RouteOption) {
	c.t.Helper()
	o := routeOptions{params: map[string]string{}, without: map[string]bool{}, literal: map[string]bool{}}
	for _, opt := range opts {
		opt(&o)
	}
	if len(routes) == 0 {
		c.t.Errorf("route span check: no routes given")
		return
	}
	methods := make([]string, 0, len(routes))
	for m := range routes {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	for _, method := range methods {
		paths := append([]string(nil), routes[method]...)
		sort.Strings(paths)
		for _, route := range paths {
			c.checkRoute(method, route, handler, o)
		}
	}
}

func (c *RouteSpanCheck) checkRoute(method, route string, handler fasthttp.RequestHandler, o routeOptions) {
	c.t.Helper()
	if !o.literal[method+" "+route] {
		if seg, hint := otherRouterSyntax(route); seg != "" {
			c.t.Errorf("%s %s: segment %q is literal text in github.com/fasthttp/router, so the route never matches a real value; write it as %q (or AllowLiteralSegment if the text is meant)", method, route, seg, hint)
		}
	}
	want, url, err := sampleRoute(route, o.params)
	if err != nil {
		c.t.Errorf("%s %s: %v", method, route, err)
		return
	}
	reqMethod := method
	if method == fasthttpRouterAnyMethod {
		reqMethod = fasthttp.MethodGet
	}
	before := len(c.rec.Ended())
	if p := serve(handler, reqMethod, url); p != nil {
		// Reported first: a panic in a middleware before the router (often a
		// test dependency left nil) also explains a missing http.route below.
		c.t.Errorf("%s %s: the handler panicked: %v (request %s %s; if it happened before routing, the route can't be recorded)", method, route, p, reqMethod, url)
	}
	span := serverSpan(c.rec.Ended()[before:])

	if o.without[reqMethod+" "+want] {
		if span != nil {
			c.t.Errorf("%s %s: marked WithoutSpan, but request %s %s produced span %q", method, route, reqMethod, url, span.Name())
		}
		return
	}
	if span == nil {
		c.t.Errorf("%s %s: request %s %s produced no server span (is the route wrapped by FastHTTPMiddleware?)", method, route, reqMethod, url)
		return
	}
	got, ok := spanStringAttr(span, "http.route")
	switch {
	case !ok:
		c.t.Errorf("%s %s: span %q has no http.route, want %q (does the router record the matched route, e.g. SaveMatchedRoutePath?)", method, route, span.Name(), want)
	case got != want:
		c.t.Errorf("%s %s: http.route = %q, want %q", method, route, got, want)
	}
	if name := reqMethod + " " + want; span.Name() != name {
		c.t.Errorf("%s %s: span name = %q, want %q", method, route, span.Name(), name)
	}
}

// serve runs one request and returns the panic value, if any. A panicking
// handler still ends its span, so the span is checked like any other.
func serve(handler fasthttp.RequestHandler, method, url string) (panicked any) {
	defer func() { panicked = recover() }()
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(method)
	ctx.Request.SetRequestURI(url)
	handler(ctx)
	return nil
}

func serverSpan(spans []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.SpanKind() == trace.SpanKindServer {
			return s
		}
	}
	return nil
}

func spanStringAttr(span sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// otherRouterSyntax returns the first path segment written in another
// router's parameter syntax (":id", "*rest") and its fasthttp/router form
// ("{id}", "{rest:*}"), or "" when there is none.
func otherRouterSyntax(route string) (segment, hint string) {
	for _, seg := range strings.Split(route, "/") {
		if len(seg) < 2 {
			continue
		}
		switch seg[0] {
		case ':':
			return seg, "{" + seg[1:] + "}"
		case '*':
			return seg, "{" + seg[1:] + ":*}"
		}
	}
	return "", ""
}

// sampleValues are tried, in order, against a parameter's regex constraint.
var sampleValues = []string{"3f2b8c1e-9a4d-4e2b-8c3a-1f2e3d4c5b6a", "1", "x", "abc", "v1", "a-b", "a_b"}

// sampleRoute turns a registered route into the template the span should
// carry ("{id:[0-9]+}" -> "{id}") and a URL that the route matches
// ("{id:[0-9]+}" -> "1"). Parameter values come from params, else from the
// constraint: none or optional -> "x", catch-all ("*") -> "a/b", a regex ->
// the first of sampleValues it fully matches.
func sampleRoute(route string, params map[string]string) (template, url string, err error) {
	var tmpl, u strings.Builder
	for i := 0; i < len(route); {
		if route[i] != '{' {
			tmpl.WriteByte(route[i])
			u.WriteByte(route[i])
			i++
			continue
		}
		end := closingBrace(route, i)
		if end < 0 {
			return "", "", fmt.Errorf("unbalanced braces in route")
		}
		inner := route[i+1 : end]
		name, constraint := inner, ""
		if k := strings.IndexAny(inner, ":?"); k >= 0 {
			name, constraint = inner[:k], inner[k:]
		}
		name = strings.TrimSpace(name)
		value, err := sampleValue(name, constraint, params)
		if err != nil {
			return "", "", err
		}
		tmpl.WriteString("{" + name + "}")
		u.WriteString(value)
		i = end + 1
	}
	return tmpl.String(), u.String(), nil
}

func sampleValue(name, constraint string, params map[string]string) (string, error) {
	if v, ok := params[name]; ok {
		return v, nil
	}
	switch {
	case constraint == "" || constraint == "?":
		return "x", nil
	case constraint == ":*":
		return "a/b", nil
	}
	expr := strings.TrimPrefix(constraint, ":")
	re, err := regexp.Compile("^(?:" + expr + ")$")
	if err != nil {
		return "", fmt.Errorf("can't read the constraint of {%s}: %v; set a value with oteltest.WithParam(%q, …)", name, err, name)
	}
	for _, v := range sampleValues {
		if re.MatchString(v) {
			return v, nil
		}
	}
	return "", fmt.Errorf("no sample value matches {%s:%s}; set one with oteltest.WithParam(%q, …)", name, expr, name)
}

// closingBrace returns the index of the "}" that closes the "{" at open,
// counting nested braces and skipping "\"-escaped characters, or -1.
func closingBrace(route string, open int) int {
	depth := 0
	for j := open; j < len(route); j++ {
		switch route[j] {
		case '\\':
			j++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}
