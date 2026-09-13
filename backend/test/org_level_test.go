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

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/calendar"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/privacy"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/version"
)

// The audit log takes a grant from the stream once however often it is
// handed over, takes a token from the act itself, keeps a year, and shows
// one organization nothing of another.
func TestTheAuditLogRecordsWhoDidWhat(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "audited")
	other := h.newWorkspace(t, "auditother")
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reader := audit.NewService(h.cluster)

	// A grant on the stream, handed over twice.
	member := h.joinExisting(t, ws, h.email(t, "audited-member"), "member")
	granted, _, err := perm.NewStore(h.cluster).Grant(ws.ctx, perm.GrantInput{Role: perm.ProjectAdministrator, ProjectKey: ws.project.Key, UserID: &member}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"assignmentId": granted.ID, "role": "project_administrator", "userId": member, "actorId": ws.actor.UserID})
	e := events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: "role.granted", Payload: payload}
	consumer := audit.NewConsumer(h.cluster, nil, log)
	for range 2 {
		if err := consumer.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := reader.List(ws.ctx, audit.Filter{Action: "role.granted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TargetID == nil || *rows[0].TargetID != granted.ID || rows[0].ActorName == "" {
		t.Fatalf("role.granted rows = %+v, want exactly one naming the grant and the actor", rows)
	}
	// A topic the log does not copy leaves nothing.
	noise, _ := json.Marshal(map[string]any{"key": ws.project.Key + "-1", "actorId": ws.actor.UserID})
	_ = consumer.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: noise})
	if got, _ := reader.List(ws.ctx, audit.Filter{Action: events.TopicIssueTransitioned}); len(got) != 0 {
		t.Errorf("an issue transition was copied into the audit log: %+v", got)
	}

	// A token is recorded by the act itself, with the actor.
	tok, _, err := h.authService().CreateAPIToken(ws.ctx, ws.actor.UserID, "ci", []string{"read"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.authService().RevokeAPIToken(ws.ctx, ws.actor.UserID, tok.ID); err != nil {
		t.Fatal(err)
	}
	tokens, _ := reader.List(ws.ctx, audit.Filter{ActorID: &ws.actor.UserID})
	var seen []string
	for _, r := range tokens {
		if strings.HasPrefix(r.Action, "token.") {
			seen = append(seen, r.Action)
		}
	}
	if strings.Join(seen, ",") != "token.revoked,token.created" {
		t.Errorf("token actions = %v, want revoked then created, newest first", seen)
	}
	actions, _ := reader.Actions(ws.ctx)
	if !contains(actions, "token.created") || !contains(actions, "role.granted") || !contains(actions, "org.created") {
		t.Errorf("actions = %v", actions)
	}

	// The other organization sees none of it, through the service and through SQL.
	if theirs, _ := reader.List(other.ctx, audit.Filter{}); len(theirs) == 0 || hasAction(theirs, "role.granted") {
		t.Errorf("the other organization's log = %+v", theirs)
	}
	csvOut, err := reader.CSV(ws.ctx, audit.Filter{Action: "role.granted"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(csvOut)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "time,action,actor") || !strings.Contains(lines[1], "role.granted") {
		t.Errorf("csv = %q", csvOut)
	}

	// Retention keeps a year and no more.
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO audit_log (org_id, actor_user_id, action, target_type, created_at)
		VALUES ($1, $2, 'ancient.thing', 'org', now() - interval '400 days')`, ws.orgID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if counts, err := privacy.NewRetention(h.cluster, privacy.DefaultPolicy(), log).Once(context.Background()); err != nil || counts["audit"] < 1 {
		t.Fatalf("retention removed %v rows, err %v", counts, err)
	}
	if old, _ := reader.List(ws.ctx, audit.Filter{Action: "ancient.thing"}); len(old) != 0 {
		t.Error("a row older than a year survived retention")
	}
	if fresh, _ := reader.List(ws.ctx, audit.Filter{Action: "role.granted"}); len(fresh) != 1 {
		t.Error("retention took a fresh row")
	}
	// A blank action is refused by the database, not only by the writer.
	if _, err := h.super.Exec(context.Background(), `INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, ' ', 'org')`, ws.orgID); err == nil {
		t.Error("the database accepted a blank audit action")
	}
}

func hasAction(rows []audit.Row, action string) bool {
	for _, r := range rows {
		if r.Action == action {
			return true
		}
	}
	return false
}

// An organization field is on every project's issues; a project may not coin
// a field by its name; promoting folds same-named fields and their answers
// into one, and refuses when the kinds differ.
func TestOrganizationFieldsAreSharedAndPromoted(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "orgfields")
	other := h.newWorkspace(t, "orgfieldsother")
	fields := ws.fields(h)
	second := ws.fromTemplate(t, h, "kanban", "Second")

	cost, _, err := fields.CreateOrg(ws.ctx, field.Input{Name: "Cost", Kind: field.Number})
	if err != nil {
		t.Fatal(err)
	}
	if !cost.Org || cost.ProjectID != nil {
		t.Fatalf("an organization field says %+v", cost)
	}
	for _, key := range []string{ws.project.Key, second.Key} {
		listed, _ := fields.Fields(ws.ctx, key)
		if len(listed) == 0 || listed[0].ID != cost.ID {
			t.Errorf("%s lists %+v, want the organization field first", key, listed)
		}
	}
	inSecond := ws.issueIn(t, second.Key, "Priced work")
	if _, _, err := fields.Set(ws.ctx, inSecond, cost.ID, json.RawMessage(`120`), ws.actor); err != nil {
		t.Fatalf("an organization field refused a value on another project's issue: %v", err)
	}
	values, _ := fields.Values(ws.ctx, inSecond)
	if len(values) == 0 || values[0].Field.ID != cost.ID || values[0].Display != "120" {
		t.Errorf("values = %+v", values)
	}

	// Its name is taken everywhere, by the service and by the database.
	if _, _, err := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: "cost", Kind: field.Text}); err == nil {
		t.Error("a project field shadowing an organization field was accepted by the service")
	}
	if _, err := h.super.Exec(context.Background(), `INSERT INTO custom_field (org_id, project_id, name, kind) VALUES ($1, $2, 'COST', 'text')`, ws.orgID, ws.project.ID); err == nil || !strings.Contains(err.Error(), "already has a field named") {
		t.Errorf("the database accepted a shadowing field: %v", err)
	}
	// Another organization's field fits none of these issues, whatever a
	// direct write says.
	theirs, _, err := fields.CreateOrg(other.ctx, field.Input{Name: "Theirs", Kind: field.Text})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO issue_field_value (org_id, issue_id, field_id, value)
		SELECT $1, i.id, $2, '"x"' FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $3 AND i.key_num = 1`,
		ws.orgID, theirs.ID, second.Key); err == nil || !strings.Contains(err.Error(), "not a field of the project") {
		t.Errorf("the database accepted another organization's field on an issue: %v", err)
	}

	// Two projects each have a Team size; promoting one folds the other in.
	mine, _, _ := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: "Team size", Kind: field.Number})
	twin, _, _ := fields.Create(ws.ctx, second.Key, field.Input{Name: "team size", Kind: field.Number})
	if mine == nil || twin == nil {
		t.Fatal("could not make the two fields")
	}
	if _, _, err := fields.Set(ws.ctx, inSecond, twin.ID, json.RawMessage(`4`), ws.actor); err != nil {
		t.Fatal(err)
	}
	promoted, _, err := fields.Promote(ws.ctx, mine.ID)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if !promoted.Org {
		t.Errorf("promoted = %+v", promoted)
	}
	if _, err := fields.Field(ws.ctx, twin.ID); err == nil {
		t.Error("the twin survived the promotion")
	}
	after, _ := fields.Values(ws.ctx, inSecond)
	var kept bool
	for _, v := range after {
		if v.Field.ID == mine.ID && v.Display == "4" {
			kept = true
		}
	}
	if !kept {
		t.Errorf("the twin's answer was lost in the fold: %+v", after)
	}

	// Different kinds do not fold.
	region, _, _ := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: "Region", Kind: field.Select, Options: []string{"EU", "US"}})
	fields.Create(ws.ctx, second.Key, field.Input{Name: "Region", Kind: field.Text})
	if _, _, err := fields.Promote(ws.ctx, region.ID); err == nil || !strings.Contains(err.Error(), "different kind") {
		t.Errorf("promoting over a field of another kind: %v", err)
	}
}

// A project's latest post is its status on the list, and a month gathers
// what is dated inside it, ranges crossing its edges included.
func TestStatusUpdatesAndTheCalendar(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "health")

	if _, _, err := ws.projects.PostUpdate(ws.ctx, ws.project.Key, project.StatusInput{Status: "fine"}, ws.actor.UserID); err == nil {
		t.Error("a status outside the three words was accepted")
	}
	if _, _, err := ws.projects.PostUpdate(ws.ctx, ws.project.Key, project.StatusInput{Status: project.OnTrack, Note: "All good."}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	target := "2026-12-24"
	posted, _, err := ws.projects.PostUpdate(ws.ctx, ws.project.Key, project.StatusInput{Status: project.AtRisk, Note: "The vendor is late.", TargetOn: &target}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if posted.TargetOn == nil || *posted.TargetOn != target || posted.AuthorName == "" {
		t.Errorf("posted = %+v", posted)
	}
	listed, _ := ws.projects.List(ws.ctx, false)
	var found *project.Project
	for i := range listed {
		if listed[i].Key == ws.project.Key {
			found = &listed[i]
		}
	}
	if found == nil || found.Status == nil || found.Status.Status != project.AtRisk || found.Status.Note != "The vendor is late." {
		t.Fatalf("the list says %+v, want the latest post", found)
	}
	updates, _ := ws.projects.Updates(ws.ctx, ws.project.Key)
	if len(updates) != 2 || updates[0].Status != project.AtRisk || updates[1].Status != project.OnTrack {
		t.Errorf("updates = %+v", updates)
	}
	if _, err := h.super.Exec(context.Background(), `INSERT INTO project_status_update (org_id, project_id, status) VALUES ($1, $2, 'fine')`, ws.orgID, ws.project.ID); err == nil {
		t.Error("the database accepted a status outside the three words")
	}

	// The calendar.
	on := func(s string) *time.Time { t, _ := time.Parse("2006-01-02", s); return &t }
	scheduled := ws.newIssue(t, "Spans the edge")
	start, due := on("2031-02-25"), on("2031-03-03")
	if _, _, err := ws.issues.Schedule(ws.ctx, scheduled.Key, issue.ScheduleInput{Start: &start, Due: &due}, ws.actor); err != nil {
		t.Fatal(err)
	}
	dueOnly := ws.newIssue(t, "Due in the month")
	only := on("2031-03-15")
	if _, _, err := ws.issues.Schedule(ws.ctx, dueOnly.Key, issue.ScheduleInput{Due: &only}, ws.actor); err != nil {
		t.Fatal(err)
	}
	ws.newIssue(t, "Undated")
	ws.planned(t, h, "March sprint", "2031-03-05", "2031-03-19", nil)
	if _, _, err := milestone.NewService(h.cluster).Create(ws.ctx, ws.project.Key, milestone.Input{Name: "Beta", DueOn: on("2031-03-31")}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	release := on("2031-04-01")
	if _, _, err := version.NewService(h.cluster).Create(ws.ctx, ws.project.Key, version.Input{Name: ptr("1.0"), StartOn: ptr(on("2031-03-20")), ReleaseOn: &release}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	month, err := calendar.NewService(h.cluster).Month(ws.ctx, ws.project.Key, 2031, 3)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]calendar.Item{}
	for _, it := range month.Items {
		byTitle[it.Title] = it
	}
	if it, ok := byTitle["Spans the edge"]; !ok || it.From != "2031-02-25" || it.To != "2031-03-03" || it.Kind != calendar.KindIssue {
		t.Errorf("the issue crossing into the month = %+v", it)
	}
	if it, ok := byTitle["Due in the month"]; !ok || it.From != "2031-03-15" || it.To != "2031-03-15" {
		t.Errorf("the issue with one date = %+v", it)
	}
	if _, ok := byTitle["Undated"]; ok {
		t.Error("an undated issue is on the calendar")
	}
	if it, ok := byTitle["March sprint"]; !ok || it.Kind != calendar.KindSprint || it.From != "2031-03-05" {
		t.Errorf("sprint = %+v", it)
	}
	if it, ok := byTitle["Beta"]; !ok || it.Kind != calendar.KindMilestone || it.To != "2031-03-31" {
		t.Errorf("milestone = %+v", it)
	}
	if it, ok := byTitle["1.0"]; !ok || it.Kind != calendar.KindVersion || it.To != "2031-04-01" {
		t.Errorf("version crossing out of the month = %+v", it)
	}
	if empty, _ := calendar.NewService(h.cluster).Month(ws.ctx, ws.project.Key, 2030, 6); len(empty.Items) != 0 {
		t.Errorf("an empty month holds %+v", empty.Items)
	}
	if _, err := calendar.NewService(h.cluster).Month(ws.ctx, ws.project.Key, 2031, 13); err == nil {
		t.Error("month 13 was accepted")
	}
}

// Every organization-level operation answers over the API, and refuses what
// it should.
func TestOrgLevelOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "orglevel")
	made := want(t, c.post("/api/v1/projects", map[string]any{"name": "Org level", "key": "OLX" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "project")
	key := obj(t, made, "project")["key"].(string)

	log := want(t, c.get("/api/v1/audit?limit=5"), http.StatusOK, "the audit log")
	if entries, _ := log.Body["entries"].([]any); len(entries) == 0 {
		t.Error("a fresh organization's log is empty; signing up should be in it")
	}
	want(t, c.get("/api/v1/audit?from=yesterday"), http.StatusBadRequest, "a date that is not one")
	want(t, c.get("/api/v1/audit?actor=nobody"), http.StatusBadRequest, "an actor that is not an id")
	export, body := c.download("/api/v1/audit/export?action=org.created")
	if export.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "time,action,actor") || !strings.Contains(string(body), "org.created") {
		t.Errorf("export = %d %q", export.StatusCode, body)
	}
	if r, _ := c.download("/api/v1/audit/export?to=never"); r.StatusCode != http.StatusBadRequest {
		t.Errorf("an export with a bad date answered %d", r.StatusCode)
	}

	want(t, c.post("/api/v1/fields", map[string]any{"name": "Cost", "kind": "money"}), http.StatusBadRequest, "a field of no kind")
	orgField := want(t, c.post("/api/v1/fields", map[string]any{"name": "Cost", "kind": "number"}), http.StatusCreated, "an organization field")
	want(t, c.get("/api/v1/fields"), http.StatusOK, "the organization's fields")
	want(t, c.post("/api/v1/projects/"+key+"/fields", map[string]any{"name": "cost", "kind": "text"}), http.StatusConflict, "a project field shadowing it")
	projectField := want(t, c.post("/api/v1/projects/"+key+"/fields", map[string]any{"name": "Team size", "kind": "number"}), http.StatusCreated, "a project field")
	want(t, c.post("/api/v1/fields/"+idOf(t, projectField, "field")+"/promote", nil), http.StatusOK, "promoting it")
	want(t, c.post("/api/v1/fields/"+uuid.New().String()+"/promote", nil), http.StatusNotFound, "promoting nothing")
	want(t, c.patch("/api/v1/fields/"+idOf(t, orgField, "field"), map[string]any{"name": "Price"}), http.StatusOK, "renaming an organization field")

	want(t, c.post("/api/v1/projects/"+key+"/status-updates", map[string]any{"status": "fine"}), http.StatusUnprocessableEntity, "a status outside the words")
	want(t, c.post("/api/v1/projects/"+key+"/status-updates", map[string]any{"status": "at_risk", "note": "Vendor late.", "targetOn": "2026-12-24"}), http.StatusCreated, "a status update")
	want(t, c.get("/api/v1/projects/"+key+"/status-updates"), http.StatusOK, "the history")
	listed := want(t, c.get("/api/v1/projects"), http.StatusOK, "projects")
	var carried bool
	for _, p := range listed.Body["projects"].([]any) {
		if pm := p.(map[string]any); pm["key"] == key {
			if st, ok := pm["status"].(map[string]any); ok && st["status"] == "at_risk" {
				carried = true
			}
		}
	}
	if !carried {
		t.Error("the projects list does not carry the latest status")
	}

	want(t, c.get("/api/v1/projects/"+key+"/calendar?month=2031-03"), http.StatusOK, "a month")
	want(t, c.get("/api/v1/projects/"+key+"/calendar"), http.StatusOK, "this month")
	want(t, c.get("/api/v1/projects/"+key+"/calendar?month=March"), http.StatusUnprocessableEntity, "a month that is not one")
}
