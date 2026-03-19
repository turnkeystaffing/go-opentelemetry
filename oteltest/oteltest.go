package oteltest

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// NewTestTracerProvider creates a TracerProvider backed by an in-memory SpanRecorder.
func NewTestTracerProvider() (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	return tp, sr
}

// SetGlobalTracerProvider sets the global OTel TracerProvider and restores the previous one on test cleanup.
func SetGlobalTracerProvider(t *testing.T, tp *sdktrace.TracerProvider) {
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
	})
}

// NewTestMeterProvider creates a MeterProvider backed by a ManualReader for synchronous metric collection.
func NewTestMeterProvider() (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return mp, reader
}

// SetGlobalMeterProvider sets the global OTel MeterProvider and restores the previous one on test cleanup.
func SetGlobalMeterProvider(t *testing.T, mp *sdkmetric.MeterProvider) {
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
	})
}

// SpanAttrMap converts a slice of span attribute KeyValues into a simple map for test assertions.
func SpanAttrMap(attrs []attribute.KeyValue) map[string]interface{} {
	m := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

// CollectMetrics collects metrics from a ManualReader and fails the test on error.
func CollectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("oteltest.CollectMetrics: failed to collect metrics: %v", err)
	}
	return rm
}

// AssertHistogramRecorded asserts that a histogram metric with the given name has at least one data point.
func AssertHistogramRecorded(t *testing.T, rm metricdata.ResourceMetrics, metricName string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if hd, ok := m.Data.(metricdata.Histogram[float64]); ok {
					for _, dp := range hd.DataPoints {
						if dp.Count > 0 {
							return
						}
					}
				}
				t.Errorf("histogram %q found but has no data points", metricName)
				return
			}
		}
	}
	t.Errorf("histogram %q not found in collected metrics", metricName)
}

// AssertHistogramInt64Recorded asserts that an Int64 histogram metric with the given name has at least one data point.
func AssertHistogramInt64Recorded(t *testing.T, rm metricdata.ResourceMetrics, metricName string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if hd, ok := m.Data.(metricdata.Histogram[int64]); ok {
					for _, dp := range hd.DataPoints {
						if dp.Count > 0 {
							return
						}
					}
				}
				t.Errorf("histogram %q found but has no data points", metricName)
				return
			}
		}
	}
	t.Errorf("histogram %q not found in collected metrics", metricName)
}

// AssertCounterValue asserts that an Int64 counter metric with the given name has the expected value.
func AssertCounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, expected int64) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == metricName {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					var total int64
					for _, dp := range sum.DataPoints {
						total += dp.Value
					}
					if total != expected {
						t.Errorf("counter %q: got value %d, want %d", metricName, total, expected)
					}
					return
				}
				t.Errorf("counter %q found but is not an Int64 Sum", metricName)
				return
			}
		}
	}
	t.Errorf("counter %q not found in collected metrics", metricName)
}
