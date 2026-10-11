package config

import (
	"strings"
	"testing"
)

// deployed sets what a staging or production process needs apart from the two
// addresses under test.
func deployed(t *testing.T, env string) {
	t.Helper()
	t.Setenv("ARMATURE_DB_PRIMARY_URL", "postgres://x")
	t.Setenv("ARMATURE_ENV", env)
	t.Setenv("ARMATURE_SECURE_COOKIES", "true")
}

// A deployment that leaves these unset would send sign-ins and mailed links to
// localhost and refuse writes from its real origin, so it must not start.
func TestRefusesLocalhostAddressesOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		deployed(t, env)
		t.Setenv("ARMATURE_APP_URL", "")
		t.Setenv("ARMATURE_OIDC_REDIRECT_URL", "")

		_, err := Load()
		if err == nil {
			t.Fatalf("%s: the localhost defaults should be refused", env)
		}
		for _, want := range []string{"ARMATURE_APP_URL", "ARMATURE_OIDC_REDIRECT_URL"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the error should name %s:\n%v", env, want, err)
			}
		}

		t.Setenv("ARMATURE_APP_URL", "http://127.0.0.1:5173")
		t.Setenv("ARMATURE_OIDC_REDIRECT_URL", "http://[::1]:8080/api/v1/auth/oidc/callback")
		if _, err := Load(); err == nil {
			t.Errorf("%s: a loopback address set by hand should be refused like the default", env)
		}

		t.Setenv("ARMATURE_APP_URL", "https://armature.example.com/")
		t.Setenv("ARMATURE_OIDC_REDIRECT_URL", "https://armature.example.com/api/v1/auth/oidc/callback")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("%s: real addresses should load: %v", env, err)
		}
		if cfg.Mail.AppBaseURL != "https://armature.example.com" {
			t.Errorf("%s: app url = %q, want it without the trailing slash links are joined to", env, cfg.Mail.AppBaseURL)
		}
	}
}

func TestDevelopmentKeepsTheLocalhostDefaults(t *testing.T) {
	t.Setenv("ARMATURE_DB_PRIMARY_URL", "postgres://x")
	t.Setenv("ARMATURE_ENV", "development")
	t.Setenv("ARMATURE_APP_URL", "")
	t.Setenv("ARMATURE_OIDC_REDIRECT_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("development should start on its defaults: %v", err)
	}
	if cfg.Mail.AppBaseURL != "http://localhost:5173" {
		t.Errorf("app url = %q, want the dev server", cfg.Mail.AppBaseURL)
	}
}

func TestLagSamplesMustBeAtLeastOne(t *testing.T) {
	t.Setenv("ARMATURE_DB_PRIMARY_URL", "postgres://x")
	for _, bad := range []string{"0", "-3"} {
		t.Setenv("ARMATURE_DB_REPLICA_LAG_SAMPLES", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ARMATURE_DB_REPLICA_LAG_SAMPLES") {
			t.Errorf("%s samples should be refused by name, got %v", bad, err)
		}
	}
	t.Setenv("ARMATURE_DB_REPLICA_LAG_SAMPLES", "5")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.ReplicaLagSamples != 5 {
		t.Errorf("samples = %d, want 5", cfg.DB.ReplicaLagSamples)
	}
}
