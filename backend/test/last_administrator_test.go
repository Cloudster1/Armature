//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/perm"
)

// revokeRounds is how often the two revokes race; one round can miss the window.
const revokeRounds = 10

// lastAdministratorRefusal is what both the service and the database say.
const lastAdministratorRefusal = "last global administrator"

// blockedFor is how long a delete that should be waiting on another is given
// to come back early, which it does only if nothing holds it.
const blockedFor = 200 * time.Millisecond

// twoAdministrators sets up an installation of its own whose only global
// administrators are two people, and returns their assignments and the first one.
func twoAdministrators(t *testing.T, own *harness) (first, second, actor uuid.UUID) {
	t.Helper()
	if _, err := own.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `INSERT INTO installation (slug, name) VALUES ('keepone', 'Keep One')`); err != nil {
			return err
		}
		for i, ids := range []*uuid.UUID{&first, &second} {
			var userID uuid.UUID
			email := []string{"first@keepone.test", "second@keepone.test"}[i]
			if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`, email, email).Scan(&userID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE app_user SET standing = 'member' WHERE id = $1`, userID); err != nil {
				return err
			}
			if i == 0 {
				actor = userID
			}
			if err := tx.QueryRow(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('global_administrator', $1) RETURNING id`, userID).Scan(ids); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("set the installation up: %v", err)
	}
	return first, second, actor
}

func globalAdministrators(t *testing.T, own *harness) int {
	t.Helper()
	var n int
	if err := own.cluster.ReadAdmin(db.PinPrimary(context.Background()), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM role_assignment WHERE role = 'global_administrator'`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// Two administrators revoked at the same time each counted the other as the
// one that stays, and both went. One of the two is refused.
func TestTwoRevokesAtOnceKeepAGlobalAdministrator(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, own := emptyInstallation(t, h, "armature_revokes_at_once_test")
	first, second, actor := twoAdministrators(t, own)
	perms := perm.NewStore(own.cluster)
	ctx := db.PinPrimary(context.Background())

	for round := range revokeRounds {
		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, id := range []uuid.UUID{first, second} {
			wg.Go(func() {
				<-start
				_, errs[i] = perms.Revoke(ctx, id, actor)
			})
		}
		close(start)
		wg.Wait()

		var kept uuid.UUID
		switch {
		case errs[0] == nil && errs[1] != nil:
			kept = second
		case errs[1] == nil && errs[0] != nil:
			kept = first
		default:
			t.Fatalf("round %d: revokes = %v and %v, want exactly one refused", round, errs[0], errs[1])
		}
		for _, err := range errs {
			if err != nil && !strings.Contains(err.Error(), lastAdministratorRefusal) {
				t.Errorf("round %d: refusal = %v, want it to say why", round, err)
			}
		}
		if n := globalAdministrators(t, own); n != 1 {
			t.Fatalf("round %d: %d global administrators remain, want 1", round, n)
		}

		// Give the role back to whoever lost it, for the next round.
		var userID uuid.UUID
		if _, err := own.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			if err := tx.QueryRow(ctx, `SELECT id FROM app_user WHERE standing IS NOT NULL AND id NOT IN (
				SELECT user_id FROM role_assignment WHERE id = $1)`, kept).Scan(&userID); err != nil {
				return err
			}
			var again uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('global_administrator', $1) RETURNING id`, userID).Scan(&again); err != nil {
				return err
			}
			first, second = kept, again
			return nil
		}); err != nil {
			t.Fatalf("round %d: grant the role back: %v", round, err)
		}
	}
}

// The service is not the only way to the table, so the database keeps the last
// global administrator too, even from a superuser.
func TestTheDatabaseKeepsTheLastGlobalAdministrator(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const dbName = "armature_keeps_an_administrator_test"
	_, own := emptyInstallation(t, h, dbName)
	first, second, _ := twoAdministrators(t, own)
	ctx := context.Background()
	connect := func(t *testing.T) *pgx.Conn {
		t.Helper()
		conn, err := pgx.Connect(ctx, withDatabase(t, os.Getenv(envSuperuser), dbName, "", ""))
		if err != nil {
			t.Fatalf("connect superuser to %s: %v", dbName, err)
		}
		t.Cleanup(func() { _ = conn.Close(ctx) })
		return conn
	}
	refused := func(t *testing.T, err error, what string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || !strings.Contains(pgErr.Message, lastAdministratorRefusal) {
			t.Fatalf("%s: error = %v, want the guard's refusal", what, err)
		}
	}
	super := connect(t)

	t.Run("two deletes at once leave one", func(t *testing.T) {
		other := connect(t)
		a, err := super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = a.Rollback(ctx) }()
		if _, err := a.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, first); err != nil {
			t.Fatalf("the first delete, with another administrator left: %v", err)
		}
		// The second waits for the first to commit, then sees nobody else left.
		b, err := other.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = b.Rollback(ctx) }()
		done := make(chan error, 1)
		go func() {
			_, err := b.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, second)
			done <- err
		}()
		select {
		case err := <-done:
			t.Fatalf("the second delete did not wait for the first: %v", err)
		case <-time.After(blockedFor):
		}
		if err := a.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		refused(t, <-done, "the second delete")
	})

	t.Run("the last one cannot be deleted", func(t *testing.T) {
		_, err := super.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, second)
		refused(t, err, "delete")
	})

	t.Run("nor demoted", func(t *testing.T) {
		_, err := super.Exec(ctx, `UPDATE role_assignment SET role = 'user' WHERE id = $1`, second)
		refused(t, err, "demote")
	})

	t.Run("nor taken away with the account", func(t *testing.T) {
		_, err := super.Exec(ctx, `DELETE FROM app_user WHERE id = (SELECT user_id FROM role_assignment WHERE id = $1)`, second)
		refused(t, err, "delete the account")
	})

	if n := globalAdministrators(t, own); n != 1 {
		t.Errorf("%d global administrators remain, want 1", n)
	}
}
