package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/installation"
)

// Service runs the sign-in flow.
//
// Discovery and the key set are fetched from the provider, which is a network
// call, so verifiers are kept per issuer rather than rebuilt per login. The
// library refreshes the keys on its own when it meets a key id it has not seen.
type Service struct {
	db          *db.Cluster
	redirectURL string
	// http reaches the provider; nil is the default client.
	http *http.Client

	mu        sync.Mutex
	verifiers map[string]*coreoidc.Provider
}

func NewService(cluster *db.Cluster, redirectURL string) *Service {
	return &Service{
		db:          cluster,
		redirectURL: redirectURL,
		verifiers:   map[string]*coreoidc.Provider{},
	}
}

// ProviderFor reads the installation's configuration.
func (s *Service) ProviderFor(ctx context.Context) (*Provider, error) {
	var p Provider
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT issuer, client_id, client_secret, groups_claim, scopes,
			       create_groups, admit_by_group, enabled, updated_at
			FROM oidc_provider`,
		).Scan(&p.Issuer, &p.ClientID, &p.ClientSecret, &p.GroupsClaim,
			&p.Scopes, &p.CreateGroups, &p.AdmitByGroup, &p.Enabled, &p.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("read the identity provider: %w", err)
	}
	p.HasSecret = p.ClientSecret != ""
	return &p, nil
}

// Save writes the installation's provider configuration.
//
// An empty secret leaves the stored one alone, so that a settings form which
// never shows the secret can be submitted without erasing it.
func (s *Service) Save(ctx context.Context, in Provider, actor uuid.UUID) (*Provider, db.LSN, error) {
	in.Issuer = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(in.Issuer), "/"))
	in.ClientID = strings.TrimSpace(in.ClientID)
	if in.Issuer == "" || in.ClientID == "" {
		return nil, 0, errors.New("an identity provider needs an issuer and a client id")
	}
	if in.GroupsClaim == "" {
		in.GroupsClaim = "groups"
	}
	if in.Scopes == "" {
		in.Scopes = "openid profile email"
	}

	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_provider
			    (issuer, client_id, client_secret, groups_claim, scopes, create_groups, enabled, admit_by_group)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (one_row) DO UPDATE SET
			    issuer = EXCLUDED.issuer,
			    client_id = EXCLUDED.client_id,
			    client_secret = CASE WHEN EXCLUDED.client_secret = ''
			                         THEN oidc_provider.client_secret
			                         ELSE EXCLUDED.client_secret END,
			    groups_claim = EXCLUDED.groups_claim,
			    scopes = EXCLUDED.scopes,
			    create_groups = EXCLUDED.create_groups,
			    enabled = EXCLUDED.enabled,
			    admit_by_group = EXCLUDED.admit_by_group`,
			in.Issuer, in.ClientID, in.ClientSecret, in.GroupsClaim,
			in.Scopes, in.CreateGroups, in.Enabled, in.AdmitByGroup)
		if err != nil {
			return err
		}
		id, err := installation.ID(ctx, tx)
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: "oidc.saved", TargetType: "org", TargetID: &id, Actor: actor, Data: map[string]any{"issuer": in.Issuer, "enabled": in.Enabled, "admitByGroup": in.AdmitByGroup}})
	})
	if err != nil {
		return nil, 0, fmt.Errorf("save the identity provider: %w", err)
	}

	// A changed issuer means the cached discovery is about somewhere else.
	s.mu.Lock()
	delete(s.verifiers, in.Issuer)
	s.mu.Unlock()

	saved, err := s.ProviderFor(ctx)
	return saved, lsn, err
}

// Disable turns sign-in through the provider off without forgetting how it was
// configured, so turning it back on is one switch rather than a form.
func (s *Service) Disable(ctx context.Context) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE oidc_provider SET enabled = false`)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotConfigured
		}
		return nil
	})
}

// discover returns the provider's endpoints and key set, fetching them once.
func (s *Service) discover(ctx context.Context, issuer string) (*coreoidc.Provider, error) {
	s.mu.Lock()
	cached, ok := s.verifiers[issuer]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}

	found, err := coreoidc.NewProvider(s.providerContext(ctx), issuer)
	if err != nil {
		return nil, fmt.Errorf("ask %s who it is: %w", issuer, err)
	}

	s.mu.Lock()
	s.verifiers[issuer] = found
	s.mu.Unlock()
	return found, nil
}

// identityFrom reads the claims this product cares about out of a token that
// has already been verified.
func identityFrom(token *coreoidc.IDToken, groupsClaim string) (*Identity, error) {
	var claims map[string]any
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("read the token's claims: %w", err)
	}

	identity := Identity{
		Subject: token.Subject,
		Email:   strings.TrimSpace(asString(claims["email"])),
		Name:    strings.TrimSpace(asString(claims["name"])),
	}
	if identity.Email == "" {
		return nil, ErrNoEmail
	}
	// The address is what ties a sign-in to an account here, so a provider that
	// says it never checked the address is not vouching for the person.
	switch verified := claims["email_verified"].(type) {
	case bool:
		if !verified {
			return nil, ErrEmailUnverified
		}
	case string:
		if strings.EqualFold(verified, "false") {
			return nil, ErrEmailUnverified
		}
	}
	if identity.Name == "" {
		// Better a name derived from the address than a blank one in every
		// listing the person appears in.
		identity.Name, _, _ = strings.Cut(identity.Email, "@")
	}
	identity.Groups = asStrings(claims[groupsClaim])
	return &identity, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// asStrings reads a claim that is a list of strings, tolerating a provider that
// sends a single string when somebody is in exactly one group.
func asStrings(v any) []string {
	switch value := v.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []string{value}
	case []any:
		out := make([]string, 0, len(value))
		for _, each := range value {
			if s := asString(each); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return value
	default:
		return nil
	}
}

// randomToken is 32 bytes of randomness, which is what both the state and the
// nonce need to be unguessable.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// VerifyAccessToken checks a person's access token from the installation's
// provider, issued to authorizedParty for Armature: the provider's issuer and
// keys, unexpired, azp the partner's client and aud holding Armature's own.
// The person in it is matched as a sign-in matches them, by verified address.
func (s *Service) VerifyAccessToken(ctx context.Context, raw, authorizedParty string) (*Identity, error) {
	ctx = s.providerContext(ctx)
	provider, err := s.ProviderFor(ctx)
	if err != nil {
		return nil, err
	}
	if !provider.Enabled {
		return nil, ErrNotConfigured
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return nil, err
	}
	verified, err := discovered.Verifier(&coreoidc.Config{SkipClientIDCheck: true}).Verify(ctx, raw)
	if err != nil {
		return nil, errors.New("it is not a current token signed by the identity provider")
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if err := verified.Claims(&claims); err != nil {
		return nil, fmt.Errorf("read the token's claims: %w", err)
	}
	if claims.AuthorizedParty != authorizedParty {
		return nil, fmt.Errorf("it was issued to %q, not to the app's Keycloak client", claims.AuthorizedParty)
	}
	if !slices.Contains(verified.Audience, provider.ClientID) {
		return nil, fmt.Errorf("its audience does not hold Armature's client %s; add an audience mapper to the app's Keycloak client", provider.ClientID)
	}
	return identityFrom(verified, provider.GroupsClaim)
}
