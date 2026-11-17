package opentelemetry

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log/global"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// restoreOTelGlobals puts back the global OTel providers, propagator and error
// handler that InitializeProvider replaces, so tests don't leak global state.
func restoreOTelGlobals(t *testing.T) {
	t.Helper()
	tp, mp, lp := otel.GetTracerProvider(), otel.GetMeterProvider(), global.GetLoggerProvider()
	prop := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetMeterProvider(mp)
		global.SetLoggerProvider(lp)
		otel.SetTextMapPropagator(prop)
		// OTel's default error handler can't be re-installed once replaced
		// (it is the global delegate), so restore its behaviour: log the error.
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) { log.Print(err) }))
	})
}

type recordingErrorHandler struct {
	mu   sync.Mutex
	errs []error
}

func (h *recordingErrorHandler) Handle(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.errs = append(h.errs, err)
}

// AC-6: a metric exporter that can't be created must not fail startup (services
// keep running without metrics), but the error is reported through otel.Handle
// instead of being dropped, and traces keep working.
func TestInitializeProvider_MetricExporterFailure_ReportedAndNonFatal(t *testing.T) {
	exporterErr := errors.New("boom: metric exporter")
	prevNew := newMetricExporter
	newMetricExporter = func(context.Context, Config) (sdkmetric.Exporter, error) { return nil, exporterErr }
	t.Cleanup(func() { newMetricExporter = prevNew })

	restoreOTelGlobals(t)
	logsOn := true // logs are off by default; turn them on to check they're unaffected
	handler := &recordingErrorHandler{}
	otel.SetErrorHandler(handler)

	cfg := Config{
		Enabled:     true,
		ServiceName: "svc",
		Protocol:    ProtocolOTLPHTTP,
		Endpoint:    "localhost:4318",
		Insecure:    true,
		Components:  ComponentsConfig{Logs: &logsOn},
	}

	p, err := InitializeProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("InitializeProvider must not fail on a metric exporter error, got %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if p.MeterProvider == nil || p.Meter == nil {
		t.Fatal("expected no-op MeterProvider and Meter to be set")
	}
	if p.TracerProvider == nil || p.Tracer == nil {
		t.Fatal("traces must be unaffected by the metric failure")
	}
	if p.LoggerProvider == nil {
		t.Fatal("logs must be unaffected by the metric failure")
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	found := false
	for _, e := range handler.errs {
		if errors.Is(e, exporterErr) {
			found = true
		}
	}
	if !found {
		t.Errorf("metric exporter error was not reported via otel.Handle; got %v", handler.errs)
	}
}

// AC-10: the resource uses the same semconv version (v1.26.0) as the rest of
// the library, and keeps the existing attribute keys.
func TestCreateResource_UsesSemconv126Schema(t *testing.T) {
	res, err := createResource(Config{ServiceName: "svc", ServiceVersion: "1.2.3", Environment: "prod"})
	if err != nil {
		t.Fatalf("createResource: %v", err)
	}
	if got, want := res.SchemaURL(), "https://opentelemetry.io/schemas/1.26.0"; got != want {
		t.Errorf("schema URL = %q, want %q", got, want)
	}
	want := map[string]string{"service.name": "svc", "service.version": "1.2.3", "deployment.environment": "prod"}
	for k, v := range want {
		got, ok := res.Set().Value(attribute.Key(k))
		if !ok || got.AsString() != v {
			t.Errorf("%s = %q (set=%v), want %q", k, got.AsString(), ok, v)
		}
	}
}

// AC-11: an invalid config is rejected with the validation error, before any
// resource or exporter is built.
func TestInitializeProvider_InvalidConfig_ReturnsValidationError(t *testing.T) {
	restoreOTelGlobals(t)
	_, err := InitializeProvider(context.Background(), Config{Enabled: true, ServiceName: "svc", Endpoint: "localhost:4318"})
	if err == nil || !strings.Contains(err.Error(), "invalid OpenTelemetry configuration") || !strings.Contains(err.Error(), "protocol is required") {
		t.Fatalf("want validation error about the missing protocol, got %v", err)
	}
}
