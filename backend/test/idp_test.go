//go:build integration

package test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// idp is a stub identity provider: discovery, a key set, and a token endpoint
// that hands back whatever the test asked it to sign.
//
// A real provider would be a second moving part in every run and would make the
// suite depend on somebody else's uptime. What matters here is what this
// product does with a token, including the tokens it must refuse, and that is
// exactly what a stub can be made to produce on demand.
type idp struct {
	*httptest.Server
	key *rsa.PrivateKey
	// claims is what the next token will say. A test changes it to describe the
	// person signing in, or to forge something the verifier has to reject.
	claims map[string]any
	// signWith, when set, signs the next token with a different key, which is
	// what a token from somebody else's provider looks like.
	signWith *rsa.PrivateKey
	// alg, when set, overrides the algorithm header.
	alg string
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate a signing key: %v", err)
	}

	provider := &idp{key: key, claims: map[string]any{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                provider.URL,
			"authorization_endpoint":                provider.URL + "/authorize",
			"token_endpoint":                        provider.URL + "/token",
			"jwks_uri":                              provider.URL + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})

	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"keys": []any{jwk(&key.PublicKey)}})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		signer := provider.key
		if provider.signWith != nil {
			signer = provider.signWith
		}
		token, err := provider.sign(signer, provider.claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"access_token": "not-used-by-this-product",
			"token_type":   "Bearer",
			"id_token":     token,
		})
	})

	provider.Server = httptest.NewServer(mux)
	t.Cleanup(provider.Close)
	return provider
}

// says sets the claims the next token will carry, filling in the ones every
// token needs so a test only writes the part it cares about.
func (p *idp) says(clientID string, claims map[string]any) {
	full := map[string]any{
		"iss": p.URL,
		"aud": clientID,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"sub": "subject-1",
	}
	for k, v := range claims {
		full[k] = v
	}
	p.claims = full
}

func (p *idp) sign(key *rsa.PrivateKey, claims map[string]any) (string, error) {
	alg := "RS256"
	if p.alg != "" {
		alg = p.alg
	}
	header, err := json.Marshal(map[string]any{"alg": alg, "typ": "JWT", "kid": "test-key"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)

	// An unsigned token is one of the two classic ways a verifier is tricked
	// into accepting anything, so the stub has to be able to produce one.
	if alg == "none" {
		return signingInput + ".", nil
	}

	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func jwk(pub *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": "test-key",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// stateFrom reads the state and nonce out of the redirect the product sends the
// browser to. A real provider echoes the nonce back inside the id token, which
// is what ties that token to this one sign-in, so the stub has to do the same.
func stateFrom(t *testing.T, target string) (state, nonce string) {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("read the redirect: %v", err)
	}
	state = parsed.Query().Get("state")
	nonce = parsed.Query().Get("nonce")
	if state == "" || nonce == "" {
		t.Fatalf("the redirect to the provider carries no state or nonce: %s", target)
	}
	return state, nonce
}
