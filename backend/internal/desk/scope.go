package desk

import (
	"context"

	"github.com/google/uuid"
)

// Scope is the one desk a session that came through an open door may see. It
// travels on the context like the tenant, since every portal read has to ask.
type Scope struct {
	ProjectID  uuid.UUID
	ProjectKey string
}

type scopeKey struct{}

// WithScope narrows the portal on ctx to one desk.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeFrom returns the desk the portal is narrowed to, if it is.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok
}

// allows says whether a desk is within the scope on ctx; everything is when
// there is none.
func allows(ctx context.Context, projectID uuid.UUID) bool {
	scope, ok := ScopeFrom(ctx)
	return !ok || scope.ProjectID == projectID
}

// allowsKey is allows by project key.
func allowsKey(ctx context.Context, projectKey string) bool {
	scope, ok := ScopeFrom(ctx)
	return !ok || scope.ProjectKey == projectKey
}
