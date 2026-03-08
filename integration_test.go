package opentelemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// TestOpenTelemetryProviderIntegration tests the full OpenTelemetry provider initialization
func TestOpenTelemetryProviderIntegration(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Protocol:       "otlp_http",
		Endpoint:       "localhost:4318",
		Insecure:       true,
		Components: ComponentsConfig{
			Traces:  &[]bool{true}[0],
			Metrics: &[]bool{true}[0],
			Logs:    &[]bool{true}[0],
		},
		Sampler: SamplerConfig{
			Type:        "always_on",
			SampleRatio: 1.0,
		},
		ResourceAttributes: map[string]string{
			"service.namespace": "test-namespace",
			"team":              "platform",
		},
	}

	ctx := context.Background()
	provider, err := InitializeProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to initialize OpenTelemetry provider: %v", err)
	}
	defer provider.Shutdown(ctx)

	// Verify provider components are initialized
	if provider.TracerProvider == nil {
		t.Error("TracerProvider should be initialized")
	}
	if provider.MeterProvider == nil {
		t.Error("MeterProvider should be initialized")
	}

	// Test trace functionality
	tracer := provider.TracerProvider.Tracer("test-tracer")
	_, span := tracer.Start(ctx, "test-span")
	span.SetStatus(codes.Ok, "Test completed successfully")
	span.End()

	t.Log("OpenTelemetry provider integration test completed successfully")
}

// TestFullObservabilityPipeline tests the complete observability pipeline (simplified)
func TestFullObservabilityPipeline(t *testing.T) {
	// Create test configuration
	cfg := Config{
		Enabled:        true,
		ServiceName:    "integration-test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Protocol:       "otlp_http",
		Endpoint:       "localhost:4318",
		Insecure:       true,
		Components: ComponentsConfig{
			Traces:  &[]bool{true}[0],
			Metrics: &[]bool{false}[0],
			Logs:    &[]bool{false}[0],
		},
		Sampler: SamplerConfig{
			Type:        "always_on",
			SampleRatio: 1.0,
		},
		ResourceAttributes: map[string]string{
			"service.namespace": "integration-test",
			"team":              "platform",
		},
	}

	// Initialize provider
	ctx := context.Background()
	provider, err := InitializeProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to initialize OpenTelemetry provider: %v", err)
	}
	defer provider.Shutdown(ctx)

	// Simulate a complete HTTP request flow
	tracer := provider.TracerProvider.Tracer("integration-test")
	_, span := tracer.Start(ctx, "handle_http_request")
	span.SetAttributes(
		attribute.String("http.method", "POST"),
		attribute.String("http.url", "/api/v1/process"),
		attribute.String("user.id", "test-user"),
	)

	// Complete the span
	span.SetStatus(codes.Ok, "Request processed successfully")
	span.End()

	// Give time for async processing
	time.Sleep(50 * time.Millisecond)

	t.Log("Full observability pipeline integration test completed successfully")
}

// TestConfigurationValidation tests configuration validation edge cases
func TestConfigurationValidation(t *testing.T) {
	testCases := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "minimal valid config",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Protocol:    "otlp_http",
				Endpoint:    "localhost:4318",
			},
			wantErr: false,
		},
		{
			name: "config with all components disabled",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Protocol:    "otlp_http",
				Endpoint:    "localhost:4318",
				Components: ComponentsConfig{
					Traces:  &[]bool{false}[0],
					Metrics: &[]bool{false}[0],
					Logs:    &[]bool{false}[0],
				},
			},
			wantErr: false,
		},
		{
			name: "config with invalid endpoint",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Protocol:    "otlp_http",
				Endpoint:    "http://localhost:4318",
			},
			wantErr: true,
		},
		{
			name: "config with invalid protocol",
			config: Config{
				Enabled:     true,
				ServiceName: "test-service",
				Protocol:    "invalid",
				Endpoint:    "localhost:4318",
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			provider, err := InitializeProvider(ctx, tc.config)

			if tc.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if provider == nil {
				t.Error("Provider should not be nil")
				return
			}

			// Cleanup
			if err := provider.Shutdown(ctx); err != nil {
				t.Logf("Warning: failed to shutdown provider: %v", err)
			}
		})
	}
}
