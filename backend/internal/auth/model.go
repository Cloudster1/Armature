package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// OrgRole is a member's standing within one organization. Project level
// permissions are layered on top of this by the perm package; org_role only
// answers "may this person administer the tenant" and "do they consume a seat".
type OrgRole string

const (
	RoleOwner  OrgRole = "owner"
	RoleAdmin  OrgRole = "admin"
	RoleMember OrgRole = "member"
	// RoleCustomer is a service desk customer: no seat, and no access to
	// anything beyond the portal and their own requests.
	RoleCustomer OrgRole = "customer"
)

// CanAdminister reports whether the role may change organization settings,
// membership and schemes.
func (r OrgRole) CanAdminister() bool { return r == RoleOwner || r == RoleAdmin }

// AppRole is the role somebody starts with when they join at this standing.
//
// Membership answers "do they hold a seat"; what they may do is decided by the
// perm package. Joining has to grant something, or a new member would arrive
// able to see the product and do nothing in it. An empty string is nothing at
// all, which is what a portal customer gets.
func (r OrgRole) AppRole() string {
	switch r {
	case RoleOwner, RoleAdmin:
		return "global_administrator"
	case RoleMember:
		return "user"
	default:
		return ""
	}
}

// IsAgent reports whether the role may use the full product rather than only
// the customer portal.
func (r OrgRole) IsAgent() bool { return r != RoleCustomer && r != "" }

func (r OrgRole) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleCustomer:
		return true
	}
	return false
}

// User is a person, global across organizations.
type User struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Timezone string    `json:"timezone"`
	Locale   string    `json:"locale"`
	IsActive bool      `json:"isActive"`
	// AvatarURL is where the picture is served from; empty means initials.
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// Membership is a user's place in one organization.
type Membership struct {
	OrgID   uuid.UUID `json:"orgId"`
	OrgSlug string    `json:"orgSlug"`
	OrgName string    `json:"orgName"`
	Role    OrgRole   `json:"role"`
}

// Principal is the authenticated caller of a request: who they are, which
// organization they are currently acting in, and what that lets them do.
type Principal struct {
	User User    `json:"user"`
	Org  *Org    `json:"org,omitempty"`
	Role OrgRole `json:"role,omitempty"`

	// PortalDesk is the key of the one desk a session that came through an
	// open door may see; empty for every other session.
	PortalDesk   string     `json:"portalDesk,omitempty"`
	PortalDeskID *uuid.UUID `json:"-"`

	// SessionID is set for browser sessions, TokenID for API tokens. Exactly
	// one of them is non-nil.
	SessionID *uuid.UUID `json:"-"`
	TokenID   *uuid.UUID `json:"-"`
	Scopes    []string   `json:"-"`
	// TokenProjects are the project keys an API token was confined to. Empty
	// means the token reaches wherever its owner does.
	TokenProjects []string `json:"-"`
	// Proof is how a browser session was opened; empty for an API token.
	Proof Proof `json:"-"`
}

// ReachesEverywhere reports whether the caller may act beyond the organization
// the session was opened for. A token is bound to its organization anyway.
func (p *Principal) ReachesEverywhere() bool {
	return p != nil && p.SessionID != nil && p.Proof.ReachesEverywhere()
}

// ScopeRead marks a token that may read everything its owner can and change
// nothing, so a script or an assistant can be handed a key that cannot act.
const ScopeRead = "read"

// HasScope says whether the principal's token carries a scope.
func (p *Principal) HasScope(scope string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ValidateScopes refuses a scope nobody enforces, so a token never carries a
// promise the api does not keep.
func ValidateScopes(scopes []string) error {
	for _, s := range scopes {
		if s != ScopeRead {
			return errors.New("that is not a token scope. Use read or leave it empty")
		}
	}
	return nil
}

// Org is the organization a principal is acting within.
type Org struct {
	ID   uuid.UUID `json:"id"`
	Slug string    `json:"slug"`
	Name string    `json:"name"`
}

// InOrg reports whether the principal has selected an organization.
func (p *Principal) InOrg() bool { return p != nil && p.Org != nil }

// Session is a browser login.
type Session struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	CurrentOrgID *uuid.UUID
	ExpiresAt    time.Time
}

// Invite is a pending invitation to join an organization.
type Invite struct {
	ID        uuid.UUID `json:"id"`
	OrgID     uuid.UUID `json:"orgId"`
	Email     string    `json:"email"`
	Role      OrgRole   `json:"role"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// APIToken is a personal access token. The secret is only ever returned once,
// at creation.
type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	// Projects are the project keys this token is confined to, empty for one
	// that reaches wherever its owner does.
	Projects []string `json:"projects"`
	// Secret is populated only by CreateAPIToken.
	Secret string `json:"secret,omitempty"`
}

var (
	// ErrEmailTaken is returned when signing up with an address that already
	// has an account.
	ErrEmailTaken = errors.New("an account with that email already exists")
	// ErrSlugTaken is returned when an organization slug is already in use.
	ErrSlugTaken = errors.New("that organization address is already taken")
	// ErrNotAMember is returned when a user tries to act in an organization
	// they do not belong to.
	ErrNotAMember = errors.New("not a member of that organization")
	// ErrInviteInvalid covers an unknown, expired or already accepted invite.
	ErrInviteInvalid = errors.New("invitation is invalid or has expired")
	// ErrUserInactive is returned when a deactivated account tries to log in.
	ErrUserInactive = errors.New("account is deactivated")
	// ErrSignInToAccept is returned when an invitation is opened anonymously for
	// an address that already has an account: only that account may take it.
	ErrSignInToAccept = errors.New("an account already uses that address")
	// ErrInviteForSomeoneElse is returned when a signed-in person opens an
	// invitation addressed to somebody else.
	ErrInviteForSomeoneElse = errors.New("that invitation is for somebody else")
	// ErrAlreadyMember is returned when inviting somebody who already works here.
	ErrAlreadyMember = errors.New("that person is already a member")
	// ErrSessionStaysHome is returned when a session proven for one organization
	// tries to act beyond it.
	ErrSessionStaysHome = errors.New("this sign-in only reaches the organization it was made for")
)

// Proof is how a session was opened, which decides how far it reaches.
type Proof string

const (
	ProofPassword   Proof = "password"
	ProofInvite     Proof = "invite"
	ProofPortalCode Proof = "portal_code"
	ProofOpenDoor   Proof = "open_door"
	ProofOIDC       Proof = "oidc"
)

// ReachesEverywhere reports whether a session proven this way may act in every
// organization the person belongs to; the other proofs vouch for one.
func (p Proof) ReachesEverywhere() bool { return p == ProofPassword || p == ProofInvite }
