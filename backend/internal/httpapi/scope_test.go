package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
)

func TestAReadTokenIsRefusedOnlyAWrite(t *testing.T) {
	id := uuid.New()
	readToken := &auth.Principal{TokenID: &id, Scopes: []string{auth.ScopeRead}}
	fullToken := &auth.Principal{TokenID: &id}
	session := &auth.Principal{SessionID: &id, Scopes: []string{auth.ScopeRead}}

	cases := []struct {
		name    string
		method  string
		p       *auth.Principal
		refused bool
	}{
		{"read token reads", http.MethodGet, readToken, false},
		{"read token heads", http.MethodHead, readToken, false},
		{"read token preflights", http.MethodOptions, readToken, false},
		{"read token posts", http.MethodPost, readToken, true},
		{"read token patches", http.MethodPatch, readToken, true},
		{"read token deletes", http.MethodDelete, readToken, true},
		{"full token posts", http.MethodPost, fullToken, false},
		{"a session is never a token", http.MethodPost, session, false},
		{"nobody posts", http.MethodPost, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := writeRefused(c.method, c.p); got != c.refused {
				t.Fatalf("writeRefused(%s) = %v, want %v", c.method, got, c.refused)
			}
		})
	}
}

func TestOnlyReadIsAScope(t *testing.T) {
	if err := auth.ValidateScopes(nil); err != nil {
		t.Fatal(err)
	}
	if err := auth.ValidateScopes([]string{auth.ScopeRead}); err != nil {
		t.Fatal(err)
	}
	if err := auth.ValidateScopes([]string{"write"}); err == nil {
		t.Fatal("write should not be a scope")
	}
}
