// Package tenant carries the organization through a request. It sits below db,
// which reads it to set the session variable current_org_id() answers with.
package tenant

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// PostgresVar is what current_org_id() reads, for the inserts and triggers that
// still name the organization; it is set per transaction.
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
