//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/tenant"
)

// Articles are offered before a request is raised, published ones only, and
// only on a service desk; canned responses fill their placeholders.
func TestArticlesAndCannedResponses(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "knowledge")
	d, p := ws.aDesk(t, h, "Know desk")

	draft, _, err := d.CreateArticle(ws.ctx, p.Key, desk.ArticleInput{Title: ptr("Resetting your password"), Body: ptr("Open Settings, then Password, then follow the mail.")}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if found, _ := d.SearchArticles(ws.ctx, p.Key, "password"); len(found) != 0 {
		t.Errorf("a draft was offered to customers: %+v", found)
	}
	if _, _, err := d.UpdateArticle(ws.ctx, draft.ID, desk.ArticleInput{Published: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	found, _ := d.SearchArticles(ws.ctx, p.Key, "password")
	if len(found) != 1 || found[0].Title != "Resetting your password" {
		t.Fatalf("search = %+v, want the published article", found)
	}
	if miss, _ := d.SearchArticles(ws.ctx, p.Key, "printer"); len(miss) != 0 {
		t.Errorf("an unrelated search found %+v", miss)
	}
	// Not on a software project, by the service and by the database.
	if _, _, err := d.CreateArticle(ws.ctx, ws.project.Key, desk.ArticleInput{Title: ptr("Nope")}, ws.actor.UserID); err == nil {
		t.Error("an article on a software project was accepted by the service")
	}
	if _, err := h.super.Exec(context.Background(), `INSERT INTO kb_article (org_id, project_id, title) VALUES ($1, $2, 'x')`, ws.orgID, ws.project.ID); err == nil || !strings.Contains(err.Error(), "service desk") {
		t.Errorf("the database accepted an article on a software project: %v", err)
	}

	canned, _, err := d.CreateCanned(ws.ctx, p.Key, desk.CannedInput{Name: ptr("Greeting"), Body: ptr("Hello {{customer.name}}, about {{issue.key}} ({{issue.summary}}): {{agent.name}} here. {{unknown}} stays.")})
	if err != nil {
		t.Fatal(err)
	}
	customer := ws.customerOf(t, h, "cannedcust")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the invoice?"}, customer)
	text, err := d.RenderCanned(ws.ctx, canned.ID, raised.Key, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "about "+raised.Key+" (Where is the invoice?)") || !strings.Contains(text, "{{unknown}} stays") || strings.Contains(text, "{{customer.name}}") {
		t.Errorf("rendered = %q", text)
	}
}

// A resolved request invites one rating through the mail; a second answer is
// refused, and the queue and the report show the score.
func TestARequestIsRatedOnce(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "ratings")
	d, p := ws.aDesk(t, h, "Rate desk")
	customer := ws.customerOf(t, h, "rater")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Rate me"}, customer)
	ws.move(t, h, raised.Key, "Resolve")

	mailer := &fakeMailer{}
	notifier := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	raw, _ := json.Marshal(map[string]any{"key": raised.Key, "transition": "Resolve", "fromStatus": "Waiting for support", "toStatus": "Resolved"})
	if err := notifier.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 1 || !strings.Contains(mailer.sent[0].Body, "http://app.test/rate/") {
		t.Fatalf("resolution mail = %+v, want the rating link", mailer.sent)
	}
	link := mailer.sent[0].Body[strings.Index(mailer.sent[0].Body, "http://app.test/rate/")+len("http://app.test/rate/"):]
	token := strings.Fields(link)[0]

	page, err := d.RatingPage(context.Background(), token)
	if err != nil || page.IssueKey != raised.Key || page.Rated {
		t.Fatalf("rating page = %+v %v", page, err)
	}
	if _, _, err := d.Rate(context.Background(), token, 7, ""); err == nil {
		t.Error("a score of 7 was accepted")
	}
	rating, _, err := d.Rate(context.Background(), token, 4, "Quick, thanks")
	if err != nil || rating.Score == nil || *rating.Score != 4 {
		t.Fatalf("rate = %+v %v", rating, err)
	}
	if _, _, err := d.Rate(context.Background(), token, 5, ""); err == nil {
		t.Error("a second rating was accepted")
	}
	if _, err := d.RatingPage(context.Background(), "not-a-token"); err == nil {
		t.Error("a made-up token answered")
	}
	// A second resolution mails no second invitation.
	if err := notifier.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mailer.sent[1].Body, "/rate/") {
		t.Error("a second resolution mail carried a new rating link")
	}

	rows, _ := d.Queue(ws.ctx, p.Key, desk.QueueAll, ws.actor.UserID)
	var scored *int
	for _, r := range rows {
		if r.Issue.Key == raised.Key {
			scored = r.Csat
		}
	}
	if scored == nil || *scored != 4 {
		t.Errorf("the queue does not show the score: %v", scored)
	}
	got, err := ws.reports(h).Report(ws.ctx, p.Key, report.CSAT, report.Params{Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	cs := got.(*report.CSATReport)
	if cs.Invited != 1 || cs.Rated != 1 || cs.Average != 4 || cs.Scores[3] != 1 || cs.ResponseRate != 1 {
		t.Errorf("csat report = %+v", cs)
	}
}

// A goal that counts business hours does not run over the weekend: the watch
// finds the suspect by wall time and the clock has the last word.
func TestAGoalThatCountsBusinessHoursWaitsForMonday(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "hours")
	d, p := ws.aDesk(t, h, "Hours desk")
	cal := desk.DefaultCalendar()
	if _, _, err := d.SaveCalendar(ws.ctx, p.Key, cal); err != nil {
		t.Fatal(err)
	}
	policies, _ := d.Policies(ws.ctx, p.Key)
	var first *desk.Policy
	for i := range policies {
		if policies[i].Metric == "first_response" {
			first = &policies[i]
		}
	}
	if first == nil {
		t.Fatal("the desk template has no first response policy")
	}
	if _, _, err := d.UpdatePolicy(ws.ctx, first.ID, desk.PolicyInput{Goals: map[issue.Priority]int{issue.PriorityLowest: 120, issue.PriorityLow: 120, issue.PriorityMedium: 120, issue.PriorityHigh: 120, issue.PriorityHighest: 120}, UseCalendar: ptr(true)}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	// Turning the calendar on without one set is refused.
	if _, _, err := d.UpdatePolicy(ws.ctx, first.ID, desk.PolicyInput{UseCalendar: ptr(true)}, ws.actor.UserID); err != nil {
		t.Fatalf("a policy on a project with hours refused the calendar: %v", err)
	}
	_, other := ws.aDesk(t, h, "Nohours")
	otherPolicies, _ := d.Policies(ws.ctx, other.Key)
	if _, _, err := d.UpdatePolicy(ws.ctx, otherPolicies[0].ID, desk.PolicyInput{UseCalendar: ptr(true)}, ws.actor.UserID); err == nil {
		t.Error("a policy on a project without hours took the calendar")
	}

	customer := ws.customerOf(t, h, "weekend")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Filed on Friday"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	// The request was raised Friday 16:00; the clock is moved by hand.
	friday := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	if _, err := h.super.Exec(context.Background(), `UPDATE sla_timer SET started_at = $2, running_since = $2 WHERE issue_id = $1`, raised.ID, friday); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 9, 30, 0, 0, time.UTC) // Monday 09:30: 1h30 of open time used
	d.WithClock(func() time.Time { return now })

	timers, _ := d.TimersFor(ws.ctx, raised.Key)
	var response *desk.Timer
	for i := range timers {
		if timers[i].Metric == "first_response" {
			response = &timers[i]
		}
	}
	if response == nil || !response.BusinessHours || response.RemainingSeconds != 30*60 || response.Breached {
		t.Fatalf("timer on Monday morning = %+v, want 30 minutes left and no breach", response)
	}

	watch := desk.NewWatch(h.cluster, d, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := watch.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	timers, _ = d.TimersFor(ws.ctx, raised.Key)
	for _, tm := range timers {
		if tm.Metric == "first_response" && tm.BreachedAt != nil {
			t.Fatal("the watch recorded a breach the calendar says has not happened")
		}
	}

	now = time.Date(2026, 9, 7, 10, 30, 0, 0, time.UTC) // Monday 10:30: 2h30 used
	if _, err := watch.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	timers, _ = d.TimersFor(ws.ctx, raised.Key)
	breached := false
	for _, tm := range timers {
		if tm.Metric == "first_response" && tm.BreachedAt != nil {
			breached = true
		}
	}
	if !breached {
		t.Error("by Monday afternoon the goal was missed and nothing was recorded")
	}
}

// Every operation over the API, both ways.
func TestDeskExtrasOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	signedUp := c.signup(t, h, "deskextras")
	orgID := uuid.MustParse(obj(t, signedUp, "principal", "org")["id"].(string))
	made := want(t, c.post("/api/v1/projects", map[string]any{"name": "Help", "key": "HLX" + strings.ToUpper(uuid.New().String()[:3]), "template": "service-desk"}), http.StatusCreated, "desk")
	key := obj(t, made, "project")["key"].(string)
	soft := want(t, c.post("/api/v1/projects", map[string]any{"name": "Soft", "key": "SFX" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "software project")
	softKey := obj(t, soft, "project")["key"].(string)

	want(t, c.post("/api/v1/projects/"+key+"/articles", map[string]any{"title": ""}), http.StatusUnprocessableEntity, "a nameless article")
	want(t, c.post("/api/v1/projects/"+softKey+"/articles", map[string]any{"title": "Nope"}), http.StatusConflict, "an article on a software project")
	article := want(t, c.post("/api/v1/projects/"+key+"/articles", map[string]any{"title": "Resetting your password", "body": "Open Settings.", "published": true}), http.StatusCreated, "an article")
	articleID := idOf(t, article, "article")
	want(t, c.get("/api/v1/projects/"+key+"/articles"), http.StatusOK, "articles")
	want(t, c.patch("/api/v1/articles/"+articleID, map[string]any{"body": "Open Settings, then Password."}), http.StatusOK, "editing")
	want(t, c.get("/api/v1/portal/desks/"+key+"/articles?q=password"), http.StatusOK, "a customer's search")
	want(t, c.get("/api/v1/portal/articles/"+articleID), http.StatusOK, "one article")
	want(t, c.get("/api/v1/portal/articles/"+uuid.New().String()), http.StatusNotFound, "an article that is not here")

	want(t, c.post("/api/v1/projects/"+key+"/canned-responses", map[string]any{"name": "Greeting"}), http.StatusUnprocessableEntity, "a reply with no words")
	canned := want(t, c.post("/api/v1/projects/"+key+"/canned-responses", map[string]any{"name": "Greeting", "body": "Hello {{customer.name}}"}), http.StatusCreated, "a canned response")
	cannedID := idOf(t, canned, "response")
	want(t, c.get("/api/v1/projects/"+key+"/canned-responses"), http.StatusOK, "canned responses")
	want(t, c.patch("/api/v1/canned-responses/"+cannedID, map[string]any{"body": "Hi {{customer.name}}"}), http.StatusOK, "editing a reply")
	issueMade := want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Printer on fire"}), http.StatusCreated, "issue")
	issueKey := obj(t, issueMade, "issue")["key"].(string)
	want(t, c.post("/api/v1/canned-responses/"+cannedID+"/render", map[string]any{}), http.StatusBadRequest, "rendering for nothing")
	want(t, c.post("/api/v1/canned-responses/"+cannedID+"/render", map[string]any{"issueKey": issueKey}), http.StatusOK, "rendering")
	want(t, c.get("/api/v1/issues/"+issueKey+"/csat"), http.StatusNotFound, "no rating yet")

	want(t, c.get("/api/v1/projects/"+key+"/business-calendar"), http.StatusConflict, "no hours yet")
	want(t, c.put("/api/v1/projects/"+key+"/business-calendar", map[string]any{"timezone": "Mars/Olympus", "hours": map[string]any{}}), http.StatusUnprocessableEntity, "a bad zone")
	want(t, c.put("/api/v1/projects/"+key+"/business-calendar", map[string]any{"timezone": "Europe/Berlin", "hours": map[string]any{"mon": []map[string]string{{"from": "09:00", "to": "17:00"}}}, "holidays": []string{"2026-12-25"}}), http.StatusOK, "setting hours")
	want(t, c.get("/api/v1/projects/"+key+"/business-calendar"), http.StatusOK, "the hours")
	policies := list(t, want(t, c.get("/api/v1/projects/"+key+"/sla-policies"), http.StatusOK, "policies"), "policies")
	policyID := policies[0].(map[string]any)["id"].(string)
	want(t, c.patch("/api/v1/sla-policies/"+policyID, map[string]any{"useCalendar": true}), http.StatusOK, "counting business hours")
	want(t, c.get("/api/v1/projects/"+key+"/reports/csat"), http.StatusOK, "the satisfaction report")

	anonymous := api.client(t)
	want(t, anonymous.get("/api/v1/csat/not-a-token"), http.StatusNotFound, "a made-up rating link")
	want(t, anonymous.post("/api/v1/csat/not-a-token", map[string]any{"score": 5}), http.StatusNotFound, "rating with a made-up token")
	// A real token: mint it through the notifier the way the mail does.
	var issueID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key || '-' || i.key_num = $1`, issueKey).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	token, err := api.api.Desk.InviteRating(db.PinPrimary(tenant.WithOrg(context.Background(), tenant.Org{ID: orgID})), issueID)
	if err != nil || token == "" {
		t.Fatalf("mint a rating token: %q %v", token, err)
	}
	want(t, anonymous.get("/api/v1/csat/"+token), http.StatusOK, "the rating page")
	want(t, anonymous.post("/api/v1/csat/"+token, map[string]any{"score": 5, "comment": "Great"}), http.StatusOK, "rating")
	want(t, anonymous.post("/api/v1/csat/"+token, map[string]any{"score": 1}), http.StatusConflict, "rating twice")
	want(t, c.get("/api/v1/issues/"+issueKey+"/csat"), http.StatusOK, "the rating on the request")

	want(t, c.delete("/api/v1/canned-responses/"+cannedID), http.StatusNoContent, "deleting a reply")
	want(t, c.delete("/api/v1/canned-responses/"+cannedID), http.StatusNotFound, "deleting it twice")
	want(t, c.delete("/api/v1/articles/"+articleID), http.StatusNoContent, "deleting an article")
	want(t, c.delete("/api/v1/articles/"+articleID), http.StatusNotFound, "deleting it twice")
}
