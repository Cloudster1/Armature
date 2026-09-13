package oidc

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackchannelReachesTheProviderUnderItsPublicName(t *testing.T) {
	var sawHost, sawPath string
	reachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHost, sawPath = r.Host, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer reachable.Close()

	client := Backchannel(map[string]string{"http://public.example:8180": reachable.URL})
	resp, err := client.Get("http://public.example:8180/realms/demo/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if sawHost != "public.example:8180" {
		t.Errorf("the provider was asked as %q, want the public name", sawHost)
	}
	if sawPath != "/realms/demo/.well-known/openid-configuration" {
		t.Errorf("path = %q", sawPath)
	}
}

func TestParseRewrites(t *testing.T) {
	got := ParseRewrites("http://localhost:8180/=http://keycloak:8080, bad, https://a=https://b/")
	if got["http://localhost:8180"] != "http://keycloak:8080" || got["https://a"] != "https://b" || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if len(ParseRewrites("")) != 0 {
		t.Fatal("nothing in, nothing out")
	}
}
