package oidc

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/armature/armature/backend/internal/netguard"
)

// Backchannel is an HTTP client for talking to a provider whose public
// address is not the one this process can reach it at.
//
// In a compose stack the browser reaches Keycloak at localhost:8180 and the
// issuer in its tokens says so, but from inside the api container that
// address is the container itself. The rewrite sends the connection to the
// reachable address and keeps the Host header as the public one, so the
// provider still describes itself under the issuer the tokens name. It is a
// development convenience; a deployment gives the provider one address.
func Backchannel(rewrites map[string]string) *http.Client {
	if len(rewrites) == 0 {
		return netguard.Client(discoveryTimeout, netguard.FromEnv())
	}
	// A rewrite is the operator saying where this provider really is, which is
	// the same permission the allow list gives.
	return &http.Client{Timeout: discoveryTimeout, Transport: &rewriting{rewrites: rewrites, next: http.DefaultTransport}}
}

// discoveryTimeout bounds asking a provider who it is; the sign-in waits on it.
const discoveryTimeout = 10 * time.Second

type rewriting struct {
	rewrites map[string]string
	next     http.RoundTripper
}

func (r *rewriting) RoundTrip(req *http.Request) (*http.Response, error) {
	public := req.URL.Scheme + "://" + req.URL.Host
	target, ok := r.rewrites[public]
	if !ok {
		return r.next.RoundTrip(req)
	}
	reach, err := url.Parse(target)
	if err != nil || reach.Host == "" {
		return r.next.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme = reach.Scheme
	clone.URL.Host = reach.Host
	// The provider answers for the name it was asked for.
	clone.Host = req.URL.Host
	return r.next.RoundTrip(clone)
}

// ParseRewrites reads "public=reachable,public=reachable" from configuration.
func ParseRewrites(spec string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(spec, ",") {
		from, to, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		from, to = strings.TrimSuffix(strings.TrimSpace(from), "/"), strings.TrimSuffix(strings.TrimSpace(to), "/")
		if from != "" && to != "" {
			out[from] = to
		}
	}
	return out
}

// WithHTTPClient sets the client the provider is reached with.
func (s *Service) WithHTTPClient(c *http.Client) *Service {
	s.http = c
	return s
}

// providerContext puts the client on the context for both libraries.
func (s *Service) providerContext(ctx context.Context) context.Context {
	if s.http == nil {
		return ctx
	}
	ctx = coreoidc.ClientContext(ctx, s.http)
	return context.WithValue(ctx, oauth2.HTTPClient, s.http)
}
