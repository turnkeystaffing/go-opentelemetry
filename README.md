# go-opentelemetry

Shared OpenTelemetry setup for TurnKey Go services: tracer, meter and logger
providers over OTLP (gRPC or HTTP), FastHTTP server middleware, an instrumented
`net/http` client, slog fan-out to stdout and OTel, pgx / Redis / AWS SDK
instrumentation, span helpers and test utilities.

```sh
go get github.com/turnkeystaffing/go-opentelemetry@latest
```

```go
import opentelemetry "github.com/turnkeystaffing/go-opentelemetry"
```

## Provider setup

Call `InitializeProvider` once at startup. It sets the W3C `traceparent` +
`baggage` propagator and the global provider of each enabled signal (tracer,
meter, and logger when `Components.Logs` is on).

```go
provider, err := opentelemetry.InitializeProvider(ctx, opentelemetry.Config{
	Enabled:        true,
	ServiceName:    "webhook-service",
	ServiceVersion: "1.4.0",
	Environment:    "production",
	Protocol:       opentelemetry.ProtocolOTLPgRPC, // or ProtocolOTLPHTTP
	Endpoint:       "otel-collector:4317",          // host:port, no scheme
	Sampler: opentelemetry.SamplerConfig{
		Type:        "parentbased_traceidratio",
		SampleRatio: 0.1,
	},
})
if err != nil {
	log.Fatal(err)
}
defer provider.Shutdown(context.Background())
```

- `Enabled: false` returns providers that export nothing, so tracing and
  metrics code needs no `if`. `Provider.LoggerProvider` is nil whenever logs
  are off (including `Enabled: false`): check it before use.
- Toggle signals with `Components.Traces`, `Components.Metrics`, `Components.Logs`
  (`*bool`). Traces and metrics default to on; logs default to off.
- The exporter uses TLS by default. Set `Insecure: true` only for a collector
  reached over a trusted local link (sidecar, localhost); it sends plaintext.
  The standard `OTEL_EXPORTER_OTLP_INSECURE` / `OTEL_EXPORTER_OTLP_*_INSECURE`
  environment variables can also switch an exporter to plaintext.
- If the metric exporter can't be created, startup still succeeds without
  metrics and the error goes to the global OTel error handler
  (`otel.SetErrorHandler` to route it to your logger).

## FastHTTP server middleware

```go
handler := opentelemetry.FastHTTPMiddleware("webhook-service")(router.Handler)
fasthttp.ListenAndServe(":8080", handler)
```

Each request gets a server span. The trace context is available to handlers:

```go
func handle(ctx *fasthttp.RequestCtx) {
	reqCtx := opentelemetry.GetTraceContext(ctx) // context.Context with the span
	traceID := opentelemetry.GetTraceID(ctx)
	// ...
}
```

**Span names should use the route template.** Provide it in one of two ways:

- `github.com/fasthttp/router` with `r.SaveMatchedRoutePath = true` — picked up automatically.
- Any router: call `opentelemetry.SetHTTPRoute(ctx, "/users/{id}")` in the handler.

With a template the span is `GET /users/{id}` and `http.route` is set. Without
one, `http.route` is left unset and the span name is a best-effort fallback:

- ID-like segments (all digits, UUIDs, 16+ hex characters) are replaced with
  `{id}`; other segments (slugs, short tokens) are kept as sent. Provide a
  template for any endpoint with other kinds of path parameters.
- A 3xx or 4xx response (404, 401/403 from auth middleware before routing,
  405, 429, redirects) is named after the method only (`GET`), so unmatched
  paths such as scanner traffic don't create one span name per path.

Methods outside GET, HEAD, POST, PUT, DELETE, CONNECT, OPTIONS, TRACE and PATCH
are recorded as `_OTHER` (span name and `http.request.method`), with the
original in `http.request.method_original`.

Samplers see the span name from before routing (the path-derived fallback);
the final name is set when the handler returns.

Pass only templates to `SetHTTPRoute`, never the raw path.

`url.path` holds the decoded path; it is not set for targets without a path
(`OPTIONS *`, `CONNECT host:port`). Invalid UTF-8 in the path or host is
replaced with `�`, `url.domain` is cut to 255 characters and `url.path`
(and the path in the span name) to 1024. `url.query` holds the raw query with the
values of credential and signature parameters replaced with `REDACTED`. Names
are compared lower-cased with `-` and `_` removed; a parameter is redacted when
its name contains `token`, `secret`, `password`, `passwd`, `signature`,
`credential`, `apikey`, `session` or `jwt`, or is `key`, `sig`, `code`, `auth`,
`authorization`, `pwd`, `otp` or `AWSAccessKeyId`.

Span status follows the HTTP semantic conventions: only 5xx responses mark the
span as Error.

## Outbound HTTP client

```go
client := opentelemetry.NewInstrumentedHTTPClient(&http.Client{Timeout: 10 * time.Second})
req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
resp, err := client.Do(req) // client span + traceparent header
```

`NewInstrumentedHTTPClientWithOptions` accepts `otelhttp.Option`s. For non-`net/http`
clients, `InjectTraceHeaders(ctx, headers)` fills a `map[string]string`.

Client spans record the full outbound URL (`url.full`). otelhttp (v0.67.0) strips
only user:password from it and does not redact the query string, so don't put
credentials such as `api_key` or `token` in outbound query strings; send them in
headers.

## Logging (slog)

```go
handler := opentelemetry.NewMultiSlogHandler(opentelemetry.SlogConfig{
	ServiceName: "webhook-service",
	Level:       slog.LevelInfo,
	Format:      "json", // or "text"
	OTelEnabled: true,
})
slog.SetDefault(slog.New(handler))
```

Records go to stdout and to the OTel log pipeline. Use `logger.InfoContext(ctx, …)`
so OTel records carry the trace and span IDs. A failure in one output does not
stop the others. Building blocks: `NewMultiHandler`, `NewLevelFilterHandler`,
`NewSlogHandler`.

## PostgreSQL (pgx)

```go
poolCfg, _ := pgxpool.ParseConfig(dsn)
poolCfg.ConnConfig.Tracer = opentelemetry.NewPgxTracer("orders")
pool, _ := pgxpool.NewWithConfig(ctx, poolCfg)

if err := opentelemetry.RecordPgxPoolStats(pool); err != nil {
	slog.Warn("pgxpool metrics not registered", "error", err)
}
```

## Redis

```go
rdb := redis.NewClient(&redis.Options{Addr: "redis:6379"})
if tErr, mErr := opentelemetry.InstrumentRedis(rdb); tErr != nil || mErr != nil {
	slog.Warn("redis instrumentation", "tracing", tErr, "metrics", mErr)
}
```

`InstrumentRedisTracing` / `InstrumentRedisMetrics` take `redisotel` options.

## AWS SDK v2

```go
awsCfg, err := opentelemetry.LoadAWSConfigWithOTel(ctx)        // new config
opentelemetry.InstrumentAWSConfig(&existingCfg)                 // or existing one
```

## Spans around operations

```go
duration := opentelemetry.NewFloat64Histogram(provider.Meter,
	"orders.create.duration", "Order creation time", "ms", logger)

order, err := opentelemetry.SpanOp(ctx, provider.Tracer, "orders.create",
	func(ctx context.Context, span trace.Span) (*Order, error) {
		return repo.Create(ctx, input)
	},
	opentelemetry.WithAttributes(attribute.String("order.source", "api")),
	opentelemetry.WithDurationHistogram(duration), // fractional milliseconds
)
```

`SpanOp` records errors and sets the span status. `SpanOpVoid` is the
error-only variant. `SanitizeSpanAttribute` truncates long values.

## Testing

`oteltest` provides in-memory providers and assertions:

```go
tp, recorder := oteltest.NewTestTracerProvider()
oteltest.SetGlobalTracerProvider(t, tp)

mp, reader := oteltest.NewTestMeterProvider()
oteltest.SetGlobalMeterProvider(t, mp)
rm := oteltest.CollectMetrics(t, reader)
oteltest.AssertHistogramRecorded(t, rm, "orders.create.duration")
```

## Telemetry changes in v0.7.0

No Go API changes (one addition: `SetHTTPRoute`). Upgrading changes what
services emit, so check dashboards, alerts and sampling rules that key on:

1. **Span names and `http.route`.** Server spans were named after the raw path
   (`GET /users/123`), with the same value in `http.route`. They are now named
   after the route template (`GET /users/{id}`); without a template, ID-like
   segments are replaced with `{id}` and `http.route` is not set.
2. **4xx is no longer an error.** Server spans are Error only for 5xx; 2xx–4xx
   leave the status Unset (previously 4xx was Error and success was Ok).
   Alerts that relied on 401/403/429 spikes showing as errors should filter
   on `http.response.status_code` instead.
3. **`url.path` has no query string** and is the decoded, normalized path
   (previously the raw request target), and is not set for `OPTIONS *` or
   `CONNECT host:port`. The query moved to `url.query`, with sensitive
   parameter values redacted.
4. **Unrouted 3xx/4xx responses are named after the method only** (`GET`),
   and non-standard methods are recorded as `_OTHER`.
5. **`http.response.body.size` is the real body size** (it was usually 0, and
   -1 for chunked streams; chunked streams now leave it out). HEAD, 1xx, 204
   and 304 responses report 0, since no body is sent.
6. **A panicking handler marks the span Error** and names it after the route;
   the panic still propagates.
7. **Invalid UTF-8 is replaced and long values are cut:** `url.domain` to 255
   characters, `url.path` and the span-name path to 1024.

Also in this release: metric exporter init failures are reported through
`otel.Handle`; if the log provider can't be created, `InitializeProvider`
shuts down the providers it already started and leaves the OTel globals as
they were; `SpanOp` durations keep sub-millisecond precision; a failing
slog handler no longer stops records reaching the others; the resource uses
semconv v1.26.0 (attribute keys unchanged).
