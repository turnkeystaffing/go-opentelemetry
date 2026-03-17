package opentelemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	// OpenTelemetry schema URL for resource attributes
	schemaURL = "https://opentelemetry.io/schemas/1.24.0"

	// Protocol constants for telemetry configuration
	ProtocolOTLPgRPC = "otlp_grpc"
	ProtocolOTLPHTTP = "otlp_http"
)

// signalPathMap maps protocol and signal type to their default paths
var signalPathMap = map[string]map[string]string{
	ProtocolOTLPHTTP: {
		"traces":  "/v1/traces",
		"metrics": "/v1/metrics",
		"logs":    "/v1/logs",
	},
	// OTLP gRPC does not use HTTP paths
}

// GetSignalPath returns the default path for a given protocol and signal type
func GetSignalPath(protocol, signal string) string {
	if paths, ok := signalPathMap[protocol]; ok {
		return paths[signal]
	}
	return ""
}

// validateConfig validates the OpenTelemetry configuration
func validateConfig(cfg Config) error {
	if !cfg.Enabled {
		return nil // No validation needed if disabled
	}

	if cfg.Protocol == "" {
		return fmt.Errorf("protocol is required when OpenTelemetry is enabled")
	}

	if cfg.Endpoint == "" {
		return fmt.Errorf("endpoint is required when OpenTelemetry is enabled")
	}

	// Validate protocol is supported
	switch cfg.Protocol {
	case ProtocolOTLPgRPC, ProtocolOTLPHTTP:
		// Valid protocols
	default:
		return fmt.Errorf("unsupported protocol %q. Must be one of: %s, %s",
			cfg.Protocol, ProtocolOTLPgRPC, ProtocolOTLPHTTP)
	}

	// Validate endpoint format (should not contain scheme for new protocol-based system)
	if strings.Contains(cfg.Endpoint, "://") {
		return fmt.Errorf("endpoint must be in host:port format (e.g., 'localhost:4318') and must not include a scheme when using protocol-based configuration")
	}

	return nil
}

// Provider holds the OpenTelemetry providers and cleanup functions
type Provider struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
	Propagator     propagation.TextMapPropagator
	Tracer         trace.Tracer
	Meter          metric.Meter
	shutdownFuncs  []func(context.Context) error
}

// Shutdown gracefully shuts down all OpenTelemetry providers
func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error
	for _, fn := range p.shutdownFuncs {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// InitializeProvider initializes OpenTelemetry with the given configuration
func InitializeProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled {
		// Return a no-op provider when disabled
		return &Provider{
			TracerProvider: sdktrace.NewTracerProvider(),
			MeterProvider:  sdkmetric.NewMeterProvider(),
			Propagator:     propagation.NewCompositeTextMapPropagator(),
			Tracer:         otel.Tracer("noop"),
			Meter:          otel.Meter("noop"),
		}, nil
	}

	// Create resource with service information
	res, err := createResource(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Validate configuration
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid OpenTelemetry configuration: %w", err)
	}

	var shutdownFuncs []func(context.Context) error

	// Initialize trace provider conditionally
	var tracerProvider *sdktrace.TracerProvider
	var tracer trace.Tracer
	if cfg.IsTracingEnabled() {
		tp, traceShutdown, err := initTraceProvider(ctx, cfg, res)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize trace provider: %w", err)
		}
		tracerProvider = tp
		shutdownFuncs = append(shutdownFuncs, traceShutdown)
		otel.SetTracerProvider(tracerProvider)
		tracer = tracerProvider.Tracer("get-native-auth", trace.WithInstrumentationVersion("1.0.0"))
	} else {
		tracerProvider = sdktrace.NewTracerProvider()
		tracer = otel.Tracer("noop")
	}

	// Initialize metrics provider conditionally
	var meterProvider *sdkmetric.MeterProvider
	var meter metric.Meter
	if cfg.IsMetricsEnabled() {
		mp, metricShutdown, err := initMeterProvider(ctx, cfg, res)
		if err != nil {
			// For protocols that don't support metrics, we create a no-op meter provider
			meterProvider = sdkmetric.NewMeterProvider()
			metricShutdown = func(context.Context) error { return nil }
		} else {
			meterProvider = mp
			shutdownFuncs = append(shutdownFuncs, metricShutdown)
			otel.SetMeterProvider(meterProvider)
		}
		meter = meterProvider.Meter("get-native-auth", metric.WithInstrumentationVersion("1.0.0"))
	} else {
		meterProvider = sdkmetric.NewMeterProvider()
		meter = otel.Meter("noop")
	}

	// Initialize log provider conditionally
	var loggerProvider *sdklog.LoggerProvider
	if cfg.IsLogsEnabled() {
		lp, logShutdown, err := initLogProvider(ctx, cfg, res)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize log provider: %w", err)
		}
		loggerProvider = lp
		shutdownFuncs = append(shutdownFuncs, logShutdown)
		global.SetLoggerProvider(loggerProvider)
	}

	// Set up propagator for distributed tracing
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	return &Provider{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
		LoggerProvider: loggerProvider,
		Propagator:     propagator,
		Tracer:         tracer,
		Meter:          meter,
		shutdownFuncs:  shutdownFuncs,
	}, nil
}

// createResource creates an OpenTelemetry resource with service information
func createResource(cfg Config) (*resource.Resource, error) {
	// Start with basic attributes using attribute package directly
	attributes := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironment(cfg.Environment),
	}

	// Add custom resource attributes if provided
	for key, value := range cfg.ResourceAttributes {
		// Map simple keys to proper OpenTelemetry resource attribute keys
		var attrKey string
		switch key {
		case "namespace":
			attrKey = "service.namespace"
		case "deployment":
			attrKey = "deployment.environment"
		default:
			attrKey = key
		}
		attributes = append(attributes, attribute.String(attrKey, value))
	}

	return resource.NewWithAttributes(
		schemaURL,
		attributes...,
	), nil
}

// initTraceProvider initializes the trace provider with appropriate exporter based on protocol
func initTraceProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, func(context.Context) error, error) {
	// Create trace exporter based on protocol configuration
	traceExporter, err := createTraceExporter(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create sampler based on configuration
	sampler, err := createSampler(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create sampler: %w", err)
	}

	// Create trace provider with batch span processor
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(100),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	return tracerProvider, tracerProvider.Shutdown, nil
}

// createTraceExporter creates the appropriate trace exporter based on protocol configuration
func createTraceExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	if cfg.Protocol == "" {
		return nil, fmt.Errorf("protocol is required")
	}

	// Validate endpoint format for new protocol-based system
	if strings.Contains(cfg.Endpoint, "://") {
		return nil, fmt.Errorf("endpoint must be in host:port format (e.g., 'localhost:4318') and must not include a scheme when using protocol-based configuration")
	}

	switch cfg.Protocol {
	case ProtocolOTLPgRPC:
		// OTLP gRPC - endpoint is host:port
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		return otlptracegrpc.New(ctx, opts...)

	case ProtocolOTLPHTTP:
		// OTLP HTTP - construct full endpoint with path
		tracesPath := GetSignalPath(ProtocolOTLPHTTP, "traces")

		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(cfg.Endpoint),
			otlptracehttp.WithURLPath(tracesPath),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)

	default:
		return nil, fmt.Errorf("unsupported protocol: %s. Must be one of: %s, %s",
			cfg.Protocol, ProtocolOTLPgRPC, ProtocolOTLPHTTP)
	}
}

// initMeterProvider initializes the meter provider with appropriate exporter based on protocol
func initMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdkmetric.MeterProvider, func(context.Context) error, error) {
	// Create metric exporter based on protocol configuration (metrics only support OTLP)
	metricExporter, err := createMetricExporter(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// Create meter provider with periodic reader
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExporter,
				sdkmetric.WithInterval(30*time.Second), // Export metrics every 30 seconds
			),
		),
		sdkmetric.WithResource(res),
		// Note: Attribute filtering for metrics will be implemented at instrumentation level
		// to avoid high-cardinality attributes like organization.id, request.id, trace.id
	)

	return meterProvider, meterProvider.Shutdown, nil
}

// createMetricExporter creates the appropriate metric exporter based on protocol configuration
func createMetricExporter(ctx context.Context, cfg Config) (sdkmetric.Exporter, error) {
	// Metrics only support OTLP protocols
	switch cfg.Protocol {
	case ProtocolOTLPgRPC:
		// OTLP gRPC for metrics
		opts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		return otlpmetricgrpc.New(ctx, opts...)

	case ProtocolOTLPHTTP:
		// OTLP HTTP for metrics - construct endpoint with metrics path
		metricsPath := GetSignalPath(ProtocolOTLPHTTP, "metrics")

		opts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(cfg.Endpoint),
			otlpmetrichttp.WithURLPath(metricsPath),
		}
		if cfg.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		return otlpmetrichttp.New(ctx, opts...)

	default:
		return nil, fmt.Errorf("unsupported protocol for metrics: %s. Metrics only support OTLP protocols", cfg.Protocol)
	}
}

// createSampler creates a sampler based on configuration
func createSampler(cfg Config) (sdktrace.Sampler, error) {
	switch cfg.Sampler.Type {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(cfg.Sampler.SampleRatio), nil
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Sampler.SampleRatio)), nil
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Sampler.SampleRatio)), nil
	}
}

// initLogProvider initializes the log provider with appropriate exporter based on protocol
func initLogProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdklog.LoggerProvider, func(context.Context) error, error) {
	logExporter, err := createLogExporter(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create log exporter: %w", err)
	}

	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)

	return loggerProvider, loggerProvider.Shutdown, nil
}

// createLogExporter creates the appropriate log exporter based on protocol configuration
func createLogExporter(ctx context.Context, cfg Config) (sdklog.Exporter, error) {
	switch cfg.Protocol {
	case ProtocolOTLPgRPC:
		opts := []otlploggrpc.Option{
			otlploggrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlploggrpc.WithInsecure())
		}
		return otlploggrpc.New(ctx, opts...)

	case ProtocolOTLPHTTP:
		logsPath := GetSignalPath(ProtocolOTLPHTTP, "logs")
		opts := []otlploghttp.Option{
			otlploghttp.WithEndpoint(cfg.Endpoint),
			otlploghttp.WithURLPath(logsPath),
		}
		if cfg.Insecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		return otlploghttp.New(ctx, opts...)

	default:
		return nil, fmt.Errorf("unsupported protocol for logs: %s", cfg.Protocol)
	}
}
