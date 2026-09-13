package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Every brake on the doors is keyed by this address, so a caller who could
// name their own would never be braked.
func TestTheCallersAddressIsBelievedOnlyFromATrustedProxy(t *testing.T) {
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name, remote, forwarded string
		trusted                 []netip.Prefix
		want                    string
	}{
		{"with no proxy named the header is ignored", "203.0.113.9:5000", "198.51.100.1", nil, "203.0.113.9"},
		{"an untrusted peer cannot name an address", "203.0.113.9:5000", "198.51.100.1", proxy, "203.0.113.9"},
		{"a trusted proxy names the client", "10.1.2.3:5000", "198.51.100.1", proxy, "198.51.100.1"},
		{"a spoofed first hop loses to the proxy's own", "10.1.2.3:5000", "6.6.6.6, 198.51.100.1", proxy, "198.51.100.1"},
		{"trusted hops are walked past", "10.1.2.3:5000", "198.51.100.1, 10.9.9.9", proxy, "198.51.100.1"},
		{"a hop that is not an address ends the walk", "10.1.2.3:5000", "not-an-address", proxy, "10.1.2.3"},
		{"a trusted proxy that says nothing is the caller", "10.1.2.3:5000", "", proxy, "10.1.2.3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{TrustedProxies: c.trusted}
			var got string
			handler := s.clientAddress(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = clientIP(r) }))
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.remote
			if c.forwarded != "" {
				r.Header.Set("X-Forwarded-For", c.forwarded)
			}
			handler.ServeHTTP(httptest.NewRecorder(), r)
			if got != c.want {
				t.Fatalf("client = %q, want %q", got, c.want)
			}
		})
	}
}
