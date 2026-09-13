// Command seed loads a demo organization so that a fresh `docker compose up`
// has something to sign in to.
//
// It is idempotent: running it twice leaves the same accounts, rather than
// failing on the second run or quietly creating duplicates.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/workflow"
)

// demoPassword is fine to hard code: this command exists only to populate a
// throwaway development database, and it refuses to run against production.
const demoPassword = "demo password please"

type demoUser struct {
	email string
	name  string
	role  auth.OrgRole
}

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProduction() {
		return errors.New("refusing to seed a production environment")
	}

	log := observability.NewLogger(cfg.LogLevel, false)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()

	// Cheap argon2 parameters: this is demo data, and the default cost would
	// make seeding several accounts needlessly slow.
	params := auth.DefaultPasswordParams()
	params.MemoryKiB = 16 * 1024
	params.Iterations = 1
	svc := auth.NewService(cluster, params, cfg.Auth.SessionTTL)

	owner := demoUser{email: "ada@armature.test", name: "Ada Lovelace", role: auth.RoleOwner}

	creds, err := svc.Signup(ctx, auth.SignupInput{
		Email:    owner.email,
		Password: demoPassword,
		Name:     owner.name,
		OrgName:  "Demo Company",
		OrgSlug:  "demo",
	})
	switch {
	case errors.Is(err, auth.ErrEmailTaken), errors.Is(err, auth.ErrSlugTaken):
		// The organization survives from an earlier run. Sign in instead and
		// top up whatever is missing, so that seeding after an upgrade adds the
		// newer demo data rather than silently doing nothing.
		log.Info("demo organization already exists, topping it up")
		creds, err = svc.Login(ctx, owner.email, demoPassword, "seed", "")
		if err != nil {
			return fmt.Errorf("sign in to the existing demo organization: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create demo organization: %w", err)
	}

	teammates := []demoUser{
		// Grace and Alan hold no organization role of their own: what they may
		// do comes from the Keycloak groups they are in, which is the point of
		// the demo's single sign-on.
		{email: "grace@armature.test", name: "Grace Hopper", role: auth.RoleMember},
		{email: "alan@armature.test", name: "Alan Turing", role: auth.RoleMember},
		{email: "rita@armature.test", name: "Rita Reader", role: auth.RoleMember},
		{email: "customer@armature.test", name: "Sam Customer", role: auth.RoleCustomer},
	}
	if err := seedMembers(ctx, svc, creds, teammates, log); err != nil {
		return err
	}

	// The demo has a file on an issue when there is a bucket to put it in, and
	// says so rather than failing when there is not.
	store, err := attachment.FromConfig(attachment.S3Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
	})
	if err != nil {
		return err
	}
	if s3, ok := store.(*attachment.S3Store); ok {
		if err := s3.EnsureBucket(ctx); err != nil {
			return fmt.Errorf("attachment bucket: %w", err)
		}
	}

	if err := seedWork(ctx, cluster, creds, store, log); err != nil {
		return err
	}
	if err := seedSingleSignOn(ctx, cluster, creds, log); err != nil {
		return err
	}

	printCredentials(owner.email)
	return nil
}

// keycloakIssuer is where the browser finds the demo's identity provider;
// the api reaches it through its backchannel rewrite.
const (
	keycloakIssuer       = "http://localhost:8180/realms/demo"
	keycloakClientID     = "armature"
	keycloakClientSecret = "armature-demo-secret"
)

// seedSingleSignOn points the demo organization at the Keycloak realm the
// stack runs, and makes the groups the realm's users are in grant roles here:
// the provider owns who is in a group, the tracker decides what a group may
// do. It is safe to run again; what exists is left as it is.
func seedSingleSignOn(ctx context.Context, cluster *db.Cluster, creds *auth.Credentials, log *slog.Logger) error {
	orgCtx := db.PinPrimary(auth.ContextForOrg(ctx, creds.Principal))
	issuer := keycloakIssuer
	if fromEnv := os.Getenv("ARMATURE_KEYCLOAK_ISSUER"); fromEnv != "" {
		issuer = fromEnv
	}
	sso := oidc.NewService(cluster, "")
	if _, _, err := sso.Save(orgCtx, creds.Principal.Org.ID, oidc.Provider{
		Issuer: issuer, ClientID: keycloakClientID, ClientSecret: keycloakClientSecret,
		GroupsClaim: "groups", Scopes: "openid profile email", CreateGroups: true, Enabled: true,
	}, creds.Principal.User.ID); err != nil {
		return fmt.Errorf("configure single sign-on: %w", err)
	}

	perms := perm.NewStore(cluster)
	existing, err := perms.Groups(orgCtx)
	if err != nil {
		return err
	}
	byRef := map[string]uuid.UUID{}
	for _, g := range existing {
		if g.ExternalRef != "" {
			byRef[g.ExternalRef] = g.ID
		}
	}
	mapping := []struct {
		group, description string
		role               perm.Role
		projectKey         string
	}{
		{"armature-administrators", "Runs the tracker itself.", perm.GlobalAdministrator, ""},
		{"portal-owners", "Owns the Customer Portal project.", perm.ProjectAdministrator, "CP"},
		{"scrum-masters", "Runs the portal's sprints.", perm.ScrumMaster, "CP"},
		{"developers", "Works the issues.", perm.User, ""},
		{"readers", "Looks, and changes nothing.", perm.Reader, ""},
	}
	made := 0
	for _, m := range mapping {
		if _, exists := byRef[m.group]; exists {
			continue
		}
		group, _, err := perms.CreateGroup(orgCtx, perm.GroupInput{Name: m.group, Description: m.description, ExternalRef: m.group}, creds.Principal.User.ID)
		if err != nil {
			return fmt.Errorf("group %s: %w", m.group, err)
		}
		if _, _, err := perms.Grant(orgCtx, perm.GrantInput{Role: m.role, ProjectKey: m.projectKey, GroupID: &group.ID}, creds.Principal.User.ID); err != nil {
			return fmt.Errorf("grant %s to %s: %w", m.role, m.group, err)
		}
		made++
	}

	// Joining by invitation grants the ordinary member role directly. For the
	// people who sign in through Keycloak that would hide what their groups
	// grant, so their direct grants go; the owner keeps hers.
	people, err := membersByEmail(orgCtx, cluster)
	if err != nil {
		return err
	}
	fromGroups := map[uuid.UUID]bool{}
	for _, email := range []string{"grace@armature.test", "alan@armature.test", "rita@armature.test"} {
		if id, ok := people[email]; ok {
			fromGroups[id] = true
		}
	}
	assignments, err := perms.Assignments(orgCtx, "")
	if err != nil {
		return err
	}
	revoked := 0
	for _, a := range assignments {
		if a.UserID != nil && fromGroups[*a.UserID] {
			if _, err := perms.Revoke(orgCtx, a.ID, creds.Principal.User.ID); err != nil {
				return fmt.Errorf("revoke the direct grant of %s to %s: %w", a.Role, a.UserName, err)
			}
			revoked++
		}
	}
	log.Info("single sign-on configured", "issuer", issuer, "groups", made, "directGrantsRevoked", revoked)
	return nil
}

// seedMembers invites and accepts the demo teammates, skipping any that are
// already there.
func seedMembers(ctx context.Context, svc *auth.Service, creds *auth.Credentials, teammates []demoUser, log *slog.Logger) error {
	orgCtx := auth.ContextForOrg(ctx, creds.Principal)

	// Both the invitation and its acceptance upsert, so re-running this simply
	// confirms the memberships that are already there.
	for _, u := range teammates {
		_, token, err := svc.CreateInvite(orgCtx, u.email, u.role, creds.Principal.User.ID, 30*24*time.Hour)
		if err != nil {
			return fmt.Errorf("invite %s: %w", u.email, err)
		}
		if _, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret:   token,
			Name:     u.name,
			Password: demoPassword,
		}); err != nil {
			return fmt.Errorf("accept invite for %s: %w", u.email, err)
		}
		log.Info("seeded member", "email", u.email, "role", u.role)
	}
	return nil
}

// seedWork fills the demo organization with a project and a spread of issues at
// different points in the workflow, so the first thing anybody sees is a
// product with something in it rather than an empty state.
func seedWork(ctx context.Context, cluster *db.Cluster, creds *auth.Credentials, store attachment.Store, log *slog.Logger) error {
	orgCtx := db.PinPrimary(auth.ContextForOrg(ctx, creds.Principal))

	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	// The same provisioner the API uses, so the demo project gets a board.
	projects := project.NewService(cluster, board.Provisioner{}, report.Provisioner{})
	templates := template.NewService(cluster, projects, workflow.NewAdmin(cluster, workflow.NewStore()))
	issues := issue.NewService(cluster, engine, workflow.NewStore())
	actor := issue.Actor{UserID: creds.Principal.User.ID, OrgRole: creds.Principal.Role}

	// A scrum project, because the demo runs sprints: its boards show the
	// sprint in flight, and the kanban board added below shows the contrast.
	created, _, err := templates.Create(orgCtx, "scrum", project.CreateInput{
		Key:         "CP",
		Name:        "Customer Portal",
		Description: "The account area customers sign in to.",
		LeadID:      &actor.UserID,
	}, actor.UserID)
	if errors.Is(err, project.ErrKeyTaken) {
		log.Info("demo project already exists, leaving its issues alone")
		// What was added to the demo later is topped up, so a stack seeded
		// before it existed still gets it.
		keys, err := existingKeys(orgCtx, cluster, "CP")
		if err != nil {
			return err
		}
		if err := seedMilestones(orgCtx, cluster, issues, "CP", keys, actor, log); err != nil {
			return err
		}
		return seedGitea(orgCtx, cluster, issues, "CP", keys, actor, log)
	}
	if err != nil {
		return fmt.Errorf("create demo project: %w", err)
	}

	types, err := issueTypeIDs(orgCtx, cluster)
	if err != nil {
		return err
	}

	// Before any issue exists, so every bug below opens in the workflow the
	// project chose for bugs rather than in the one it was about to inherit.
	if err := seedWorkflowOverride(orgCtx, cluster, created.Key, types, actor.UserID, log); err != nil {
		return err
	}

	// The list is in dependency order: an issue names its parent by summary,
	// and the parent is always earlier in the list than its children.
	// Dates are relative to today so the plan always opens on work in flight.
	// A zero span means unscheduled, which the timeline shows as a row with no
	// bar rather than as a guess.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(offset int) *time.Time {
		d := today.AddDate(0, 0, offset)
		return &d
	}
	pts := func(v float64) *float64 { return &v }

	work := []struct {
		summary string
		// description is what the summary has no room for; most of the demo's
		// larger items carry one, the small ones do not need to.
		description string
		typeName    string
		parent      string
		priority    issue.Priority
		start, due  *time.Time
		// estimate is nil for work nobody has sized, which the plan counts
		// rather than guessing at.
		estimate *float64
		advance  []string
	}{
		{"Make signing in effortless", "Half of our support tickets are about getting in. By the end of the year signing in should be something nobody notices.\nSuccess is a sign-in ticket rate under one a week and no password reset taking more than two minutes.", bootstrap.TypeInitiative, "", issue.PriorityHigh, nil, nil, nil, nil},
		{"Password and recovery", "Everything about passwords: what we accept, how they are recovered, and how long a session lasts.\nThe stories under this are the customer facing pieces; the bugs are what the current flow gets wrong.", bootstrap.TypeEpic, "Make signing in effortless", issue.PriorityHigh, nil, nil, nil, []string{"Start progress"}},
		{"Portal performance", "The portal has to be usable on a phone on a train. First paint under two seconds on a slow 4G connection.", bootstrap.TypeEpic, "Make signing in effortless", issue.PriorityMedium, nil, nil, nil, nil},

		{"Sign-in rejects valid passwords containing a plus sign", "Reported by three customers this month. The password is accepted at signup and refused at sign-in, so the validator and the checker disagree about the plus sign.", bootstrap.TypeBug, "Password and recovery", issue.PriorityHigh, day(-6), day(2), pts(3), []string{"Start progress"}},
		{"Add password reset by email", "As a customer who has forgotten their password, I want a reset link by email so that I can get back in without calling support.\nDone means: a reset can be requested from the sign-in page, the link arrives within a minute, expires after an hour, and works exactly once.", bootstrap.TypeStory, "Password and recovery", issue.PriorityMedium, day(1), day(18), pts(8), []string{"Start progress", "Ready for review"}},
		{"Session expires without warning the user", "", bootstrap.TypeBug, "Password and recovery", issue.PriorityMedium, day(12), day(24), pts(5), nil},
		{"Portal is slow to load on mobile networks", "Lighthouse on a throttled connection puts first paint at 6.2 seconds. Most of it is the bundle; the rest is the avatar images.", bootstrap.TypeBug, "Portal performance", issue.PriorityHigh, day(4), day(30), pts(5), []string{"Start progress"}},

		// Not everything belongs to an epic, and a tree that pretends otherwise
		// is a tree nobody trusts.
		{"Export invoices as CSV", "As an accounts administrator, I want to download a quarter's invoices as CSV so that I can reconcile them in a spreadsheet.", bootstrap.TypeStory, "", issue.PriorityLow, day(21), day(40), pts(13), nil},
		{"Update the privacy policy link in the footer", "", bootstrap.TypeTask, "", issue.PriorityLowest, nil, nil, pts(1), []string{"Close"}},

		{"Design the reset email", "", bootstrap.TypeSubtask, "Add password reset by email", issue.PriorityMedium, day(1), day(6), pts(3), []string{"Start progress"}},
		{"Expire reset tokens after an hour", "", bootstrap.TypeSubtask, "Add password reset by email", issue.PriorityMedium, day(7), day(18), nil, nil},
	}

	keys := map[string]string{}
	for _, w := range work {
		in := issue.CreateInput{
			ProjectKey: created.Key,
			Summary:    w.summary,
			TypeID:     types[w.typeName],
			ParentKey:  keys[w.parent],
			Priority:   w.priority,
			StartDate:  w.start,
			DueDate:    w.due,
			Estimate:   w.estimate,
		}
		if w.description != "" {
			in.Description = paragraphs(w.description)
		}
		created, _, err := issues.Create(orgCtx, in, actor)
		if err != nil {
			return fmt.Errorf("create demo issue %q: %w", w.summary, err)
		}
		keys[w.summary] = created.Key

		for _, name := range w.advance {
			available, err := issues.Transitions(orgCtx, created.Key, actor)
			if err != nil {
				return fmt.Errorf("list transitions for %s: %w", created.Key, err)
			}
			var id uuid.UUID
			for _, t := range available {
				if t.Name == name {
					id = t.ID
				}
			}
			if id == uuid.Nil {
				return fmt.Errorf("transition %q is not available on %s", name, created.Key)
			}
			if _, _, err := issues.Transition(orgCtx, created.Key, issue.TransitionInput{
				TransitionID: id,
			}, actor); err != nil {
				return fmt.Errorf("transition %s through %q: %w", created.Key, name, err)
			}
		}
		log.Info("seeded issue", "key", created.Key, "type", w.typeName, "summary", w.summary)
	}

	formed, err := seedTeams(orgCtx, cluster, issues, created.Key, keys, actor, log)
	if err != nil {
		return err
	}

	// The demo's sprints belong to the platform team, because the work
	// committed to them is theirs. The portal team has a backlog and has not
	// planned a sprint yet, which is the other half of what teams are for.
	platform := formed["Platform"]
	if err := seedSprints(orgCtx, cluster, issues, created.Key, keys, &platform, actor, log); err != nil {
		return err
	}

	if err := seedMilestones(orgCtx, cluster, issues, created.Key, keys, actor, log); err != nil {
		return err
	}

	// One real dependency, deliberately scheduled too early, so the plan opens
	// with something worth noticing rather than an unbroken row of green.
	if _, _, err := issues.AddLink(orgCtx, keys["Add password reset by email"], issue.LinkInput{
		TypeName:  issue.LinkTypeBlocks,
		TargetKey: keys["Session expires without warning the user"],
	}, actor); err != nil {
		return fmt.Errorf("link the demo dependency: %w", err)
	}

	if err := seedAccess(orgCtx, cluster, created.Key, formed, log); err != nil {
		return err
	}

	// The demo's sprints belong to its teams, so the project's own stream has
	// nothing running and a scrum board over the whole project would open
	// empty. Switching that board to kanban puts the two types side by side:
	// this one shows everything, the team boards show their sprints.
	boards := board.NewService(cluster, issues)
	opening, err := boards.ForProject(orgCtx, created.Key)
	if err != nil {
		return fmt.Errorf("find the demo project's board: %w", err)
	}
	kanban := board.TypeKanban
	description := "Every card in the project, whatever sprint it is in."
	if _, _, err := boards.UpdateBoard(orgCtx, opening.ID, board.UpdateBoardInput{
		Type: &kanban, Description: &description,
	}, actor.UserID); err != nil {
		return fmt.Errorf("make the demo's opening board kanban: %w", err)
	}

	// And a second project from another template, so the list shows what a
	// template changes rather than one project that could have been anything.
	if _, _, err := templates.Create(orgCtx, "task-tracking", project.CreateInput{
		Key:         "OPS",
		Name:        "Office operations",
		Description: "Everything that keeps the lights on.",
		LeadID:      &actor.UserID,
	}, actor.UserID); err != nil {
		return fmt.Errorf("create the task tracking demo project: %w", err)
	}

	// And a third from the default template, so the list shows all four
	// shapes and what each leaves out.
	if _, _, err := templates.Create(orgCtx, "kanban", project.CreateInput{
		Key:         "WEB",
		Name:        "Website",
		Description: "The public site and the blog.",
		LeadID:      &actor.UserID,
	}, actor.UserID); err != nil {
		return fmt.Errorf("create the kanban demo project: %w", err)
	}

	if err := seedRepository(orgCtx, cluster, issues, created.Key, keys, actor, log); err != nil {
		return err
	}
	if err := seedGitea(orgCtx, cluster, issues, created.Key, keys, actor, log); err != nil {
		return err
	}

	if err := seedDesk(orgCtx, cluster, issues, templates, actor, log); err != nil {
		return err
	}

	if _, _, err := issues.AddComment(orgCtx, "CP-1",
		issue.TextDocument("Reproduced on staging. The validator rejects anything with a plus sign."),
		actor); err != nil {
		return fmt.Errorf("add demo comment: %w", err)
	}

	if err := seedFields(orgCtx, cluster, created.Key, keys, actor, log); err != nil {
		return err
	}
	if err := seedTimeAndLabels(orgCtx, cluster, issues, keys, actor, log); err != nil {
		return err
	}
	if err := seedAttachment(orgCtx, cluster, store, keys, actor, log); err != nil {
		return err
	}

	return nil
}

// paragraphs turns text with line breaks into a document, one paragraph per
// line, which is how the client wraps what somebody types.
func paragraphs(text string) json.RawMessage {
	content := []any{}
	for _, line := range strings.Split(text, "\n") {
		content = append(content, map[string]any{
			"type":    "paragraph",
			"content": []any{map[string]any{"type": "text", "text": line}},
		})
	}
	encoded, _ := json.Marshal(map[string]any{"type": "doc", "content": content})
	return encoded
}

// seedFields gives the portal project the fields a real one would have, and
// answers them on a few issues so the detail page has something to show.
func seedFields(ctx context.Context, cluster *db.Cluster, projectKey string, keys map[string]string, actor issue.Actor, log *slog.Logger) error {
	fields := field.NewService(cluster)
	defined := map[string]uuid.UUID{}
	for _, def := range []field.Input{
		{Name: "Customer", Kind: field.Select, Options: []string{"Acme", "Globex", "Initech", "All customers"}},
		{Name: "Spec", Kind: field.URL},
		{Name: "Estimated cost", Kind: field.Number},
		{Name: "Needs release note", Kind: field.Checkbox},
	} {
		made, _, err := fields.Create(ctx, projectKey, def)
		if err != nil {
			return fmt.Errorf("define demo field %q: %w", def.Name, err)
		}
		defined[def.Name] = made.ID
	}

	answers := []struct {
		summary, field, value string
	}{
		{"Sign-in rejects valid passwords containing a plus sign", "Customer", `"Globex"`},
		{"Sign-in rejects valid passwords containing a plus sign", "Needs release note", `true`},
		{"Add password reset by email", "Customer", `"All customers"`},
		{"Add password reset by email", "Spec", `"https://wiki.armature.test/portal/password-reset"`},
		{"Add password reset by email", "Estimated cost", `4800`},
		{"Add password reset by email", "Needs release note", `true`},
		{"Export invoices as CSV", "Customer", `"Acme"`},
		{"Export invoices as CSV", "Estimated cost", `12000`},
		{"Portal is slow to load on mobile networks", "Customer", `"Initech"`},
	}
	for _, a := range answers {
		if _, _, err := fields.Set(ctx, keys[a.summary], defined[a.field], json.RawMessage(a.value), actor); err != nil {
			return fmt.Errorf("answer %q on %s: %w", a.field, keys[a.summary], err)
		}
	}
	log.Info("seeded custom fields", "project", projectKey, "fields", len(defined), "answers", len(answers))
	return nil
}

// seedTimeAndLabels gives the demo's work labels, time estimates and a few
// logged hours, so the issue page and the tables have something to show.
func seedTimeAndLabels(ctx context.Context, cluster *db.Cluster, issues *issue.Service, keys map[string]string, actor issue.Actor, log *slog.Logger) error {
	labels := label.NewService(cluster)
	tagged := map[string][]string{
		"Sign-in rejects valid passwords containing a plus sign": {"auth", "regression"},
		"Add password reset by email":                            {"auth", "customer-request"},
		"Session expires without warning the user":               {"auth"},
		"Portal is slow to load on mobile networks":              {"performance", "mobile"},
		"Export invoices as CSV":                                 {"billing", "customer-request"},
		"Design the reset email":                                 {"design"},
	}
	for summary, names := range tagged {
		if _, _, err := labels.SetIssueLabels(ctx, keys[summary], names, actor); err != nil {
			return fmt.Errorf("label %s: %w", keys[summary], err)
		}
	}

	minutes := func(v int) *int { return &v }
	estimated := map[string]*int{
		"Sign-in rejects valid passwords containing a plus sign": minutes(4 * 60),
		"Add password reset by email":                            minutes(3 * 8 * 60),
		"Design the reset email":                                 minutes(6 * 60),
		"Expire reset tokens after an hour":                      minutes(3 * 60),
		"Portal is slow to load on mobile networks":              minutes(2 * 8 * 60),
	}
	for summary, estimate := range estimated {
		if _, _, err := issues.Update(ctx, keys[summary], issue.UpdateInput{TimeEstimate: &estimate}, actor); err != nil {
			return fmt.Errorf("estimate %s: %w", keys[summary], err)
		}
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	daysAgo := func(n int) *time.Time {
		d := today.AddDate(0, 0, -n)
		return &d
	}
	worked := []struct {
		summary string
		minutes int
		daysAgo int
		note    string
	}{
		{"Sign-in rejects valid passwords containing a plus sign", 90, 2, "Reproduced it and traced the validator."},
		{"Sign-in rejects valid passwords containing a plus sign", 60, 1, "Fix written, test added."},
		{"Add password reset by email", 4 * 60, 3, "Request form and the mail template."},
		{"Add password reset by email", 5 * 60, 1, "Token store and expiry."},
		{"Design the reset email", 2 * 60, 2, "First draft in the design tool."},
		{"Portal is slow to load on mobile networks", 3 * 60, 1, "Measured with Lighthouse, found the bundle."},
	}
	for _, w := range worked {
		if _, _, err := issues.LogWork(ctx, keys[w.summary], issue.WorklogInput{Minutes: w.minutes, StartedOn: daysAgo(w.daysAgo), Note: w.note}, actor); err != nil {
			return fmt.Errorf("log work on %s: %w", keys[w.summary], err)
		}
	}
	log.Info("seeded labels, estimates and worklogs", "labelled", len(tagged), "estimated", len(estimated), "worklogs", len(worked))
	return nil
}

// seedAttachment puts one file on the reset story, when there is a bucket.
func seedAttachment(ctx context.Context, cluster *db.Cluster, store attachment.Store, keys map[string]string, actor issue.Actor, log *slog.Logger) error {
	if _, off := store.(attachment.Unavailable); off {
		log.Warn("no attachment bucket configured; the demo has no attachments")
		return nil
	}
	notes := "# Acceptance notes: password reset\n\n" +
		"- The request form accepts any address and always says the same thing, so nobody can probe for accounts.\n" +
		"- The link works once. Following it twice shows a friendly explanation, not an error page.\n" +
		"- Tokens die after an hour; the mail says so.\n" +
		"- Resetting signs every other session out.\n"
	attachments := attachment.NewService(cluster, store, issue.NewService(cluster, workflow.NewEngine(workflow.NewDefaultRegistry()), workflow.NewStore()))
	made, _, err := attachments.Upload(ctx, keys["Add password reset by email"], attachment.UploadInput{
		FileName:    "acceptance-notes.md",
		ContentType: "text/markdown",
		Body:        strings.NewReader(notes),
	}, actor)
	if err != nil {
		return fmt.Errorf("attach the demo notes: %w", err)
	}
	log.Info("seeded attachment", "issue", made.IssueKey, "file", made.FileName)
	return nil
}

// seedAccess gives the demo the shape of a real access setup: a group per team
// with a role on it, and one person who can only read.
//
// A demo where everybody is an administrator shows nothing about what the roles
// are for.
func seedAccess(ctx context.Context, cluster *db.Cluster, projectKey string, teams map[string]uuid.UUID, log *slog.Logger) error {
	perms := perm.NewStore(cluster)

	people, err := membersByEmail(ctx, cluster)
	if err != nil {
		return err
	}

	// The platform lead runs sprints in the demo project; the group is what
	// holds the role, so adding somebody to it is all it takes to hand over.
	scrum, _, err := perms.CreateGroup(ctx, perm.GroupInput{
		Name:        "Platform scrum",
		Description: "Runs the platform team's sprints.",
	}, people["ada@armature.test"])
	if err != nil {
		return fmt.Errorf("create the scrum group: %w", err)
	}
	if _, _, err := perms.AddToGroup(ctx, scrum.ID, people["grace@armature.test"], people["ada@armature.test"]); err != nil {
		return fmt.Errorf("put grace in the scrum group: %w", err)
	}
	if _, _, err := perms.Grant(ctx, perm.GrantInput{
		Role: perm.ScrumMaster, ProjectKey: projectKey, GroupID: &scrum.ID,
	}, people["ada@armature.test"]); err != nil {
		return fmt.Errorf("grant the scrum role: %w", err)
	}

	// Nobody is granted a role directly: what Grace, Alan and Rita may do
	// comes from the Keycloak groups they are in, see seedSingleSignOn.

	log.Info("seeded access", "project", projectKey, "group", scrum.Name)
	return nil
}

// membersByEmail looks up the demo accounts so roles can be given by name.
func membersByEmail(ctx context.Context, cluster *db.Cluster) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.email, u.id FROM app_user u
			JOIN org_member m ON m.user_id = u.id AND m.org_id = current_org_id()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				email string
				id    uuid.UUID
			)
			if err := rows.Scan(&email, &id); err != nil {
				return err
			}
			out[email] = id
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the demo members: %w", err)
	}
	return out, nil
}

// seedTeams splits the demo project between two teams, each with a board of
// its own, so the difference between one backlog and several is visible without
// anybody having to set it up first.
func seedTeams(ctx context.Context, cluster *db.Cluster, issues *issue.Service, projectKey string, keys map[string]string, actor issue.Actor, log *slog.Logger) (map[string]uuid.UUID, error) {
	teams := team.NewService(cluster)
	boards := board.NewService(cluster, issues)

	formed := map[string][]string{
		"Platform": {
			"Sign-in rejects valid passwords containing a plus sign",
			"Add password reset by email",
			"Session expires without warning the user",
			"Design the reset email",
			"Expire reset tokens after an hour",
		},
		"Portal": {
			"Portal is slow to load on mobile networks",
			"Export invoices as CSV",
		},
	}

	ids := map[string]uuid.UUID{}
	for _, name := range []string{"Platform", "Portal"} {
		created, _, err := teams.Create(ctx, projectKey, team.CreateInput{
			Name:        name,
			Description: "Owns " + strings.ToLower(name) + " work in the customer portal.",
		}, actor.UserID)
		if err != nil {
			return nil, fmt.Errorf("form the %s team: %w", name, err)
		}
		ids[name] = created.ID

		if _, _, err := teams.AddMember(ctx, created.ID, actor.UserID, true, actor.UserID); err != nil {
			return nil, fmt.Errorf("put the owner on %s: %w", name, err)
		}

		for _, summary := range formed[name] {
			if _, _, err := issues.SetTeam(ctx, keys[summary], &created.ID, actor); err != nil {
				return nil, fmt.Errorf("hand %q to %s: %w", summary, name, err)
			}
		}

		// No type stated: a team's board takes after the project's, which
		// here makes it a scrum board following the team's own sprint.
		if _, _, err := boards.CreateBoard(ctx, projectKey, board.CreateInput{
			Name:        name + " board",
			Description: "Only the work " + name + " is carrying.",
			TeamID:      &created.ID,
		}, actor.UserID); err != nil {
			return nil, fmt.Errorf("give %s a board: %w", name, err)
		}
		log.Info("seeded team", "project", projectKey, "team", name, "issues", len(formed[name]))
	}
	return ids, nil
}

// existingKeys reads a project's issue keys by summary, which is how the seed
// names issues, so a top-up can find the ones an earlier run made.
func existingKeys(ctx context.Context, cluster *db.Cluster, projectKey string) (map[string]string, error) {
	keys := map[string]string{}
	err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT p.key || '-' || i.key_num, i.summary FROM issue i
			JOIN project p ON p.id = i.project_id WHERE p.key = $1`, projectKey)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key, summary string
			if err := rows.Scan(&key, &summary); err != nil {
				return err
			}
			keys[summary] = key
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the demo's issues: %w", err)
	}
	return keys, nil
}

// seedMilestones gives the demo two targets to read progress off: one close
// and partly done, one further out and barely begun. A milestone that already
// exists is left alone, so running the seed twice adds nothing.
func seedMilestones(ctx context.Context, cluster *db.Cluster, issues *issue.Service, projectKey string, keys map[string]string, actor issue.Actor, log *slog.Logger) error {
	milestones := milestone.NewService(cluster)
	existing, err := milestones.List(ctx, projectKey, true)
	if err != nil {
		return fmt.Errorf("read milestones: %w", err)
	}
	have := map[string]bool{}
	for _, m := range existing {
		have[strings.ToLower(m.Name)] = true
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(offset int) *time.Time {
		d := today.AddDate(0, 0, offset)
		return &d
	}

	targets := []struct {
		name, description string
		due               *time.Time
		summaries         []string
	}{
		{"Password reset live", "Customers can get back in without calling support.", day(20), []string{
			"Sign-in rejects valid passwords containing a plus sign",
			"Add password reset by email",
			"Session expires without warning the user",
			"Update the privacy policy link in the footer",
		}},
		{"Portal usable on a phone", "First paint under two seconds on a slow connection.", day(45), []string{
			"Portal is slow to load on mobile networks",
			"Export invoices as CSV",
		}},
	}
	for _, target := range targets {
		if have[strings.ToLower(target.name)] {
			continue
		}
		created, _, err := milestones.Create(ctx, projectKey, milestone.Input{
			Name: target.name, Description: target.description, DueOn: target.due,
		}, actor.UserID)
		if err != nil {
			return fmt.Errorf("create milestone %s: %w", target.name, err)
		}
		for _, summary := range target.summaries {
			if _, _, err := issues.SetMilestone(ctx, keys[summary], &created.ID, actor); err != nil {
				return fmt.Errorf("assign %s to %s: %w", keys[summary], target.name, err)
			}
		}
		log.Info("seeded milestone", "name", target.name, "issues", len(target.summaries))
	}
	return nil
}

// seedSprints gives the demo a sprint that has already run and one in flight,
// so the capacity numbers open on something worth reading rather than on zero.
//
// The running sprint is deliberately committed past its capacity: an
// over-committed sprint is the normal case a team needs to see, and the plan
// reports it rather than refusing it.
func seedSprints(ctx context.Context, cluster *db.Cluster, issues *issue.Service, projectKey string, keys map[string]string, teamID *uuid.UUID, actor issue.Actor, log *slog.Logger) error {
	sprints := sprint.NewService(cluster)
	sprints.CountWith(plan.NewService(issues, sprints))

	today := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(offset int) *time.Time {
		d := today.AddDate(0, 0, offset)
		return &d
	}
	capacity := 16.0

	finished, _, err := sprints.Create(ctx, projectKey, sprint.CreateInput{
		Name: "Sprint 1", Goal: "Get password reset moving.",
		StartsOn: day(-21), EndsOn: day(-8), Capacity: &capacity, TeamID: teamID,
	}, actor.UserID)
	if err != nil {
		return fmt.Errorf("create the finished sprint: %w", err)
	}
	running, _, err := sprints.Create(ctx, projectKey, sprint.CreateInput{
		Name: "Sprint 2", Goal: "Ship password reset and stop the session surprises.",
		StartsOn: day(-7), EndsOn: day(6), Capacity: &capacity, TeamID: teamID,
	}, actor.UserID)
	if err != nil {
		return fmt.Errorf("create the running sprint: %w", err)
	}
	next, _, err := sprints.Create(ctx, projectKey, sprint.CreateInput{
		Name: "Sprint 3", StartsOn: day(7), EndsOn: day(20), Capacity: &capacity, TeamID: teamID,
	}, actor.UserID)
	if err != nil {
		return fmt.Errorf("create the next sprint: %w", err)
	}

	// The first sprint ran and closed, carrying what was left into the second.
	if _, _, err := sprints.Start(ctx, finished.ID, actor.UserID); err != nil {
		return fmt.Errorf("start the first sprint: %w", err)
	}
	if _, _, err := issues.SetSprint(ctx, keys["Update the privacy policy link in the footer"], &finished.ID, actor); err != nil {
		return fmt.Errorf("commit the first sprint's work: %w", err)
	}
	if _, _, err := sprints.Complete(ctx, finished.ID,
		sprint.CompleteInput{MoveTo: &running.ID}, actor.UserID); err != nil {
		return fmt.Errorf("complete the first sprint: %w", err)
	}

	committed := []string{
		"Sign-in rejects valid passwords containing a plus sign",
		"Add password reset by email",
		"Session expires without warning the user",
		"Portal is slow to load on mobile networks",
	}
	for _, summary := range committed {
		if _, _, err := issues.SetSprint(ctx, keys[summary], &running.ID, actor); err != nil {
			return fmt.Errorf("commit %q: %w", summary, err)
		}
	}
	if _, _, err := sprints.Start(ctx, running.ID, actor.UserID); err != nil {
		return fmt.Errorf("start the running sprint: %w", err)
	}

	if _, _, err := issues.SetSprint(ctx, keys["Export invoices as CSV"], &next.ID, actor); err != nil {
		return fmt.Errorf("commit the next sprint's work: %w", err)
	}

	log.Info("seeded sprints", "project", projectKey, "running", running.Name)
	return nil
}

// seedDesk sets up a service desk with a customer's request in it: one being
// answered, one waiting on the customer, and one already past its goal, so the
// queue opens on something worth looking at.
func seedDesk(ctx context.Context, cluster *db.Cluster, issues *issue.Service, templates *template.Service, actor issue.Actor, log *slog.Logger) error {
	d := desk.NewService(cluster, issues)
	templates.WithDesk(d)
	created, _, err := templates.Create(ctx, "service-desk", project.CreateInput{
		Key:         "HELP",
		Name:        "IT helpdesk",
		Description: "Where the company asks for help.",
		LeadID:      &actor.UserID,
	}, actor.UserID)
	if err != nil {
		return fmt.Errorf("create the demo desk: %w", err)
	}

	people, err := membersByEmail(ctx, cluster)
	if err != nil {
		return err
	}
	customer := issue.Actor{UserID: people["customer@armature.test"], OrgRole: auth.RoleCustomer}
	types, err := d.RequestTypes(ctx, created.Key)
	if err != nil {
		return err
	}
	byName := map[string]uuid.UUID{}
	for _, rt := range types {
		byName[rt.Name] = rt.ID
	}

	answered, _, err := d.Raise(ctx, desk.RaiseInput{
		RequestTypeID: byName["Report a problem"], Summary: "Cannot print from the third floor",
		Description: "Every job from our floor sits in the queue and never comes out.",
	}, customer)
	if err != nil {
		return fmt.Errorf("raise the demo request: %w", err)
	}
	if _, _, err := issues.AddNote(ctx, answered.Key, issue.TextDocument("Same printer as last month. Check the driver on the print server first."), actor); err != nil {
		return err
	}
	if _, _, err := issues.AddComment(ctx, answered.Key, issue.TextDocument("Thanks, we are looking at the print server now."), actor); err != nil {
		return err
	}
	if err := advance(ctx, issues, answered.Key, actor, "Start work", "Wait for customer"); err != nil {
		return err
	}
	if _, _, err := issues.AddComment(ctx, answered.Key, issue.TextDocument("Could you try once more and tell us the exact error on the printer's screen?"), actor); err != nil {
		return err
	}

	waiting, _, err := d.Raise(ctx, desk.RaiseInput{
		RequestTypeID: byName["Request something"], Summary: "Access to the finance shared drive",
	}, customer)
	if err != nil {
		return err
	}
	_ = waiting

	late, _, err := d.Raise(ctx, desk.RaiseInput{
		RequestTypeID: byName["Ask a question"], Summary: "Which VPN client should I use on the new laptop?",
	}, customer)
	if err != nil {
		return err
	}
	// Ten hours in on an eight hour goal: the watch will mark it the next time
	// it looks, and the queue already shows it red.
	_, err = cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE sla_timer t SET running_since = now() - interval '10 hours', started_at = now() - interval '10 hours'
			FROM issue i WHERE i.id = t.issue_id AND i.id = $1`, late.ID)
		return err
	})
	if err != nil {
		return fmt.Errorf("age the demo request: %w", err)
	}

	log.Info("seeded service desk", "project", created.Key, "requests", 3)
	return nil
}

// advance takes named transitions on an issue, in order.
func advance(ctx context.Context, issues *issue.Service, key string, actor issue.Actor, names ...string) error {
	for _, name := range names {
		available, err := issues.Transitions(ctx, key, actor)
		if err != nil {
			return err
		}
		var id uuid.UUID
		for _, t := range available {
			if t.Name == name {
				id = t.ID
			}
		}
		if id == uuid.Nil {
			return fmt.Errorf("transition %q is not available on %s", name, key)
		}
		if _, _, err := issues.Transition(ctx, key, issue.TransitionInput{TransitionID: id}, actor); err != nil {
			return fmt.Errorf("transition %s through %q: %w", key, name, err)
		}
	}
	return nil
}

// seedRepository connects a repository to the demo project and records what a
// few days of work on it would have sent: commits naming issues, a pull request
// with a green check, and one that is still red.
//
// It records the delivery directly rather than through the webhook, since the
// demo has no host to receive from; everything downstream of the endpoint is
// the same code.
func seedRepository(ctx context.Context, cluster *db.Cluster, issues *issue.Service, projectKey string, keys map[string]string, actor issue.Actor, log *slog.Logger) error {
	repos := git.NewService(cluster, issues)
	repo, _, err := repos.Connect(ctx, projectKey, git.ConnectInput{
		Host: git.GitHub, Name: "demo-company/customer-portal", TransitionOnMerge: "Close",
	}, actor.UserID)
	if err != nil {
		return fmt.Errorf("connect the demo repository: %w", err)
	}

	reset := keys["Add password reset by email"]
	plus := keys["Sign-in rejects valid passwords containing a plus sign"]
	at := func(daysAgo int) time.Time { return time.Now().UTC().AddDate(0, 0, -daysAgo) }
	commit := func(sha, message string, daysAgo int) git.Commit {
		return git.Commit{
			SHA: sha, Message: message, AuthorName: "Grace Hopper", AuthorEmail: "grace@armature.test",
			URL: repo.URL + "/commit/" + sha, Branch: reset + "-password-reset", CommittedAt: at(daysAgo),
		}
	}
	finished := at(1)
	_, err = repos.Record(ctx, repo, &git.Delivery{
		Branches: []git.Branch{
			{Name: reset + "-password-reset", URL: repo.URL + "/tree/" + reset + "-password-reset"},
			{Name: plus + "-plus-sign", URL: repo.URL + "/tree/" + plus + "-plus-sign"},
		},
		Commits: []git.Commit{
			commit("3f1c2a9d5e7b4c6a8f0d1e2b3c4d5e6f7a8b9c0d", reset+" Send the reset email through the mailer", 3),
			commit("a7d4e1f0b3c2d5e6f7a8b9c0d1e2f3a4b5c6d7e8", reset+" Expire reset tokens after an hour", 2),
			commit("c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0", plus+" Stop the validator rejecting a plus sign", 1),
		},
		PullRequests: []git.PullRequest{
			{Number: 42, Title: reset + " Password reset by email", URL: repo.URL + "/pull/42", State: git.PullOpen,
				SourceBranch: reset + "-password-reset", TargetBranch: "main", AuthorName: "grace",
				HeadSHA: "a7d4e1f0b3c2d5e6f7a8b9c0d1e2f3a4b5c6d7e8", OpenedAt: at(2)},
			{Number: 43, Title: plus + " Accept a plus sign in passwords", URL: repo.URL + "/pull/43", State: git.PullOpen,
				SourceBranch: plus + "-plus-sign", TargetBranch: "main", AuthorName: "grace",
				HeadSHA: "c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0", OpenedAt: at(1)},
		},
		Runs: []git.CIRun{
			{ExternalID: "check_run:1001", Name: "build and test", Status: git.CISuccess, URL: repo.URL + "/runs/1001",
				SHA: "a7d4e1f0b3c2d5e6f7a8b9c0d1e2f3a4b5c6d7e8", Branch: reset + "-password-reset", StartedAt: at(2), FinishedAt: &finished},
			{ExternalID: "check_run:1002", Name: "build and test", Status: git.CIFailure, URL: repo.URL + "/runs/1002",
				SHA: "c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0", Branch: plus + "-plus-sign", StartedAt: at(1), FinishedAt: &finished},
		},
	})
	if err != nil {
		return fmt.Errorf("record the demo repository's history: %w", err)
	}
	log.Info("seeded repository", "project", projectKey, "repository", repo.Name)
	return nil
}

// seedWorkflowOverride gives the demo project a workflow of its own for bugs,
// so the settings pages open on a real difference rather than on a table where
// every row says the same thing.
//
// Bugs here skip review: they go straight from In Progress to Done. Everything
// else in the project still follows the organization.
func seedWorkflowOverride(ctx context.Context, cluster *db.Cluster, projectKey string, types map[string]uuid.UUID, actor uuid.UUID, log *slog.Logger) error {
	admin := workflow.NewAdmin(cluster, workflow.NewStore())
	projects := project.NewService(cluster)

	statuses, err := statusIDs(ctx, cluster)
	if err != nil {
		return err
	}
	todo := statuses[bootstrap.StatusToDo]
	progress := statuses[bootstrap.StatusInProgress]
	done := statuses[bootstrap.StatusDone]

	built, _, err := admin.CreateWorkflow(ctx, workflow.GraphInput{
		Name:        "Bug triage",
		Description: "Confirm it, fix it, close it. No review step.",
		Steps: []workflow.StepInput{
			{StatusID: todo, IsInitial: true},
			{StatusID: progress},
			{StatusID: done},
		},
		Transitions: []workflow.TransitionInput{
			{Name: "Start progress", FromStatusID: &todo, ToStatusID: progress},
			{Name: "Fixed", FromStatusID: &progress, ToStatusID: done},
			{Name: "Reopen", FromStatusID: &done, ToStatusID: todo},
			{Name: "Close", ToStatusID: done},
		},
	}, actor)
	if err != nil {
		return fmt.Errorf("create the demo bug workflow: %w", err)
	}

	bugType := types[bootstrap.TypeBug]
	scheme, _, err := admin.CreateScheme(ctx, workflow.SchemeInput{
		Name:  "Customer Portal workflows",
		Items: []workflow.SchemeItemInput{{IssueTypeID: &bugType, WorkflowID: built.ID}},
	}, actor)
	if err != nil {
		return fmt.Errorf("create the demo scheme: %w", err)
	}

	if _, _, err := projects.SetWorkflowScheme(ctx, projectKey, &scheme.ID, actor); err != nil {
		return fmt.Errorf("give the demo project its scheme: %w", err)
	}
	log.Info("seeded workflow override", "project", projectKey, "scheme", scheme.Name)
	return nil
}

// statusIDs looks up the organization's statuses by name.
func statusIDs(ctx context.Context, cluster *db.Cluster) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id, name FROM issue_status`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id   uuid.UUID
				name string
			)
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			out[name] = id
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the demo statuses: %w", err)
	}
	return out, nil
}

// issueTypeIDs looks up the organization's issue types by name.
func issueTypeIDs(ctx context.Context, cluster *db.Cluster) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id, name FROM issue_type`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id   uuid.UUID
				name string
			)
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			out[name] = id
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the demo issue types: %w", err)
	}
	return out, nil
}

func printCredentials(email string) {
	fmt.Printf("\n  Demo organization ready.\n\n    sign in at  http://localhost:5173/login\n    email       %s\n    password    %s\n\n", email, demoPassword)
}
