package config

import (
	"strings"
	"testing"
)

func TestS3NeedsCredentialsWhenAnEndpointIsSet(t *testing.T) {
	t.Setenv("ARMATURE_DB_PRIMARY_URL", "postgres://x")
	t.Setenv("ARMATURE_S3_ENDPOINT", "seaweedfs:8333")
	t.Setenv("ARMATURE_S3_ACCESS_KEY", "")
	t.Setenv("ARMATURE_S3_SECRET_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("an endpoint without credentials should be refused")
	}
	for _, want := range []string{"ARMATURE_S3_ACCESS_KEY", "ARMATURE_S3_SECRET_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name %s:\n%v", want, err)
		}
	}

	t.Setenv("ARMATURE_S3_ACCESS_KEY", "k")
	t.Setenv("ARMATURE_S3_SECRET_KEY", "s")
	if _, err := Load(); err != nil {
		t.Fatalf("with credentials the configuration should load: %v", err)
	}
}

func TestNoEndpointMeansNoS3Requirements(t *testing.T) {
	t.Setenv("ARMATURE_DB_PRIMARY_URL", "postgres://x")
	t.Setenv("ARMATURE_S3_ENDPOINT", "")
	if _, err := Load(); err != nil {
		t.Fatalf("attachments off should be a valid configuration: %v", err)
	}
}

func TestTelemetryDefaultsAndOff(t *testing.T) {
	t.Setenv("ARMATURE_DB_PRIMARY_URL", "postgres://x")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Telemetry.MetricsAddr != ":9090" || c.Telemetry.OTLPEndpoint != "" || c.Telemetry.SampleRatio != 1 {
		t.Fatalf("defaults = %+v", c.Telemetry)
	}

	// Blank on purpose is off, unlike every other setting where blank is unset.
	t.Setenv("ARMATURE_METRICS_ADDR", "")
	t.Setenv("ARMATURE_OTEL_ENDPOINT", "http://jaeger:4318/v1/traces")
	t.Setenv("ARMATURE_OTEL_SAMPLE_RATIO", "0.25")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Telemetry.MetricsAddr != "" || c.Telemetry.OTLPEndpoint != "http://jaeger:4318/v1/traces" || c.Telemetry.SampleRatio != 0.25 {
		t.Fatalf("set = %+v", c.Telemetry)
	}

	t.Setenv("ARMATURE_OTEL_SAMPLE_RATIO", "7")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ARMATURE_OTEL_SAMPLE_RATIO") {
		t.Fatalf("a ratio past one should be refused: %v", err)
	}
}

func TestSignupPolicy(t *testing.T) {
	t.Setenv("ARMATURE_DB_PRIMARY_URL", "postgres://x")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Signup != "open" {
		t.Errorf("signup = %q, want open by default so development keeps its sign-up page", cfg.Auth.Signup)
	}

	for _, policy := range []string{"open", "first", "closed"} {
		t.Setenv("ARMATURE_SIGNUP", policy)
		if _, err := Load(); err != nil {
			t.Errorf("%s should be accepted: %v", policy, err)
		}
	}

	// A typo must not quietly leave sign-up open.
	t.Setenv("ARMATURE_SIGNUP", "disabled")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ARMATURE_SIGNUP") {
		t.Errorf("an unknown policy should be refused and named, got %v", err)
	}
}
