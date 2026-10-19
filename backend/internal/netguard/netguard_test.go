package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestTheServersOwnNetworkIsNotTheInternet(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "::1", "10.1.2.3", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "100.64.0.1", "0.0.0.0", "fd00::1", "224.0.0.1",
	} {
		if !Reserved(netip.MustParseAddr(address)) {
			t.Errorf("%s is inside the server's network, and was treated as the internet", address)
		}
	}
	for _, address := range []string{"93.184.216.34", "8.8.8.8", "2606:2800:220:1:248:1893:25c8:1946"} {
		if Reserved(netip.MustParseAddr(address)) {
			t.Errorf("%s is on the internet, and was refused", address)
		}
	}
}

// A webhook or a git host is an address somebody in the product typed, so the
// one thing it must not reach is whatever else answers on this network.
func TestAClientRefusesTheServersOwnNetwork(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secrets"))
	}))
	defer inside.Close()

	if _, err := Client(2*time.Second, ParseAllow("")).Get(inside.URL); err == nil {
		t.Fatal("a request to the server's own network went through")
	} else if !strings.Contains(err.Error(), ErrBlocked.Error()) {
		t.Fatalf("error = %v, want it to say the address is not reachable", err)
	}

	// The operator may name what the product legitimately reaches, such as a
	// Gitea or an identity provider inside the same network.
	resp, err := Client(2*time.Second, ParseAllow("127.0.0.0/8, ::1/128")).Get(inside.URL)
	if err != nil {
		t.Fatalf("an allowed address was refused: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestAnAllowListReadsHostsAndRanges(t *testing.T) {
	allow := ParseAllow("gitea, 10.0.0.0/8, 192.168.1.5, nonsense/99")
	if !allow.hosts["gitea"] {
		t.Error("a host name was not kept")
	}
	if !allow.permits(netip.MustParseAddr("10.9.9.9")) || !allow.permits(netip.MustParseAddr("192.168.1.5")) {
		t.Error("a named range or address was not kept")
	}
	if allow.permits(netip.MustParseAddr("172.16.0.1")) {
		t.Error("something nobody named was allowed")
	}
	if allow.hosts["nonsense/99"] {
		t.Error("an unreadable entry was kept as a host name")
	}
}

// An address is judged when it is set, so a refusal can name the value that
// lets it through rather than surfacing on the first delivery.
func TestAnAddressIsCheckedBeforeAnythingIsSent(t *testing.T) {
	ctx := t.Context()
	var refused *RefusedError
	if err := Check(ctx, "http://127.0.0.1:8080/in", ParseAllow("")); !errors.As(err, &refused) || refused.Host != "127.0.0.1" {
		t.Errorf("a loopback address: %v, want it refused naming 127.0.0.1", err)
	}
	if err := Check(ctx, "http://LOCALHOST:8080/in", ParseAllow("")); !errors.As(err, &refused) || refused.Host != "localhost" {
		t.Errorf("a name for this machine: %v, want it refused naming localhost", err)
	}
	if err := Check(ctx, "http://localhost:8080/in", ParseAllow("localhost")); err != nil {
		t.Errorf("a name the operator allowed was refused: %v", err)
	}
	if err := Check(ctx, "http://[::1]/in", ParseAllow("::1")); err != nil {
		t.Errorf("an address the operator allowed was refused: %v", err)
	}
	if err := Check(ctx, "https://93.184.216.34/in", ParseAllow("")); err != nil {
		t.Errorf("an address on the internet was refused: %v", err)
	}
	var unresolved *UnresolvedError
	if err := Check(ctx, "https://nowhere.invalid/in", ParseAllow("")); !errors.As(err, &unresolved) || unresolved.Host != "nowhere.invalid" {
		t.Errorf("a name that resolves to nothing: %v", err)
	}
}
