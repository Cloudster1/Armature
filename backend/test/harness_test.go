//go:build integration

// Package test holds the integration suite. It runs against the real compose
// stack rather than a stub, because the properties under test here are
// properties of Postgres itself: row level security, streaming replication and
// replay lag cannot be faked usefully.
//
// Run with: make test-integration
package test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/tenant"
	"github.com/armature/armature/backend/internal/workflow"
)

// Environment the harness needs. Set by the make target.
const (
	envPrimary   = "ARMATURE_DB_PRIMARY_URL"
	envReplicas  = "ARMATURE_DB_REPLICA_URLS"
	envAdmin     = "ARMATURE_DB_ADMIN_URL"
	envSuperuser = "ARMATURE_TEST_SUPERUSER_URL"
	// Pausing and resuming WAL replay is a superuser operation and has to be
	// issued on the standby itself, not on the primary.
	envReplicaSuper = "ARMATURE_TEST_REPLICA_SUPERUSER_URL"
)

// harness bundles the cluster under test with a superuser connection used only
// to manipulate the environment: pausing replay, forcing lag, cleaning up.
type harness struct {
	cluster *db.Cluster
	super   *pgxpool.Pool
	replica *pgxpool.Pool
	// tel is the telemetry the api under test reports through, and spans is
	// where its traces land instead of a collector.
	tel   *observability.Telemetry
	spans *spanSink
	store attachment.Store
}

// newHarness hands out what TestMain opened for the whole run. Each test gets
// its own copy of the struct, so a test that swaps a part changes only its own.
func newHarness(t *testing.T) *harness {
	t.Helper()

	if os.Getenv(envPrimary) == "" {
		t.Skipf("%s is not set; run the integration suite with `make test-integration`", envPrimary)
	}
	if shared.cluster == nil {
		t.Fatal("the shared harness is not open; TestMain opens it")
	}
	return &harness{
		cluster: shared.cluster, super: shared.super, replica: shared.replica,
		tel: shared.tel, spans: shared.spans, store: shared.store,
	}
}

// makeOrg creates an organization directly, bypassing the API, and returns a
// context scoped to it. Cleanup removes it and everything that cascades.
func (h *harness) makeOrg(t *testing.T, slug string) (uuid.UUID, context.Context) {
	t.Helper()
	ctx := context.Background()

	// Unique per run so repeated test runs do not collide on the slug.
	full := fmt.Sprintf("%s-%s", slug, uuid.New().String()[:8])

	var id uuid.UUID
	err := h.super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, full, slug).Scan(&id)
	if err != nil {
		t.Fatalf("create org %s: %v", full, err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM org WHERE id = $1`, id)
	})

	return id, tenant.WithOrg(ctx, tenant.Org{ID: id, Slug: full})
}

// waitForReplica blocks until the replica has replayed up to lsn, or the
// deadline passes.
func (h *harness) waitForReplica(t *testing.T, lsn db.LSN, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		var text string
		if err := h.replica.QueryRow(context.Background(), `SELECT pg_last_wal_replay_lsn()::text`).Scan(&text); err == nil {
			if got, err := db.ParseLSN(text); err == nil && got >= lsn {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// waitForPrimary waits until the replica has replayed everything the primary
// has written so far, which is what a client that reads from the replica
// needs after somebody else's write about it.
func (h *harness) waitForPrimary(t *testing.T) {
	t.Helper()
	var text string
	if err := h.super.QueryRow(context.Background(), `SELECT pg_current_wal_lsn()::text`).Scan(&text); err != nil {
		t.Fatalf("read the primary's position: %v", err)
	}
	lsn, err := db.ParseLSN(text)
	if err != nil {
		t.Fatalf("parse the primary's position: %v", err)
	}
	if !h.waitForReplica(t, lsn, 10*time.Second) {
		t.Fatalf("the replica did not reach %s in time", text)
	}
}

// pauseReplay stops the standby applying WAL, which is how these tests create
// deterministic replication lag instead of racing against a fast local network.
func (h *harness) pauseReplay(t *testing.T) {
	t.Helper()
	if _, err := h.replica.Exec(context.Background(), `SELECT pg_wal_replay_pause()`); err != nil {
		t.Fatalf("pause replay: %v", err)
	}
	t.Cleanup(func() { h.resumeReplay(t) })
}

func (h *harness) resumeReplay(t *testing.T) {
	t.Helper()
	// Resuming an already running standby raises an error; ignore it so that
	// the cleanup is safe to call twice.
	_, _ = h.replica.Exec(context.Background(), `
		SELECT CASE WHEN pg_get_wal_replay_pause_state() <> 'not paused'
		            THEN pg_wal_replay_resume() END`)
}

// ------------------------------------------------------------ test fixtures ---

// unique returns an identifier that will not collide with other runs, so the
// suite can be run repeatedly against a database it does not own.
func unique(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.New().String()[:8])
}

// email returns a unique address and removes the account it belongs to at the
// end of the test, along with everything that cascades from it.
func (h *harness) email(t *testing.T, prefix string) string {
	t.Helper()
	addr := unique(prefix) + "@armature.test"
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM app_user WHERE email = $1`, addr)
	})
	return addr
}

// orgSlug returns a unique slug and removes the organization afterwards.
func (h *harness) orgSlug(t *testing.T, prefix string) string {
	t.Helper()
	slug := unique(prefix)
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM org WHERE slug = $1`, slug)
	})
	return slug
}

// attachmentStore is the real bucket the stack runs with. Nothing is faked
// below the layer under test: an upload in these tests lands in the stack's object store.
func (h *harness) attachmentStore(t *testing.T) attachment.Store {
	t.Helper()
	return h.store
}

// authService builds the identity service with argon2 parameters cheap enough
// that a suite full of signups still runs in seconds. The production cost is
// covered by the unit tests in internal/auth.
func (h *harness) authService() *auth.Service {
	return h.authServiceWithoutKey().SecondFactor(shared.sealer)
}

// authServiceWithoutKey is the identity service of a server with no
// ARMATURE_SECRET_KEY, which cannot set up a second factor.
func (h *harness) authServiceWithoutKey() *auth.Service {
	return auth.NewService(h.cluster, testPasswordParams(), time.Hour)
}

// testPasswordParams are the cheap argon2 costs every account here is made with.
func testPasswordParams() auth.PasswordParams {
	params := auth.DefaultPasswordParams()
	params.MemoryKiB = 8 * 1024
	params.Iterations = 1
	return params
}

// foundOrganization makes an organization owned by a new account, straight
// through the admin path. No product path makes a second one; tests keep apart
// by them until #217.
func (h *harness) foundOrganization(t *testing.T, name, orgName string) (email string) {
	t.Helper()
	ctx := context.Background()
	email = h.email(t, name)
	slug := h.orgSlug(t, name)
	hash, err := auth.HashPassword(testPassword, testPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var userID, orgID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name, password_hash) VALUES ($1, $2, $3) RETURNING id`,
			email, name, hash).Scan(&userID); err != nil {
			return fmt.Errorf("make the owner: %w", err)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, slug, orgName).Scan(&orgID); err != nil {
			return fmt.Errorf("make the organization: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')`, orgID, userID); err != nil {
			return fmt.Errorf("make the membership: %w", err)
		}
		if _, err := tx.Exec(ctx, `SELECT org_builtin_roles($1)`, orgID); err != nil {
			return fmt.Errorf("give it its roles: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO role_assignment (org_id, role, user_id) VALUES ($1, 'global_administrator', $2)`, orgID, userID); err != nil {
			return fmt.Errorf("grant the owner's role: %w", err)
		}
		if _, err := bootstrap.Org(ctx, tx, orgID); err != nil {
			return fmt.Errorf("prepare the organization: %w", err)
		}
		return audit.Write(ctx, tx, orgID, audit.Entry{Action: "org.created", TargetType: "org", TargetID: &orgID, Actor: userID})
	})
	if err != nil {
		t.Fatalf("found an organization for %s: %v", name, err)
	}
	return email
}

// signup founds an organization for a new owner and signs them in with their
// password, returning the credentials.
func (h *harness) signup(t *testing.T, svc *auth.Service, name string) *auth.Credentials {
	t.Helper()
	email := h.foundOrganization(t, name, name+" Corp")
	creds, err := svc.Login(context.Background(), email, testPassword, "", "")
	if err != nil {
		t.Fatalf("sign %s in: %v", name, err)
	}
	return creds
}

// testPassword is long enough to satisfy the minimum length rule.
const testPassword = "a sufficiently long password"

// services builds the project, issue and board services over the harness's
// cluster, wired exactly as cmd/api wires them.
func (h *harness) services() (*project.Service, *issue.Service, *board.Service, *workflow.Engine, *workflow.Store) {
	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	store := workflow.NewStore()
	issues := issue.NewService(h.cluster, engine, store)
	return project.NewService(h.cluster, board.Provisioner{}, report.Provisioner{}), issues, board.NewService(h.cluster, issues), engine, store
}

// sprintService builds the sprint service and the plan it counts with, wired
// exactly as cmd/api wires them.
func (h *harness) sprintService(issues *issue.Service) (*sprint.Service, *plan.Service) {
	sprints := sprint.NewService(h.cluster)
	plans := plan.NewService(issues, sprints)
	sprints.CountWith(plans)
	return sprints, plans
}

// workflowAdmin builds the configuration writer over the harness's cluster.
func (h *harness) workflowAdmin() *workflow.Admin {
	return workflow.NewAdmin(h.cluster, workflow.NewStore()).WithObserver(board.Follower{})
}

// workspace is an organization with an owner and a project, which is the
// starting point for almost every issue test.
type workspace struct {
	ctx      context.Context
	orgID    uuid.UUID
	owner    *auth.Credentials
	actor    issue.Actor
	project  *project.Project
	projects *project.Service
	issues   *issue.Service
	boards   *board.Service
	engine   *workflow.Engine
	store    *workflow.Store
	admin    *workflow.Admin
	sprints  *sprint.Service
	plans    *plan.Service
	teams    *team.Service
}

// newWorkspace founds an organization for a fresh owner, with its statuses,
// issue types and default workflow, then creates a project in it.
func (h *harness) newWorkspace(t *testing.T, name string) *workspace {
	t.Helper()

	svc := h.authService()
	creds := h.signup(t, svc, name)

	// Pinned to the primary on purpose. These tests write and immediately read
	// back, which in production is handled by the middleware pinning the
	// caller to their own write position. Reproducing that plumbing here would
	// test the routing layer, which has its own tests, and would otherwise make
	// every assertion a race against replication.
	ctx := db.PinPrimary(auth.ContextForOrg(context.Background(), creds.Principal))

	projects, issues, boards, engine, store := h.services()
	sprints, plans := h.sprintService(issues)
	teams := team.NewService(h.cluster)
	plans.WithTeams(teams)
	plans.WithAvailability(availability.NewService(h.cluster))
	actor := issue.Actor{UserID: creds.Principal.User.ID, OrgRole: creds.Principal.Role, Perms: perm.NewSet(nil).WithEverything()}

	// The key must be unique per run and not merely derived from the name:
	// two workspaces whose names share a prefix would otherwise get the same
	// key, and a test that reads "the other tenant's project" would quietly be
	// reading its own.
	key := strings.ToUpper(name[:min(len(name), 3)] + uuid.New().String()[:3])

	p, _, err := projects.Create(ctx, project.CreateInput{
		Name: name + " project",
		Key:  key,
	}, actor.UserID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return &workspace{
		ctx: ctx, orgID: creds.Principal.Org.ID, owner: creds, actor: actor,
		project: p, projects: projects, issues: issues, boards: boards,
		engine: engine, store: store, admin: h.workflowAdmin(),
		sprints: sprints, plans: plans, teams: teams,
	}
}

// issueTypeID looks up one of the bootstrapped issue types by name.
func (h *harness) issueTypeID(t *testing.T, ws *workspace, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM issue_type WHERE name = $1`, name).Scan(&id)
	})
	if err != nil {
		t.Fatalf("look up issue type %q: %v", name, err)
	}
	return id
}

// transitionID finds an available transition by name, failing the test when it
// is not offered.
func (h *harness) transitionID(t *testing.T, ws *workspace, key, name string) uuid.UUID {
	t.Helper()
	transitions, err := ws.issues.Transitions(ws.ctx, key, ws.actor)
	if err != nil {
		t.Fatalf("list transitions for %s: %v", key, err)
	}
	for _, tr := range transitions {
		if tr.Name == name {
			return tr.ID
		}
	}
	var available []string
	for _, tr := range transitions {
		available = append(available, tr.Name)
	}
	t.Fatalf("transition %q is not available on %s; available: %v", name, key, available)
	return uuid.Nil
}

// joinExisting adds somebody to an organization by email, creating the account
// if it does not exist, which is the state an invited person is in before they
// have ever signed in.
func (h *harness) joinExisting(t *testing.T, ws *workspace, email, role string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO app_user (email, name) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET name = app_user.name
		RETURNING id`, email, "Invited Person").Scan(&id); err != nil {
		t.Fatalf("create the account: %v", err)
	}
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET org_role = EXCLUDED.org_role`,
		ws.orgID, id, role); err != nil {
		t.Fatalf("add them to the organization: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM app_user WHERE id = $1`, id)
	})
	return id
}
