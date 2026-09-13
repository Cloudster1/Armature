package httpapi

import "testing"

func TestRedactPathKeepsASharedTokenOutOfTheLog(t *testing.T) {
	for path, want := range map[string]string{
		"/api/v1/shared/abc123":            "/api/v1/shared/{token}",
		"/api/v1/shared/abc123/widgets/w1": "/api/v1/shared/{token}/widgets/w1",
		"/api/v1/projects/CP/dashboards":   "/api/v1/projects/CP/dashboards",
		"/api/v1/dashboards/d1/shares":     "/api/v1/dashboards/d1/shares",
	} {
		if got := redactPath(path); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", path, got, want)
		}
	}
}
