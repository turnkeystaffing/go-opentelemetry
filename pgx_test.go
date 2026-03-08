package opentelemetry

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestNewPgxTracer(t *testing.T) {
	tracer := NewPgxTracer("testdb")
	if tracer == nil {
		t.Fatal("NewPgxTracer returned nil")
	}
}

func TestNewPgxTracerWithExtraAttributes(t *testing.T) {
	tracer := NewPgxTracer("testdb",
		attribute.String("service.name", "test-service"),
	)
	if tracer == nil {
		t.Fatal("NewPgxTracer with extra attributes returned nil")
	}
}

func TestNewPgxTracerEmptyDBName(t *testing.T) {
	tracer := NewPgxTracer("")
	if tracer == nil {
		t.Fatal("NewPgxTracer with empty dbName returned nil")
	}
}
