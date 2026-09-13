// Package tenant carries the current organization (tenant) through a request.
//
// It deliberately has no database dependency: the db package reads from here to
// set the Postgres session variable that row level security policies key on, so
// tenant must sit below db in the import graph.
package tenant

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// PostgresVar is the session variable that every row level security policy
// compares org_id against. It is set with SET LOCAL inside each transaction, so
// it is scoped to that transaction and cannot leak across pooled connections.
const PostgresVar = "app.org_id"

// ErrNoTenant is returned when a tenant scoped operation is attempted without an
// organization in context.
var ErrNoTenant = errors.New("no organization in context")

type ctxKey struct{}

// Org identifies the organization a request is acting within.
type Org struct {
	ID   uuid.UUID
	Slug string
}

// WithOrg returns a context bound to org.
func WithOrg(ctx context.Context, org Org) context.Context {
	return context.WithValue(ctx, ctxKey{}, org)
}

// FromContext returns the organization bound to ctx, if any.
func FromContext(ctx context.Context) (Org, bool) {
	org, ok := ctx.Value(ctxKey{}).(Org)
	return org, ok
}

// MustFromContext returns the organization bound to ctx or an error.
func MustFromContext(ctx context.Context) (Org, error) {
	org, ok := FromContext(ctx)
	if !ok || org.ID == uuid.Nil {
		return Org{}, ErrNoTenant
	}
	return org, nil
}
