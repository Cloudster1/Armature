package httpapi

import (
	"errors"
	"fmt"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/oidc"
)

// The sign-in page has a sentence for each of these reasons, shared with Stator
// and Exciter; a reason it does not know reads as the general one.
func TestASignInFailureIsNamedAsTheSuiteNamesIt(t *testing.T) {
	cases := map[string]error{
		"not_a_member":     oidc.ErrNotAMember,
		"no_email":         oidc.ErrNoEmail,
		"unverified_email": oidc.ErrEmailUnverified,
		"expired":          oidc.ErrUnknownLogin,
		"not_configured":   oidc.ErrNotConfigured,
		"unreachable":      fmt.Errorf("%w: ask the issuer who it is: dial tcp: refused", oidc.ErrUnreachable),
		"inactive":         auth.ErrUserInactive,
		"failed":           errors.New("the id token belongs to a different sign-in"),
	}
	for want, err := range cases {
		if got := signInFailure(err); got != want {
			t.Errorf("%v is named %q, want %q", err, got, want)
		}
	}
}
