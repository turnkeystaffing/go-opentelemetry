package opentelemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewInstrumentedHTTPClient creates an HTTP client with OpenTelemetry instrumentation.
// Spans go to the global tracer provider; before InitializeProvider runs they are no-ops.
// This client automatically injects standard W3C traceparent headers for external service communication
// (NOT the internal x-trace-* headers used for database storage)
func NewInstrumentedHTTPClient(baseClient *http.Client) *http.Client {
	if baseClient == nil {
		baseClient = http.DefaultClient
	}

	// Create instrumented client with automatic W3C traceparent header injection
	return &http.Client{
		Transport:     otelhttp.NewTransport(baseClient.Transport),
		CheckRedirect: baseClient.CheckRedirect,
		Jar:           baseClient.Jar,
		Timeout:       baseClient.Timeout,
	}
}

// NewInstrumentedHTTPClientWithOptions creates an HTTP client with OpenTelemetry instrumentation and custom options.
func NewInstrumentedHTTPClientWithOptions(baseClient *http.Client, opts ...otelhttp.Option) *http.Client {
	if baseClient == nil {
		baseClient = http.DefaultClient
	}

	// Create instrumented client
	return &http.Client{
		Transport:     otelhttp.NewTransport(baseClient.Transport, opts...),
		CheckRedirect: baseClient.CheckRedirect,
		Jar:           baseClient.Jar,
		Timeout:       baseClient.Timeout,
	}
}
