package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// The addresses a development stack runs at, and nothing else may start on.
const (
	DevAppURL          = "http://localhost:5173"
	DevOIDCRedirectURL = "http://localhost:8080/api/v1/auth/oidc/callback"
)

// refuseLoopback names a variable whose address only this machine can reach,
// which outside development sends people's browsers to their own computer.
func refuseLoopback(name, address, wanted string) []string {
	u, err := url.Parse(address)
	if err != nil || !isLoopback(u.Hostname()) {
		return nil
	}
	return []string{fmt.Sprintf("%s is %s, which only this machine can reach; set it to %s", name, address, wanted)}
}

func isLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}
