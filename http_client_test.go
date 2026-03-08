package opentelemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func TestNewInstrumentedHTTPClient(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	// Create an instrumented HTTP client
	baseClient := &http.Client{Timeout: 10 * time.Second}
	instrumentedClient := NewInstrumentedHTTPClient(baseClient)

	// Verify client properties are preserved
	if instrumentedClient.Timeout != baseClient.Timeout {
		t.Errorf("Expected timeout %v, got %v", baseClient.Timeout, instrumentedClient.Timeout)
	}

	// Verify the client has the instrumented transport
	if instrumentedClient.Transport == nil {
		t.Error("Expected instrumented transport to be set")
	}

	// Make a request with the instrumented client
	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := instrumentedClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}

	// Basic functionality test - the instrumentation should not interfere with normal HTTP operations
	// Detailed span testing would require more complex setup and is better tested in integration tests
}

func TestNewInstrumentedHTTPClient_NilClient(t *testing.T) {
	// Test with nil base client
	instrumentedClient := NewInstrumentedHTTPClient(nil)

	if instrumentedClient == nil {
		t.Error("Expected non-nil instrumented client")
	}

	// Should use default timeout since we started with nil
	if instrumentedClient.Timeout != 0 {
		t.Errorf("Expected default timeout (0), got %v", instrumentedClient.Timeout)
	}

	// Verify the client has the instrumented transport
	if instrumentedClient.Transport == nil {
		t.Error("Expected instrumented transport to be set")
	}
}

func TestNewInstrumentedHTTPClientWithOptions(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	// Create an instrumented HTTP client with custom options
	baseClient := &http.Client{Timeout: 5 * time.Second}
	instrumentedClient := NewInstrumentedHTTPClientWithOptions(
		baseClient,
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			return "custom-" + operation
		}),
	)

	// Verify client properties are preserved
	if instrumentedClient.Timeout != baseClient.Timeout {
		t.Errorf("Expected timeout %v, got %v", baseClient.Timeout, instrumentedClient.Timeout)
	}

	// Verify the client has the instrumented transport
	if instrumentedClient.Transport == nil {
		t.Error("Expected instrumented transport to be set")
	}

	// Make a request with the instrumented client
	req, err := http.NewRequest("POST", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := instrumentedClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Verify the response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}

	// The custom options are applied to the transport - detailed testing would be done in integration tests
}
