package opentelemetry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// shutdownCountingExporter wraps a real metric exporter and counts Shutdown calls.
type shutdownCountingExporter struct {
	sdkmetric.Exporter
	shutdowns *atomic.Int32
}

func (e shutdownCountingExporter) Shutdown(ctx context.Context) error {
	e.shutdowns.Add(1)
	return e.Exporter.Shutdown(ctx)
}

// AC-19: when the log provider can't be created, InitializeProvider returns the
// error, shuts down the trace and meter providers it already started, and
// leaves the global OTel providers as they were.
func TestInitializeProvider_LogProviderFailure_ShutsDownStartedProviders(t *testing.T) {
	logErr := errors.New("boom: log exporter")
	prevLog := newLogExporter
	newLogExporter = func(context.Context, Config) (sdklog.Exporter, error) { return nil, logErr }
	t.Cleanup(func() { newLogExporter = prevLog })

	var shutdowns atomic.Int32
	prevMetric := newMetricExporter
	newMetricExporter = func(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
		exp, err := createMetricExporter(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return shutdownCountingExporter{Exporter: exp, shutdowns: &shutdowns}, nil
	}
	t.Cleanup(func() { newMetricExporter = prevMetric })

	restoreOTelGlobals(t)
	tpBefore, mpBefore, lpBefore := otel.GetTracerProvider(), otel.GetMeterProvider(), global.GetLoggerProvider()

	logsOn := true
	cfg := Config{
		Enabled:     true,
		ServiceName: "svc",
		Protocol:    ProtocolOTLPHTTP,
		Endpoint:    "localhost:4318",
		Insecure:    true,
		Components:  ComponentsConfig{Logs: &logsOn},
	}
	p, err := InitializeProvider(context.Background(), cfg)
	if !errors.Is(err, logErr) {
		t.Fatalf("want the log exporter error, got %v", err)
	}
	if p != nil {
		t.Error("no Provider must be returned on failure")
	}
	if n := shutdowns.Load(); n != 1 {
		t.Errorf("started meter provider must be shut down once, got %d Shutdown calls", n)
	}
	if otel.GetTracerProvider() != tpBefore {
		t.Error("global tracer provider must not be replaced when init fails")
	}
	if otel.GetMeterProvider() != mpBefore {
		t.Error("global meter provider must not be replaced when init fails")
	}
	if global.GetLoggerProvider() != lpBefore {
		t.Error("global logger provider must not be replaced when init fails")
	}
}

// AC-19: a successful init still installs every started provider as the
// global one (installing is deferred until all signals have started).
func TestInitializeProvider_Success_InstallsGlobalProviders(t *testing.T) {
	restoreOTelGlobals(t)
	logsOn := true
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
		t.Fatalf("InitializeProvider: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if otel.GetTracerProvider() != p.TracerProvider {
		t.Error("global tracer provider must be the started one")
	}
	if otel.GetMeterProvider() != p.MeterProvider {
		t.Error("global meter provider must be the started one")
	}
	if global.GetLoggerProvider() != p.LoggerProvider {
		t.Error("global logger provider must be the started one")
	}
}
