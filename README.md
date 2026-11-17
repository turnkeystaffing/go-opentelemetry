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

Call `InitializeProvider` once at startup. It sets the global tracer, meter and
logger providers and the W3C `traceparent` + `baggage` propagator.

```go
provider, err := opentelemetry.InitializeProvider(ctx, opentelemetry.Config{
	Enabled:        true,
	ServiceName:    "webhook-service",
	ServiceVersion: "1.4.0",
	Environment:    "production",
	Protocol:       opentelemetry.ProtocolOTLPgRPC, // or ProtocolOTLPHTTP
	Endpoint:       "otel-collector:4317",          // host:port, no scheme
	Insecure:       true,                           // no TLS (local / sidecar collector)
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

- `Enabled: false` returns no-op providers, so the rest of the code needs no `if`.
- Turn single signals off with `Components.Traces`, `Components.Metrics`, `Components.Logs` (`*bool`, default on).
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

**Span names use the route template, never the raw path.** Provide the template
in one of two ways:

- `github.com/fasthttp/router` with `r.SaveMatchedRoutePath = true` — picked up automatically.
- Any router: call `opentelemetry.SetHTTPRoute(ctx, "/users/{id}")` in the handler.

With a template the span is `GET /users/{id}` and `http.route` is set. Without
one, ID-like segments (all digits, UUIDs, 16+ hex characters) are replaced with
`{id}` in the span name only, and `http.route` is left unset.

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
3. **`url.path` has no query string.** The query moved to `url.query`.

Also in this release: metric exporter init failures are reported through
`otel.Handle`; `SpanOp` durations keep sub-millisecond precision; a failing
slog handler no longer stops records reaching the others; the resource uses
semconv v1.26.0 (attribute keys unchanged).
