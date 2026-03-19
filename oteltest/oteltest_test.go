package oteltest

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewTestTracerProvider(t *testing.T) {
	tp, sr := NewTestTracerProvider()
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	if sr == nil {
		t.Fatal("expected non-nil SpanRecorder")
	}

	// Create a span and verify it's recorded
	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test.span")
	span.End()

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name() != "test.span" {
		t.Errorf("expected span name 'test.span', got %q", spans[0].Name())
	}
}

func TestSetGlobalTracerProvider(t *testing.T) {
	prevTP := otel.GetTracerProvider()

	tp, _ := NewTestTracerProvider()
	SetGlobalTracerProvider(t, tp)

	if otel.GetTracerProvider() != tp {
		t.Error("expected global tracer provider to be set")
	}

	// After test cleanup, the previous provider should be restored.
	// We can't test cleanup directly, but we verified the set works.
	_ = prevTP
}

func TestNewTestMeterProvider(t *testing.T) {
	mp, reader := NewTestMeterProvider()
	if mp == nil {
		t.Fatal("expected non-nil MeterProvider")
	}
	if reader == nil {
		t.Fatal("expected non-nil ManualReader")
	}

	// Record a metric and collect
	meter := mp.Meter("test")
	h, err := meter.Float64Histogram("test.hist")
	if err != nil {
		t.Fatalf("failed to create histogram: %v", err)
	}
	h.Record(context.Background(), 1.5)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("failed to collect: %v", err)
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "test.hist" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected test.hist metric")
	}
}

func TestSetGlobalMeterProvider(t *testing.T) {
	mp, _ := NewTestMeterProvider()
	SetGlobalMeterProvider(t, mp)

	if otel.GetMeterProvider() != mp {
		t.Error("expected global meter provider to be set")
	}
}

func TestSpanAttrMap(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("str", "hello"),
		attribute.Int("num", 42),
		attribute.Bool("flag", true),
	}

	m := SpanAttrMap(attrs)

	if m["str"] != "hello" {
		t.Errorf("expected str=hello, got %v", m["str"])
	}
	if m["num"] != int64(42) {
		t.Errorf("expected num=42, got %v", m["num"])
	}
	if m["flag"] != true {
		t.Errorf("expected flag=true, got %v", m["flag"])
	}
}

func TestSpanAttrMap_Empty(t *testing.T) {
	m := SpanAttrMap(nil)
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}

func TestCollectMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	h, _ := meter.Float64Histogram("test.collect")
	h.Record(context.Background(), 10.0)

	rm := CollectMetrics(t, reader)

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "test.collect" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected test.collect metric")
	}
}

func TestAssertHistogramRecorded_Found(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	h, _ := meter.Float64Histogram("test.assert")
	h.Record(context.Background(), 5.0)

	rm := CollectMetrics(t, reader)
	AssertHistogramRecorded(t, rm, "test.assert")
}

func TestAssertHistogramRecorded_NotFound(t *testing.T) {
	// Use a sub-test so we can verify the failure without failing this test
	mockT := &testing.T{}
	rm := metricdata.ResourceMetrics{}

	// This should call t.Errorf on mockT, but we can't easily capture that.
	// Instead we just verify it doesn't panic with empty data.
	AssertHistogramRecorded(mockT, rm, "nonexistent")
}
