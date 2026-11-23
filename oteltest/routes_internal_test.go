package oteltest

import "testing"

func TestSampleRoute(t *testing.T) {
	uuid := "{id:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}}"
	tests := []struct {
		route, wantTmpl, wantURL string
		params                   map[string]string
	}{
		{"/health", "/health", "/health", nil},
		{"/users/{id}", "/users/{id}", "/users/x", nil},
		{"/checks/" + uuid + "/reports", "/checks/{id}/reports", "/checks/3f2b8c1e-9a4d-4e2b-8c3a-1f2e3d4c5b6a/reports", nil},
		{"/n/{n:\\d+}", "/n/{n}", "/n/1", nil},
		{"/list/{page?}", "/list/{page}", "/list/x", nil},
		{"/docs/{filepath:*}", "/docs/{filepath}", "/docs/a/b", nil},
		{"/hooks/{provider:github|gitlab}", "/hooks/{provider}", "/hooks/gh", map[string]string{"provider": "gh"}},
	}
	for _, tt := range tests {
		tmpl, url, err := sampleRoute(tt.route, tt.params)
		if err != nil || tmpl != tt.wantTmpl || url != tt.wantURL {
			t.Errorf("sampleRoute(%q) = %q, %q, %v; want %q, %q", tt.route, tmpl, url, err, tt.wantTmpl, tt.wantURL)
		}
	}
	for _, bad := range []string{"/hooks/{provider:github|gitlab}", "/b/{id:[0-9]+", "/r/{x:(}"} {
		if _, _, err := sampleRoute(bad, nil); err == nil {
			t.Errorf("sampleRoute(%q): want an error", bad)
		}
	}
}
