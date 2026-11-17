package opentelemetry

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

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

	handler := &recordingErrorHandler{}
	otel.SetErrorHandler(handler)
	t.Cleanup(func() { otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {})) })

	logsOff := false
	cfg := Config{
		Enabled:     true,
		ServiceName: "svc",
		Protocol:    ProtocolOTLPHTTP,
		Endpoint:    "localhost:4318",
		Insecure:    true,
		Components:  ComponentsConfig{Logs: &logsOff},
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
