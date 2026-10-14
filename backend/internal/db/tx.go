package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

// inTx opens a transaction, runs fn and commits. The installation is the one
// organization, so nothing binds the transaction to one (#218).
func (c *Cluster) inTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	opts pgx.TxOptions,
	fn func(context.Context, pgx.Tx) error,
) error {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback is a no-op once the transaction has committed, so this is safe
	// on every path including panics.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// WriteAdmin runs fn on the primary as the admin role, for what runs before
// anybody is signed in: sign-up, a sign-in's lookup, an invitation, the outbox relay.
func (c *Cluster) WriteAdmin(ctx context.Context, fn func(context.Context, DBTX) error) (LSN, error) {
	err := c.inTx(ctx, c.admin, pgx.TxOptions{AccessMode: pgx.ReadWrite}, func(ctx context.Context, tx pgx.Tx) error {
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
	return c.inTx(ctx, c.admin, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, tx)
	})
}
