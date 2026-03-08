package opentelemetry

// Config contains OpenTelemetry configuration that is independent of application-specific config
type Config struct {
	// Enabled toggles the entire OpenTelemetry system
	Enabled bool

	// ServiceName is the name of your service, e.g., "webhook-service"
	ServiceName string

	// ServiceVersion is the version of your service
	ServiceVersion string

	// Environment is the environment name, e.g., "development", "staging", "production"
	Environment string

	// Protocol specifies the telemetry protocol: "otlp_grpc", "otlp_http"
	Protocol string

	// Endpoint is the collector endpoint in host:port format, e.g., "localhost:4318", "otel-collector:4317"
	Endpoint string

	// Insecure enables insecure connections (no TLS). Defaults to false for security.
	// Set to true for local development or when TLS is terminated by a proxy
	Insecure bool

	// Components provides granular control over OpenTelemetry signals
	Components ComponentsConfig

	// Sampler contains sampling configuration
	Sampler SamplerConfig

	// ResourceAttributes contains optional resource attributes
	ResourceAttributes map[string]string
}

// ComponentsConfig provides granular control over OpenTelemetry components
type ComponentsConfig struct {
	// Traces enables/disables trace collection and export
	Traces *bool

	// Metrics enables/disables metrics collection and export
	Metrics *bool

	// Logs enables/disables log forwarding to OTLP collector
	Logs *bool

	// Database contains database-specific observability configuration
	Database DatabaseConfig

	// Redis contains Redis-specific observability configuration
	Redis RedisConfig

	// S3 contains S3-specific observability configuration
	S3 S3Config
}

// DatabaseConfig controls database observability features
type DatabaseConfig struct {
	// Tracing enables/disables database operation tracing
	Tracing *bool

	// Metrics enables/disables database metrics collection (connection stats, query metrics)
	Metrics *bool
}

// RedisConfig controls Redis observability features
type RedisConfig struct {
	// Tracing enables/disables Redis operation tracing via redisotel
	Tracing *bool

	// Metrics enables/disables Redis metrics collection via redisotel
	Metrics *bool
}

// S3Config controls S3 observability features
type S3Config struct {
	// Tracing enables/disables S3 operation tracing
	Tracing *bool
}

// SamplerConfig contains OpenTelemetry sampling configuration
type SamplerConfig struct {
	// Type is the sampling strategy: "always_on", "always_off", "traceidratio", "parentbased_traceidratio"
	Type string

	// SampleRatio is the ratio of traces to sample. 1.0 means 100%, 0.05 means 5%
	SampleRatio float64
}

// Helper methods for Config to check component enablement
// Uses pointer fields for explicit nil checking and backward compatibility

// IsTracingEnabled returns true if tracing is enabled (default: true when OpenTelemetry is enabled)
func (cfg *Config) IsTracingEnabled() bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.Components.Traces == nil {
		return true // Default to enabled
	}
	return *cfg.Components.Traces
}

// IsMetricsEnabled returns true if metrics collection is enabled (default: true when OpenTelemetry is enabled)
func (cfg *Config) IsMetricsEnabled() bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.Components.Metrics == nil {
		return true // Default to enabled
	}
	return *cfg.Components.Metrics
}

// IsLogsEnabled returns true if log forwarding to OTLP is enabled (default: false for backward compatibility)
func (cfg *Config) IsLogsEnabled() bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.Components.Logs == nil {
		return false // Default to disabled for backward compatibility
	}
	return *cfg.Components.Logs
}

// IsDatabaseTracingEnabled returns true if database tracing is enabled
func (cfg *Config) IsDatabaseTracingEnabled() bool {
	if !cfg.IsTracingEnabled() {
		return false
	}
	if cfg.Components.Database.Tracing == nil {
		return true // Default to enabled when tracing is enabled
	}
	return *cfg.Components.Database.Tracing
}

// IsDatabaseMetricsEnabled returns true if database metrics are enabled
func (cfg *Config) IsDatabaseMetricsEnabled() bool {
	if !cfg.IsMetricsEnabled() {
		return false
	}
	if cfg.Components.Database.Metrics == nil {
		return true // Default to enabled when metrics are enabled
	}
	return *cfg.Components.Database.Metrics
}

// IsRedisTracingEnabled returns true if Redis tracing is enabled
func (cfg *Config) IsRedisTracingEnabled() bool {
	if !cfg.IsTracingEnabled() {
		return false
	}
	if cfg.Components.Redis.Tracing == nil {
		return true // Default to enabled when tracing is enabled
	}
	return *cfg.Components.Redis.Tracing
}

// IsRedisMetricsEnabled returns true if Redis metrics are enabled
func (cfg *Config) IsRedisMetricsEnabled() bool {
	if !cfg.IsMetricsEnabled() {
		return false
	}
	if cfg.Components.Redis.Metrics == nil {
		return true // Default to enabled when metrics are enabled
	}
	return *cfg.Components.Redis.Metrics
}

// IsS3TracingEnabled returns true if S3 tracing is enabled
func (cfg *Config) IsS3TracingEnabled() bool {
	if !cfg.IsTracingEnabled() {
		return false
	}
	if cfg.Components.S3.Tracing == nil {
		return true // Default to enabled when tracing is enabled
	}
	return *cfg.Components.S3.Tracing
}
