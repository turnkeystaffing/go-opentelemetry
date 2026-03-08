package opentelemetry

import "testing"

func boolPtr(b bool) *bool { return &b }

func TestIsRedisTracingEnabled(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		expect bool
	}{
		{
			name:   "disabled when otel disabled",
			cfg:    Config{Enabled: false},
			expect: false,
		},
		{
			name:   "enabled by default when tracing enabled",
			cfg:    Config{Enabled: true},
			expect: true,
		},
		{
			name: "disabled when traces disabled",
			cfg: Config{Enabled: true, Components: ComponentsConfig{
				Traces: boolPtr(false),
			}},
			expect: false,
		},
		{
			name: "disabled when redis tracing explicitly disabled",
			cfg: Config{Enabled: true, Components: ComponentsConfig{
				Redis: RedisConfig{Tracing: boolPtr(false)},
			}},
			expect: false,
		},
		{
			name: "enabled when explicitly set",
			cfg: Config{Enabled: true, Components: ComponentsConfig{
				Redis: RedisConfig{Tracing: boolPtr(true)},
			}},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsRedisTracingEnabled()
			if got != tt.expect {
				t.Errorf("IsRedisTracingEnabled() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestIsRedisMetricsEnabled(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		expect bool
	}{
		{
			name:   "disabled when otel disabled",
			cfg:    Config{Enabled: false},
			expect: false,
		},
		{
			name:   "enabled by default",
			cfg:    Config{Enabled: true},
			expect: true,
		},
		{
			name: "disabled when metrics disabled",
			cfg: Config{Enabled: true, Components: ComponentsConfig{
				Metrics: boolPtr(false),
			}},
			expect: false,
		},
		{
			name: "disabled when redis metrics explicitly disabled",
			cfg: Config{Enabled: true, Components: ComponentsConfig{
				Redis: RedisConfig{Metrics: boolPtr(false)},
			}},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsRedisMetricsEnabled()
			if got != tt.expect {
				t.Errorf("IsRedisMetricsEnabled() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestIsS3TracingEnabled(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		expect bool
	}{
		{
			name:   "disabled when otel disabled",
			cfg:    Config{Enabled: false},
			expect: false,
		},
		{
			name:   "enabled by default",
			cfg:    Config{Enabled: true},
			expect: true,
		},
		{
			name: "disabled when traces disabled",
			cfg: Config{Enabled: true, Components: ComponentsConfig{
				Traces: boolPtr(false),
			}},
			expect: false,
		},
		{
			name: "disabled when s3 tracing explicitly disabled",
			cfg: Config{Enabled: true, Components: ComponentsConfig{
				S3: S3Config{Tracing: boolPtr(false)},
			}},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsS3TracingEnabled()
			if got != tt.expect {
				t.Errorf("IsS3TracingEnabled() = %v, want %v", got, tt.expect)
			}
		})
	}
}
