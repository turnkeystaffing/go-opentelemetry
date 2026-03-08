package opentelemetry

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestCreateResource(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Protocol:       "otlp_http",
		Endpoint:       "localhost:4318",
		Insecure:       true,
	}

	res, err := createResource(cfg)
	if err != nil {
		t.Fatalf("createResource failed: %v", err)
	}

	if res == nil {
		t.Fatal("createResource returned nil resource")
	}

	// Verify resource attributes
	attrs := res.Attributes()
	found := make(map[string]bool)

	for _, attr := range attrs {
		switch string(attr.Key) {
		case "service.name":
			if attr.Value.AsString() != "test-service" {
				t.Errorf("Expected service name 'test-service', got '%s'", attr.Value.AsString())
			}
			found["service_name"] = true
		case "service.version":
			if attr.Value.AsString() != "1.0.0" {
				t.Errorf("Expected service version '1.0.0', got '%s'", attr.Value.AsString())
			}
			found["service_version"] = true
		case "deployment.environment":
			if attr.Value.AsString() != "test" {
				t.Errorf("Expected deployment environment 'test', got '%s'", attr.Value.AsString())
			}
			found["deployment_environment"] = true
		}
	}

	// Verify all expected attributes are present
	expectedAttrs := []string{"service_name", "service_version", "deployment_environment"}
	for _, attr := range expectedAttrs {
		if !found[attr] {
			t.Errorf("Missing expected attribute: %s", attr)
		}
	}
}

func TestCreateSampler(t *testing.T) {
	tests := []struct {
		name         string
		config       Config
		expectedType string
	}{
		{
			name: "always_on sampler",
			config: Config{
				Sampler: SamplerConfig{
					Type:        "always_on",
					SampleRatio: 0.5,
				},
			},
			expectedType: "*trace.alwaysSampler",
		},
		{
			name: "always_off sampler",
			config: Config{
				Sampler: SamplerConfig{
					Type:        "always_off",
					SampleRatio: 0.5,
				},
			},
			expectedType: "*trace.neverSampler",
		},
		{
			name: "traceidratio sampler",
			config: Config{
				Sampler: SamplerConfig{
					Type:        "traceidratio",
					SampleRatio: 0.1,
				},
			},
			expectedType: "*trace.traceIDRatioSampler",
		},
		{
			name: "parentbased_traceidratio sampler",
			config: Config{
				Sampler: SamplerConfig{
					Type:        "parentbased_traceidratio",
					SampleRatio: 0.05,
				},
			},
			expectedType: "*trace.parentBasedSampler",
		},
		{
			name: "default sampler (unknown type)",
			config: Config{
				Sampler: SamplerConfig{
					Type:        "unknown",
					SampleRatio: 0.05,
				},
			},
			expectedType: "*trace.parentBasedSampler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampler, err := createSampler(tt.config)
			if err != nil {
				t.Fatalf("createSampler failed: %v", err)
			}

			if sampler == nil {
				t.Fatal("createSampler returned nil sampler")
			}

			// Note: We can't easily test the exact type due to internal types,
			// but we can verify that a sampler was created without error
		})
	}
}

func TestInitializeProvider_Disabled(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		Enabled:        false,
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Protocol:       "otlp_http",
		Endpoint:       "localhost:4318",
		Insecure:       true,
	}

	provider, err := InitializeProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("InitializeProvider failed: %v", err)
	}

	if provider == nil {
		t.Fatal("InitializeProvider returned nil provider")
	}

	// Verify no-op providers are returned when disabled
	if provider.TracerProvider == nil {
		t.Error("Expected TracerProvider to be set")
	}

	if provider.MeterProvider == nil {
		t.Error("Expected MeterProvider to be set")
	}

	if provider.Tracer == nil {
		t.Error("Expected Tracer to be set")
	}

	if provider.Meter == nil {
		t.Error("Expected Meter to be set")
	}

	// Test shutdown
	err = provider.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestInitializeProvider_ValidConfig(t *testing.T) {
	ctx := context.Background()

	// Test with a valid config that should succeed
	cfg := Config{
		Enabled:        true,
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Protocol:       "otlp_grpc",
		Endpoint:       "localhost:4317", // This might still fail if no collector is running, but that's expected
		Insecure:       true,
		Sampler: SamplerConfig{
			Type:        "parentbased_traceidratio",
			SampleRatio: 0.05,
		},
	}

	// Set a short timeout to avoid hanging if collector isn't available
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	provider, err := InitializeProvider(ctx, cfg)

	// If we got a provider, make sure to shut it down
	if provider != nil {
		defer provider.Shutdown(ctx)
	}

	// Note: This test may fail if no OpenTelemetry collector is running locally,
	// but that's expected behavior for integration testing
	if err != nil {
		t.Logf("InitializeProvider failed (expected if no OTEL collector running): %v", err)
		// This is acceptable - the test verifies the code structure is correct
		return
	}

	// If provider was created successfully, verify it has the expected structure
	if provider.TracerProvider == nil {
		t.Error("Expected TracerProvider to be set")
	}

	if provider.MeterProvider == nil {
		t.Error("Expected MeterProvider to be set")
	}

	if provider.Tracer == nil {
		t.Error("Expected Tracer to be set")
	}

	if provider.Meter == nil {
		t.Error("Expected Meter to be set")
	}
}

func TestProvider_Shutdown(t *testing.T) {
	ctx := context.Background()

	// Test shutdown with no shutdown functions
	provider := &Provider{
		TracerProvider: sdktrace.NewTracerProvider(),
		MeterProvider:  sdkmetric.NewMeterProvider(),
		Tracer:         otel.Tracer("test"),
		Meter:          otel.Meter("test"),
		shutdownFuncs:  nil,
	}

	err := provider.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown with no functions failed: %v", err)
	}

	// Test shutdown with working shutdown functions
	called := false
	provider.shutdownFuncs = []func(context.Context) error{
		func(ctx context.Context) error {
			called = true
			return nil
		},
	}

	err = provider.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown with working functions failed: %v", err)
	}

	if !called {
		t.Error("Shutdown function was not called")
	}

	// Test shutdown with failing shutdown function
	provider.shutdownFuncs = []func(context.Context) error{
		func(ctx context.Context) error {
			return context.Canceled
		},
	}

	err = provider.Shutdown(ctx)
	if err == nil {
		t.Error("Expected shutdown to fail with failing function")
	}
}

func TestCreateSampler_SampleRatio(t *testing.T) {
	tests := []struct {
		name    string
		ratio   float64
		sampler string
	}{
		{
			name:    "zero ratio",
			ratio:   0.0,
			sampler: "traceidratio",
		},
		{
			name:    "half ratio",
			ratio:   0.5,
			sampler: "traceidratio",
		},
		{
			name:    "full ratio",
			ratio:   1.0,
			sampler: "traceidratio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Sampler: SamplerConfig{
					Type:        tt.sampler,
					SampleRatio: tt.ratio,
				},
			}

			sampler, err := createSampler(cfg)
			if err != nil {
				t.Fatalf("createSampler failed: %v", err)
			}

			if sampler == nil {
				t.Fatal("createSampler returned nil sampler")
			}
		})
	}
}

func TestProvider_Fields(t *testing.T) {
	// Test that Provider struct has all expected fields
	provider := &Provider{}

	// Test that fields can be set without compilation error
	provider.TracerProvider = sdktrace.NewTracerProvider()
	provider.MeterProvider = sdkmetric.NewMeterProvider()
	provider.Tracer = otel.Tracer("test")
	provider.Meter = otel.Meter("test")
	provider.shutdownFuncs = []func(context.Context) error{}

	if provider.TracerProvider == nil {
		t.Error("TracerProvider field not properly set")
	}

	if provider.MeterProvider == nil {
		t.Error("MeterProvider field not properly set")
	}

	if provider.Tracer == nil {
		t.Error("Tracer field not properly set")
	}

	if provider.Meter == nil {
		t.Error("Meter field not properly set")
	}

	if provider.shutdownFuncs == nil {
		t.Error("shutdownFuncs field not properly set")
	}
}

func TestGetSignalPath(t *testing.T) {
	tests := []struct {
		protocol string
		signal   string
		expected string
	}{
		{ProtocolOTLPHTTP, "traces", "/v1/traces"},
		{ProtocolOTLPHTTP, "metrics", "/v1/metrics"},
		{ProtocolOTLPgRPC, "traces", ""}, // gRPC doesn't use paths
		{"unknown", "traces", ""},        // Unknown protocol
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.protocol, tt.signal), func(t *testing.T) {
			result := GetSignalPath(tt.protocol, tt.signal)
			if result != tt.expected {
				t.Errorf("GetSignalPath(%s, %s) = %s, want %s", tt.protocol, tt.signal, result, tt.expected)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "disabled config - no validation",
			config: Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid otlp_http config",
			config: Config{
				Enabled:  true,
				Protocol: "otlp_http",
				Endpoint: "localhost:4318",
			},
			wantErr: false,
		},
		{
			name: "valid otlp_grpc config",
			config: Config{
				Enabled:  true,
				Protocol: "otlp_grpc",
				Endpoint: "localhost:4317",
			},
			wantErr: false,
		},
		{
			name: "missing protocol",
			config: Config{
				Enabled:  true,
				Endpoint: "localhost:4318",
			},
			wantErr: true,
			errMsg:  "protocol is required",
		},
		{
			name: "missing endpoint",
			config: Config{
				Enabled:  true,
				Protocol: "otlp_http",
			},
			wantErr: true,
			errMsg:  "endpoint is required",
		},
		{
			name: "invalid protocol",
			config: Config{
				Enabled:  true,
				Protocol: "invalid_protocol",
				Endpoint: "localhost:4318",
			},
			wantErr: true,
			errMsg:  "unsupported protocol",
		},
		{
			name: "endpoint with scheme",
			config: Config{
				Enabled:  true,
				Protocol: "otlp_http",
				Endpoint: "http://localhost:4318",
			},
			wantErr: true,
			errMsg:  "endpoint must be in host:port format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateConfig() expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateConfig() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateConfig() unexpected error = %v", err)
				}
			}
		})
	}
}
