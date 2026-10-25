//go:build integration

// Package test holds the integration suite, run against the real compose stack:
// triggers, streaming replication and replay lag cannot be faked usefully.
package test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
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
	// made is what the test made in the installation, swept after everything
	// else of the test, so nothing of its own still holds on to it.
	made *madeThings
}

// madeThings collects what the test and its subtests alike made.
type madeThings struct {
	mu sync.Mutex
	// keys and people are swept again in case their own cleanup was refused
	// while something of the test still held them.
	keys, emails, userIDs []string
	names                 []string
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
	h := &harness{
		cluster: shared.cluster, super: shared.super, replica: shared.replica,
		tel: shared.tel, spans: shared.spans, store: shared.store,
		made: &madeThings{},
	}
	t.Cleanup(h.sweep)
	return h
}

// sweep removes what is left of the test once its own cleanups have run: its
// projects, its people and what was sent to them, then what it named.
func (h *harness) sweep() {
	h.made.mu.Lock()
	defer h.made.mu.Unlock()
	ctx := context.Background()
	if len(h.made.keys) > 0 {
		_, _ = h.super.Exec(ctx, `DELETE FROM project WHERE key = ANY($1)`, h.made.keys)
	}
	if len(h.made.emails)+len(h.made.userIDs) > 0 {
		// A role somebody granted themselves is updated as its granter goes,
		// and that update is refused once they are no member; so roles go first.
		const people = `(SELECT id FROM app_user WHERE email = ANY($1) OR id::text = ANY($2))`
		_, _ = h.super.Exec(ctx, `DELETE FROM role_assignment WHERE user_id IN `+people, h.made.emails, h.made.userIDs)
		_, _ = h.super.Exec(ctx, `DELETE FROM app_user WHERE id IN `+people, h.made.emails, h.made.userIDs)
	}
	if len(h.made.emails) > 0 {
		// Invitations and portal codes are kept by address, not by account.
		_, _ = h.super.Exec(ctx, `DELETE FROM invite WHERE email = ANY($1)`, h.made.emails)
		_, _ = h.super.Exec(ctx, `DELETE FROM portal_code WHERE email = ANY($1)`, h.made.emails)
	}
	// Table by table in sharedLists' order, so a scheme goes before the
	// workflows it maps and a workflow before the statuses it holds.
	for _, table := range sharedLists {
		for _, name := range h.made.names {
			_, _ = h.super.Exec(ctx, `DELETE FROM `+table+` WHERE lower(name) = lower($1)`, name)
		}
	}
}

// installation is the id of the one organization every test works in.
func (h *harness) installation() uuid.UUID {
	return shared.installation.ID
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
	h.made.mu.Lock()
	h.made.emails = append(h.made.emails, addr)
	h.made.mu.Unlock()
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM app_user WHERE email = $1`, addr)
	})
	return addr
}

// projectKey returns a project key no other test's project has, and removes
// that project and everything in it at the end of the test.
func (h *harness) projectKey(t *testing.T, prefix string) string {
	t.Helper()
	key := uniqueKey(prefix)
	h.made.mu.Lock()
	h.made.keys = append(h.made.keys, key)
	h.made.mu.Unlock()
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM project WHERE key = $1`, key)
	})
	return key
}

// uniqueKey is up to four letters of prefix and six random ones: a project key
// is at most ten, and every test's projects share one installation.
func uniqueKey(prefix string) string {
	letters := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r
		}
		return -1
	}, strings.ToUpper(prefix))
	if letters == "" {
		letters = "P"
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	random := uuid.New()
	tail := make([]byte, 6)
	for i := range tail {
		tail[i] = alphabet[int(random[i])%len(alphabet)]
	}
	return letters[:min(len(letters), 4)] + string(tail)
}

// sharedLists are the tables that hold one list for everyone, by name: what a
// test names in one must not be another test's, and goes when the test ends.
var sharedLists = []string{
	"automation_rule", "oauth_client", "webhook_endpoint", "dashboard_template", "workflow_scheme", "workflow",
	"issue_link_type", "custom_field", "label", "user_group", "role", "holiday_calendar",
	"issue_type", "issue_status",
}

// name returns a name no other test uses for anything on a shared list, and
// removes what the test named with it from those lists once its projects are gone.
func (h *harness) name(t *testing.T, prefix string) string {
	t.Helper()
	name := unique(prefix)
	h.made.mu.Lock()
	h.made.names = append(h.made.names, name)
	h.made.mu.Unlock()
	return name
}

// erasable removes the account by its id when the test ends, for one the test
// erases: erasing renames the address the account would be found by.
func (h *harness) erasable(userID string) {
	h.made.mu.Lock()
	h.made.userIDs = append(h.made.userIDs, userID)
	h.made.mu.Unlock()
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

// person makes an account in the installation at the standing given, with
// the role that standing joins with, removed at the end of the test.
func (h *harness) person(t *testing.T, name string, role auth.OrgRole) (email string) {
	t.Helper()
	email = h.email(t, name)
	_, err := h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var userID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name, password_hash) VALUES ($1, $2, $3) RETURNING id`,
			email, name, shared.passwordHash).Scan(&userID); err != nil {
			return fmt.Errorf("make the account: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE app_user SET standing = $2 WHERE id = $1`, userID, role); err != nil {
			return fmt.Errorf("give the standing: %w", err)
		}
		if role.AppRole() == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ($1, $2)`, role.AppRole(), userID)
		return err
	})
	if err != nil {
		t.Fatalf("make %s: %v", name, err)
	}
	return email
}

// outsider is a member who holds no role anywhere yet: somebody a project is
// a wall to until they are granted one in it.
func (h *harness) outsider(t *testing.T, name string) (email string) {
	t.Helper()
	email = h.person(t, name, auth.RoleMember)
	if _, err := h.super.Exec(context.Background(), `
		DELETE FROM role_assignment WHERE user_id = (SELECT id FROM app_user WHERE email = $1)`, email); err != nil {
		t.Fatalf("take %s's role away: %v", name, err)
	}
	return email
}

// keepProvider puts the installation's identity provider back as it was when
// the test ends, or takes the test's away when there was none.
func (h *harness) keepProvider(t *testing.T) {
	t.Helper()
	var kept *string
	if err := h.super.QueryRow(context.Background(), `
		SELECT (SELECT row_to_json(p)::text FROM oidc_provider p)`).Scan(&kept); err != nil {
		t.Fatalf("read the installation's provider: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := h.super.Exec(ctx, `DELETE FROM oidc_provider`); err != nil {
			t.Errorf("take the test's provider away: %v", err)
		}
		if kept == nil {
			return
		}
		if _, err := h.super.Exec(ctx, `
			INSERT INTO oidc_provider SELECT * FROM json_populate_record(NULL::oidc_provider, $1::json)`, *kept); err != nil {
			t.Errorf("put the installation's provider back: %v", err)
		}
	})
}

// keepSecondFactorRule puts the installation's rule back as it found it when
// the test ends: every other test, and the browser suite, signs in under it.
func (h *harness) keepSecondFactorRule(t *testing.T) {
	t.Helper()
	orgID := h.installation()
	var rule string
	if err := h.super.QueryRow(context.Background(), `SELECT second_factor::text FROM installation WHERE id = $1`, orgID).Scan(&rule); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := h.super.Exec(context.Background(), `UPDATE installation SET second_factor = $2 WHERE id = $1`, orgID, rule); err != nil {
			t.Errorf("put the second-factor rule back: %v", err)
		}
	})
}

// signIn signs somebody in with their password.
func (h *harness) signIn(t *testing.T, svc *auth.Service, email string) *auth.Credentials {
	t.Helper()
	creds, err := svc.Login(context.Background(), email, testPassword, "", "")
	if err != nil {
		t.Fatalf("sign %s in: %v", email, err)
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

// workspace is an owner and a project of theirs, which is the starting point
// for almost every issue test.
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

// newProject makes an owner of the installation and a project of theirs under
// a key no other test has, both removed at the end of the test.
func (h *harness) newProject(t *testing.T, name string) *workspace {
	t.Helper()

	svc := h.authService()
	creds := h.signIn(t, svc, h.person(t, name, auth.RoleOwner))

	// Pinned to the primary on purpose. These tests write and immediately read
	// back, which in production is handled by the middleware pinning the
	// caller to their own write position. Reproducing that plumbing here would
	// test the routing layer, which has its own tests, and would otherwise make
	// every assertion a race against replication.
	ctx := db.PinPrimary(context.Background())

	projects, issues, boards, engine, store := h.services()
	sprints, plans := h.sprintService(issues)
	teams := team.NewService(h.cluster)
	plans.WithTeams(teams)
	plans.WithAvailability(availability.NewService(h.cluster))
	actor := issue.Actor{UserID: creds.Principal.User.ID, OrgRole: creds.Principal.Role, Perms: perm.NewSet(nil).WithEverything()}

	p, _, err := projects.Create(ctx, project.CreateInput{
		Name: name + " project",
		Key:  h.projectKey(t, name),
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

// nameOf is the part of an address before the at sign, which for the suite's
// addresses is unique and so names one person whoever reads it.
func nameOf(email string) string {
	name, _, _ := strings.Cut(email, "@")
	return name
}

// joinExisting adds somebody to the installation by email, creating the account
// if it does not exist, named after the address so no other test's is named alike.
func (h *harness) joinExisting(t *testing.T, email, role string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO app_user (email, name) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET name = app_user.name
		RETURNING id`, email, nameOf(email)).Scan(&id); err != nil {
		t.Fatalf("create the account: %v", err)
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET standing = $2 WHERE id = $1`, id, role); err != nil {
		t.Fatalf("add them to the installation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM app_user WHERE id = $1`, id)
	})
	return id
}

// joinAsMember is joinExisting with the role a member joins with, for somebody
// who must read the projects to be told about their issues.
func (h *harness) joinAsMember(t *testing.T, email string) uuid.UUID {
	t.Helper()
	id := h.joinExisting(t, email, string(auth.RoleMember))
	if _, err := h.super.Exec(context.Background(), `INSERT INTO role_assignment (role, user_id) VALUES ($1, $2)`, auth.RoleMember.AppRole(), id); err != nil {
		t.Fatalf("give %s the member's role: %v", email, err)
	}
	return id
}
