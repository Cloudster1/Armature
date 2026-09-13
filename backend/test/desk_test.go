//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
)

// aDesk makes a service desk project in the workspace and returns the desk
// service that observes its issues.
func (ws *workspace) aDesk(t *testing.T, h *harness, name string) (*desk.Service, *project.Project) {
	t.Helper()
	d := desk.NewService(h.cluster, ws.issues)
	templates := template.NewService(h.cluster, ws.projects, ws.admin).WithDesk(d)
	p, _, err := templates.Create(ws.ctx, "service-desk", project.CreateInput{
		Name: name, Key: strings.ToUpper(name[:min(len(name), 3)] + uuid.New().String()[:3]),
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create the desk: %v", err)
	}
	return d, p
}

// customerOf adds a portal customer to the organization.
func (ws *workspace) customerOf(t *testing.T, h *harness, prefix string) issue.Actor {
	t.Helper()
	id := h.joinExisting(t, ws, h.email(t, prefix), "customer")
	return issue.Actor{UserID: id, OrgRole: auth.RoleCustomer}
}

func requestTypeNamed(t *testing.T, types []desk.RequestType, name string) desk.RequestType {
	t.Helper()
	for _, rt := range types {
		if rt.Name == name {
			return rt
		}
	}
	t.Fatalf("no request type called %q in %+v", name, types)
	return desk.RequestType{}
}

func timerFor(t *testing.T, timers []desk.Timer, metric desk.Metric) desk.Timer {
	t.Helper()
	for _, tm := range timers {
		if tm.Metric == metric {
			return tm
		}
	}
	t.Fatalf("no %s timer in %+v", metric, timers)
	return desk.Timer{}
}

func TestAServiceDeskIsSetUpFromItsTemplate(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "desking")
	d, p := ws.aDesk(t, h, "Helpdesk")

	if p.Kind != project.KindService {
		t.Errorf("kind = %q, want service", p.Kind)
	}
	types, err := d.RequestTypes(ws.ctx, p.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 3 {
		t.Errorf("request types = %+v, want the three defaults", types)
	}
	if problem := requestTypeNamed(t, types, "Report a problem"); problem.IssueTypeName != bootstrap.TypeBug || problem.Priority != issue.PriorityHigh {
		t.Errorf("a problem comes in as %s at %s, want a high Bug", problem.IssueTypeName, problem.Priority)
	}

	policies, err := d.Policies(ws.ctx, p.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 {
		t.Fatalf("policies = %+v, want first response and resolution", policies)
	}
	for _, pol := range policies {
		if pol.Goals[issue.PriorityHigh] == 0 {
			t.Errorf("%s has no goal for high priority", pol.Name)
		}
		if len(pol.PauseStatuses) != 1 || pol.PauseStatuses[0] != desk.WaitingOnCustomer {
			t.Errorf("%s pauses in %v, want only while waiting for the customer", pol.Name, pol.PauseStatuses)
		}
	}

	b := ws.boardOf(t, p.Key)
	want := "Waiting for support,In Progress,Waiting for customer,Resolved"
	if got := laneNames(b); got != want {
		t.Errorf("board lanes = %s, want %s", got, want)
	}
}

func TestACustomerRaisesAndFollowsARequest(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "raising")
	d, p := ws.aDesk(t, h, "Support")
	customer := ws.customerOf(t, h, "customer")
	stranger := ws.customerOf(t, h, "stranger")

	types, _ := d.RequestTypes(ws.ctx, p.Key)
	problem := requestTypeNamed(t, types, "Report a problem")

	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: problem.ID, Summary: "I cannot sign in", Description: "It says my password is wrong."}, customer)
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if raised.Reporter == nil || raised.Reporter.ID != customer.UserID {
		t.Errorf("reporter = %+v, want the customer", raised.Reporter)
	}
	if raised.RequestTypeID == nil || *raised.RequestTypeID != problem.ID || raised.RequestTypeName != "Report a problem" {
		t.Errorf("request type on the issue = %v %q", raised.RequestTypeID, raised.RequestTypeName)
	}
	if raised.Priority != issue.PriorityHigh || raised.Type.Name != bootstrap.TypeBug {
		t.Errorf("came in as %s %s, want a high bug as the request type says", raised.Priority, raised.Type.Name)
	}
	if raised.Status.Name != "Waiting for support" {
		t.Errorf("status = %q, want the desk's first status", raised.Status.Name)
	}

	t.Run("both clocks start running", func(t *testing.T) {
		timers, err := d.TimersFor(ws.ctx, raised.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(timers) != 2 {
			t.Fatalf("timers = %+v, want two", timers)
		}
		for _, tm := range timers {
			if tm.RunningSince == nil || tm.Paused || tm.CompletedAt != nil || tm.Breached {
				t.Errorf("%s is not simply running: %+v", tm.Metric, tm)
			}
		}
		if got := timerFor(t, timers, desk.FirstResponse).GoalMinutes; got != 4*60 {
			t.Errorf("first response goal = %d minutes, want the high priority's 4 hours", got)
		}
	})

	t.Run("the customer sees their request and nobody else's", func(t *testing.T) {
		mine, err := d.MyRequests(ws.ctx, customer.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if len(mine) != 1 || mine[0].Key != raised.Key {
			t.Errorf("my requests = %+v", mine)
		}
		if _, err := d.Request(ws.ctx, raised.Key, stranger.UserID); !errors.Is(err, desk.ErrNotFound) {
			t.Errorf("a stranger reading the request got %v, want not found", err)
		}
	})

	t.Run("a note is for agents and a reply answers the customer", func(t *testing.T) {
		if _, _, err := ws.issues.AddNote(ws.ctx, raised.Key, issue.TextDocument("looks like the plus sign bug"), ws.actor); err != nil {
			t.Fatal(err)
		}
		req, err := d.Request(ws.ctx, raised.Key, customer.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if len(req.Comments) != 0 {
			t.Errorf("the customer sees %d comments after an internal note, want none", len(req.Comments))
		}
		first := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse)
		if first.CompletedAt != nil {
			t.Error("a note counted as a first response")
		}

		if _, _, err := ws.issues.AddComment(ws.ctx, raised.Key, issue.TextDocument("We are on it."), ws.actor); err != nil {
			t.Fatal(err)
		}
		req, _ = d.Request(ws.ctx, raised.Key, customer.UserID)
		if len(req.Comments) != 1 || req.Comments[0].Internal {
			t.Errorf("the customer sees %+v, want the one public reply", req.Comments)
		}
		first = timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse)
		if first.CompletedAt == nil {
			t.Error("an agent's reply did not complete the first response clock")
		}
		agentView, _ := ws.issues.Comments(ws.ctx, raised.Key, true)
		if len(agentView) != 2 {
			t.Errorf("agents see %d comments, want the note and the reply", len(agentView))
		}
	})

	t.Run("waiting on the customer stops the clock and their reply restarts it", func(t *testing.T) {
		ws.move(t, h, raised.Key, "Start work")
		ws.move(t, h, raised.Key, "Wait for customer")
		resolution := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution)
		if !resolution.Paused || resolution.RunningSince != nil {
			t.Errorf("resolution clock while waiting = %+v, want paused", resolution)
		}

		if _, _, err := d.Reply(ws.ctx, raised.Key, "Still broken, here is a screenshot", customer); err != nil {
			t.Fatal(err)
		}
		after, _ := ws.issues.ByKey(ws.ctx, raised.Key)
		if after.Status.Name != bootstrap.StatusInProgress {
			t.Errorf("after the reply the request is %q, want back In Progress", after.Status.Name)
		}
		resolution = timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution)
		if resolution.Paused || resolution.RunningSince == nil {
			t.Errorf("resolution clock after the reply = %+v, want running", resolution)
		}
	})

	t.Run("resolving completes the clock", func(t *testing.T) {
		ws.move(t, h, raised.Key, "Resolve")
		resolution := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution)
		if resolution.CompletedAt == nil || resolution.Breached {
			t.Errorf("resolution clock = %+v, want completed within its goal", resolution)
		}
		ws.move(t, h, raised.Key, "Reopen")
		resolution = timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution)
		if resolution.CompletedAt != nil || resolution.RunningSince == nil || resolution.ElapsedSeconds > 5 {
			t.Errorf("reopened resolution clock = %+v, want started over", resolution)
		}
	})
}

func mustTimers(t *testing.T, d *desk.Service, ws *workspace, key string) []desk.Timer {
	t.Helper()
	timers, err := d.TimersFor(ws.ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	return timers
}

// ageTimer pretends a clock has been running for a while, so that a test does
// not have to wait for the goal to pass.
func ageTimer(t *testing.T, h *harness, ws *workspace, key string, metric desk.Metric, minutesAgo int) {
	t.Helper()
	projectKey, num, _ := issue.ParseKey(key)
	_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE sla_timer t SET running_since = now() - make_interval(mins => $3)
			FROM issue i, project p, sla_policy sp
			WHERE i.id = t.issue_id AND p.id = i.project_id AND sp.id = t.policy_id
			  AND p.key = $1 AND i.key_num = $2 AND sp.metric = $4`, projectKey, num, minutesAgo, string(metric))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestABreachIsNoticedByTheWatch(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "breaching")
	d, p := ws.aDesk(t, h, "Slow desk")
	customer := ws.customerOf(t, h, "waiting")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "How do I export?"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	// Medium priority: first response within 8 hours. Nine have passed.
	ageTimer(t, h, ws, raised.Key, desk.FirstResponse, 9*60)

	watch := desk.NewWatch(h.cluster, d, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	n, err := watch.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("the watch recorded %d breaches, want at least this one", n)
	}
	first := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse)
	if first.BreachedAt == nil || !first.Breached || first.RemainingSeconds >= 0 {
		t.Errorf("first response clock = %+v, want breached", first)
	}
	resolution := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution)
	if resolution.Breached {
		t.Error("the resolution clock, well within its three days, is called breached")
	}

	var breaches int
	if err := h.super.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2`, ws.orgID, events.TopicSLABreached).Scan(&breaches); err != nil {
		t.Fatal(err)
	}
	if breaches != 1 {
		t.Errorf("%d breach events, want one", breaches)
	}

	t.Run("a breach is recorded once", func(t *testing.T) {
		if n, _ := watch.Once(context.Background()); n != 0 {
			t.Errorf("a second look recorded %d more breaches for the same clock", n)
		}
	})

	t.Run("and the breached queue shows it", func(t *testing.T) {
		rows, err := d.Queue(ws.ctx, p.Key, desk.QueueBreached, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Issue.Key != raised.Key {
			t.Errorf("breached queue = %+v", rows)
		}
	})
}

func TestTheQueuePutsTheMostPressingFirst(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "queueing")
	d, p := ws.aDesk(t, h, "Queue desk")
	customer := ws.customerOf(t, h, "queuer")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	question := requestTypeNamed(t, types, "Ask a question")

	calm, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: question.ID, Summary: "no hurry"}, customer)
	urgent, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: question.ID, Summary: "getting late"}, customer)
	ageTimer(t, h, ws, urgent.Key, desk.FirstResponse, 7*60)

	rows, err := d.Queue(ws.ctx, p.Key, desk.QueueOpen, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Issue.Key != urgent.Key || rows[1].Issue.Key != calm.Key {
		t.Errorf("queue order = %v, want the one with least time left first", queueKeys(rows))
	}
	if rows[0].RequestTypeName != "Ask a question" || len(rows[0].Timers) != 2 {
		t.Errorf("queue row = %+v, want its request type and clocks", rows[0])
	}

	ws.move(t, h, urgent.Key, "Start work")
	mine, _ := d.Queue(ws.ctx, p.Key, desk.QueueMine, ws.actor.UserID)
	if len(mine) != 1 || mine[0].Issue.Key != urgent.Key {
		t.Errorf("mine = %v, want the one starting work assigned to me", queueKeys(mine))
	}
	unassigned, _ := d.Queue(ws.ctx, p.Key, desk.QueueUnassigned, ws.actor.UserID)
	if len(unassigned) != 1 || unassigned[0].Issue.Key != calm.Key {
		t.Errorf("unassigned = %v, want the other", queueKeys(unassigned))
	}
}

func queueKeys(rows []desk.QueueRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Issue.Key)
	}
	return out
}

// fakeMailer keeps what would have been sent.
type fakeMailer struct {
	mu   sync.Mutex
	sent []struct{ To, Subject, Body string }
}

func (m *fakeMailer) Send(ctx context.Context, mail desk.Mail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, struct{ To, Subject, Body string }{mail.To, mail.Subject, mail.Body})
	return nil
}

func TestACustomerIsMailedAboutTheirRequest(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "mailing")
	d, p := ws.aDesk(t, h, "Mail desk")
	customer := ws.customerOf(t, h, "mailed")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the invoice?"}, customer)

	mailer := &fakeMailer{}
	notifier := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	handle := func(topic string, payload map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if err := notifier.Handle(context.Background(), events.Event{OrgID: ws.orgID, Topic: topic, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}

	note, _, _ := ws.issues.AddNote(ws.ctx, raised.Key, issue.TextDocument("check the billing service"), ws.actor)
	handle(events.TopicCommentAdded, map[string]any{"key": raised.Key, "commentId": note.ID, "internal": true, "actorId": ws.actor.UserID})
	if len(mailer.sent) != 0 {
		t.Fatalf("an internal note was mailed to the customer: %+v", mailer.sent)
	}

	reply, _, _ := ws.issues.AddComment(ws.ctx, raised.Key, issue.TextDocument("It is under Billing, top right."), ws.actor)
	handle(events.TopicCommentAdded, map[string]any{"key": raised.Key, "commentId": reply.ID, "internal": false, "actorId": ws.actor.UserID})
	if len(mailer.sent) != 1 {
		t.Fatalf("mails = %+v, want one for the reply", mailer.sent)
	}
	mail := mailer.sent[0]
	if !strings.HasSuffix(mail.To, "@armature.test") && !strings.Contains(mail.To, "mailed") {
		t.Errorf("mailed %q, want the customer", mail.To)
	}
	if !strings.Contains(mail.Subject, raised.Key) || !strings.Contains(mail.Body, "under Billing") || !strings.Contains(mail.Body, "http://app.test/desk/") || !strings.Contains(mail.Body, "next=%2Fportal%2Frequests%2F"+raised.Key) {
		t.Errorf("mail = %+v, want the key, the words and a link to the request", mail)
	}

	// The customer's own reply is not mailed back to them.
	theirs, _, _ := d.Reply(ws.ctx, raised.Key, "Found it, thanks", customer)
	handle(events.TopicCommentAdded, map[string]any{"key": raised.Key, "commentId": theirs.ID, "internal": false, "actorId": customer.UserID})
	if len(mailer.sent) != 1 {
		t.Errorf("the customer was mailed their own reply: %+v", mailer.sent)
	}

	ws.move(t, h, raised.Key, "Resolve")
	handle(events.TopicIssueTransitioned, map[string]any{"key": raised.Key, "transition": "Resolve", "fromStatus": "Waiting for support", "toStatus": "Resolved"})
	if len(mailer.sent) != 2 || !strings.Contains(mailer.sent[1].Subject, "resolved") {
		t.Errorf("mails after resolving = %+v, want one saying so", mailer.sent)
	}
}

func TestCustomersAreKeptToThePortal(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "deskowner")

	made := owner.post("/api/v1/projects", map[string]any{"name": "Helpdesk", "key": "HLP", "template": "service-desk"})
	if made.Status != http.StatusCreated {
		t.Fatalf("create desk: %d %s", made.Status, made.Raw)
	}

	invite := owner.post("/api/v1/invites", map[string]string{"email": h.email(t, "portal"), "role": "customer"})
	if invite.Status != http.StatusCreated {
		t.Fatalf("invite: %d %s", invite.Status, invite.Raw)
	}
	customer := api.client(t)
	accepted := customer.post("/api/v1/auth/invites/accept", map[string]string{
		"token": invite.Body["token"].(string), "name": "Sam Customer", "password": testPassword,
	})
	if accepted.Status != http.StatusOK {
		t.Fatalf("accept: %d %s", accepted.Status, accepted.Raw)
	}

	t.Run("the agent api refuses a customer", func(t *testing.T) {
		for _, path := range []string{"/api/v1/projects", "/api/v1/issues", "/api/v1/projects/HLP/queue"} {
			if r := customer.get(path); r.Status != http.StatusForbidden || r.ErrorCode() != "portal_only" {
				t.Errorf("GET %s as a customer: %d %s", path, r.Status, r.Raw)
			}
		}
	})

	desks := customer.get("/api/v1/portal/desks")
	if desks.Status != http.StatusOK {
		t.Fatalf("desks: %d %s", desks.Status, desks.Raw)
	}
	first := desks.Body["desks"].([]any)[0].(map[string]any)
	types := first["requestTypes"].([]any)
	if len(types) != 3 {
		t.Fatalf("the portal offers %d request types", len(types))
	}
	typeID := types[0].(map[string]any)["id"].(string)

	raised := customer.post("/api/v1/portal/requests", map[string]any{"requestTypeId": typeID, "summary": "Printer on fire", "description": "Literally."})
	if raised.Status != http.StatusCreated {
		t.Fatalf("raise: %d %s", raised.Status, raised.Raw)
	}
	key := raised.Body["request"].(map[string]any)["key"].(string)

	note := owner.post("/api/v1/issues/"+key+"/notes", map[string]any{"text": "call the fire brigade"})
	if note.Status != http.StatusCreated {
		t.Fatalf("note: %d %s", note.Status, note.Raw)
	}
	reply := owner.post("/api/v1/issues/"+key+"/comments", map[string]any{"text": "Help is on the way."})
	if reply.Status != http.StatusCreated {
		t.Fatalf("reply: %d %s", reply.Status, reply.Raw)
	}

	seen := customer.get("/api/v1/portal/requests/" + key)
	if seen.Status != http.StatusOK {
		t.Fatalf("request: %d %s", seen.Status, seen.Raw)
	}
	comments := seen.Body["comments"].([]any)
	if len(comments) != 1 {
		t.Errorf("the customer sees %d comments, want only the reply", len(comments))
	}
	if r := customer.get("/api/v1/issues/" + key + "/comments"); r.Status != http.StatusForbidden {
		t.Errorf("the customer read the agent comment list: %d", r.Status)
	}

	agentView := owner.get("/api/v1/issues/" + key + "/comments")
	if got := len(agentView.Body["comments"].([]any)); got != 2 {
		t.Errorf("the agent sees %d comments, want the note and the reply", got)
	}
	timers := owner.get("/api/v1/issues/" + key + "/timers")
	if got := len(timers.Body["timers"].([]any)); got != 2 {
		t.Errorf("timers over the api = %s", timers.Raw)
	}
	queue := owner.get("/api/v1/projects/HLP/queue?filter=open")
	if got := len(queue.Body["rows"].([]any)); got != 1 {
		t.Errorf("queue = %s", queue.Raw)
	}
}

// A request type is the desk's template: it names a category to be offered
// under, the details a requester starts from, and the team the request lands
// with. The team has to be the project's own, and the row refuses otherwise.
func TestARequestIsRoutedByItsTemplate(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "routing")
	d, p := ws.aDesk(t, h, "Routed desk")
	field, _, err := ws.teams.Create(ws.ctx, p.Key, team.CreateInput{Name: "Field support"}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, _, err := ws.teams.Create(ws.ctx, ws.project.Key, team.CreateInput{Name: "Other project's team"}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	laptop, _, err := d.CreateRequestType(ws.ctx, p.Key, desk.RequestTypeInput{
		Name: "Laptop replacement", Category: "Hardware", DetailsTemplate: "Device:\nWhat happened:\n\n", TeamID: &field.ID,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if laptop.Category != "Hardware" || laptop.TeamName != "Field support" || laptop.DetailsTemplate != "Device:\nWhat happened:" {
		t.Errorf("request type = %+v, want the category, the team and a tidied template", laptop)
	}

	customer := ws.customerOf(t, h, "routed")
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: laptop.ID, Summary: "Screen cracked"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	if raised.Team == nil || raised.Team.ID != field.ID {
		t.Errorf("the request landed with %+v, want Field support", raised.Team)
	}

	t.Run("a team of another project is refused with a sentence", func(t *testing.T) {
		_, _, err := d.CreateRequestType(ws.ctx, p.Key, desk.RequestTypeInput{Name: "Misrouted", TeamID: &elsewhere.ID}, ws.actor.UserID)
		if !errors.Is(err, desk.ErrTeamElsewhere) {
			t.Errorf("got %v, want the team refused", err)
		}
		_, _, err = d.UpdateRequestType(ws.ctx, laptop.ID, desk.RequestTypeUpdate{TeamID: &elsewhere.ID}, ws.actor.UserID)
		if !errors.Is(err, desk.ErrTeamElsewhere) {
			t.Errorf("update: got %v, want the team refused", err)
		}
	})

	t.Run("the database refuses the same row written through SQL", func(t *testing.T) {
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE request_type SET team_id = $2 WHERE id = $1`, laptop.ID, elsewhere.ID)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "another project") {
			t.Errorf("the trigger let the team through: %v", err)
		}
	})

	t.Run("an edit changes the offer and can take the team away", func(t *testing.T) {
		access := "Access"
		edited, _, err := d.UpdateRequestType(ws.ctx, laptop.ID, desk.RequestTypeUpdate{Category: &access, ClearTeam: true}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if edited.Category != "Access" || edited.TeamID != nil || edited.Name != "Laptop replacement" {
			t.Errorf("edited = %+v, want the category changed, the team gone and the rest kept", edited)
		}
		desks, err := d.Desks(ws.ctx, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		for _, desk := range desks {
			if desk.ProjectKey != p.Key {
				continue
			}
			offered := requestTypeNamed(t, desk.RequestTypes, "Laptop replacement")
			if offered.Category != "Access" || offered.DetailsTemplate != "Device:\nWhat happened:" {
				t.Errorf("the portal is offered %+v, want the category and the template", offered)
			}
		}
	})
}
