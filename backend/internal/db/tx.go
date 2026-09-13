package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/armature/armature/backend/internal/tenant"
)

// setOrgSQL binds the row level security session variable for the current
// transaction. SET LOCAL cannot take a bind parameter, but set_config can, and
// its third argument makes the setting transaction local, so it is discarded on
// commit or rollback and can never leak to the next user of a pooled connection.
const setOrgSQL = `SELECT set_config($1, $2, true)`

// currentLSNSQL reads the primary's current write ahead log insert position.
const currentLSNSQL = `SELECT pg_current_wal_insert_lsn()::text`

// Write runs fn inside a read-write transaction on the primary and returns the
// write ahead log position that reflects it. Pin a user's later reads to that
// LSN (see PinLSN) and they will never be served a replica that has not yet
// replayed their own change.
func (c *Cluster) Write(ctx context.Context, fn func(context.Context, DBTX) error) (LSN, error) {
	err := c.inTx(ctx, c.primary, pgx.TxOptions{AccessMode: pgx.ReadWrite}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
	if err != nil {
		return 0, err
	}
	return c.committedLSN(ctx, c.primary)
}

// committedLSN is read after the commit, never inside the transaction: the
// commit record lands past any position read before it, with other sessions'
// records in between under load, and a replica at the earlier position has not
// replayed the change yet.
func (c *Cluster) committedLSN(ctx context.Context, pool *pgxpool.Pool) (LSN, error) {
	var text string
	if err := pool.QueryRow(ctx, currentLSNSQL).Scan(&text); err != nil {
		return 0, fmt.Errorf("read wal position: %w", err)
	}
	return ParseLSN(text)
}

// Read runs fn inside a read-only transaction on whichever pool is appropriate
// for ctx: a healthy, sufficiently caught-up replica when one exists, otherwise
// the primary.
func (c *Cluster) Read(ctx context.Context, fn func(context.Context, DBTX) error) error {
	pool := c.reader(ctx)
	return c.inTx(ctx, pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
}

// ReadPrimary is Read pinned to the primary.
func (c *Cluster) ReadPrimary(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return c.Read(PinPrimary(ctx), fn)
}

// inTx opens a transaction, applies the tenant scope, runs fn and commits.
//
// The tenant scope is not optional: without an organization in context, and
// without the context being explicitly marked as system level, the transaction
// is refused. A query that forgets its tenant filter is then a failed request
// rather than a cross-tenant disclosure.
func (c *Cluster) inTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	opts pgx.TxOptions,
	fn func(context.Context, pgx.Tx) error,
) error {
	org, hasOrg := tenant.FromContext(ctx)
	if !hasOrg && !isSystem(ctx) {
		return tenant.ErrNoTenant
	}

	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback is a no-op once the transaction has committed, so this is safe
	// on every path including panics.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if hasOrg {
		if _, err := tx.Exec(ctx, setOrgSQL, tenant.PostgresVar, org.ID.String()); err != nil {
			return fmt.Errorf("apply tenant scope: %w", err)
		}
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// WriteAdmin runs fn on the primary as the row-level-security exempt role.
//
// It exists for the handful of operations that cannot be tenant scoped because
// they run before a tenant is known: creating an organization, looking a user
// up by email at login, accepting an invite, and relaying the outbox. Every
// other write must go through Write so that its tenant scope is enforced by the
// database rather than by the caller remembering to add a filter.
func (c *Cluster) WriteAdmin(ctx context.Context, fn func(context.Context, DBTX) error) (LSN, error) {
	err := c.inTx(WithSystem(ctx), c.admin, pgx.TxOptions{AccessMode: pgx.ReadWrite}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
	if err != nil {
		return 0, err
	}
	return c.committedLSN(ctx, c.admin)
}

// ReadAdmin is the read-only counterpart of WriteAdmin. It reads from the
// primary: the operations that need it are authentication paths where a stale
// answer is worse than a slightly more expensive one.
func (c *Cluster) ReadAdmin(ctx context.Context, fn func(context.Context, DBTX) error) error {
	return c.inTx(WithSystem(ctx), c.admin, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
}
