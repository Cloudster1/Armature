//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/notify"
	"github.com/armature/armature/backend/internal/perm"
)

// accessCrew is a project with one issue, its administrator, somebody who may
// read it, and somebody it is a wall to.
type accessCrew struct {
	key, issueKey        string
	owner, admin, reader *client
	asker                *client
	askerID, adminEmail  string
}

func newAccessCrew(t *testing.T, h *harness, api *apiServer, name string) accessCrew {
	t.Helper()
	c := accessCrew{key: h.projectKey(t, name)}
	c.owner = api.client(t)
	c.owner.signIn(t, h.person(t, name+"owner", auth.RoleOwner))
	want(t, c.owner.post("/api/v1/projects", map[string]any{"name": name, "key": c.key}), http.StatusCreated, "the project")
	made := obj(t, want(t, c.owner.post("/api/v1/projects/"+c.key+"/issues", map[string]any{"summary": "Hidden work"}), http.StatusCreated, "an issue"), "issue")
	c.issueKey = made["key"].(string)
	c.admin = api.asRole(t, h, c.owner, c.key, perm.ProjectAdministrator)
	c.adminEmail = obj(t, want(t, c.admin.get("/api/v1/auth/me"), http.StatusOK, "the administrator"), "principal", "user")["email"].(string)
	c.reader = api.asRole(t, h, c.owner, c.key, perm.Reader)
	email := h.outsider(t, name+"asker")
	if err := h.super.QueryRow(context.Background(), `SELECT id::text FROM app_user WHERE email = $1`, email).Scan(&c.askerID); err != nil {
		t.Fatal(err)
	}
	c.asker = api.client(t)
	c.asker.signIn(t, email)
	return c
}

func (c accessCrew) ask(key, message string) response {
	return c.asker.post("/api/v1/issues/"+key+"/access-requests", map[string]any{"message": message})
}

// answer is what a caller could compare between two answers: the status, the
// body and the headers, leaving out what differs on every request.
func answer(r response) string {
	var b strings.Builder
	b.WriteString(http.StatusText(r.Status) + "\n" + r.Raw + "\n")
	for _, name := range []string{"Content-Type", "Content-Length", "Cache-Control"} {
		b.WriteString(name + ": " + r.Header.Get(name) + "\n")
	}
	return b.String()
}

// fanOutAccess hands the asker's access events to the inbox's fan-out, as the
// worker would, each twice to show a repeat tells nobody twice.
func fanOutAccess(t *testing.T, h *harness, mailer *fakeMailer, askerID string) int {
	t.Helper()
	fan := notify.NewFanOut(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	rows, err := h.super.Query(context.Background(), `
		SELECT e.id, e.topic, e.payload FROM outbox_event e
		JOIN access_request r ON r.id::text = e.payload->>'requestId'
		WHERE e.topic = $1 AND r.requester_id = $2
		ORDER BY e.created_at, e.id`, events.TopicAccessRequested, askerID)
	if err != nil {
		t.Fatal(err)
	}
	var found []events.Event
	for rows.Next() {
		e := events.Event{}
		if err := rows.Scan(&e.ID, &e.Topic, &e.Payload); err != nil {
			t.Fatal(err)
		}
		found = append(found, e)
	}
	rows.Close()
	for _, e := range found {
		for range 2 {
			if err := fan.Handle(context.Background(), e); err != nil {
				t.Fatalf("fan out: %v", err)
			}
		}
	}
	return len(found)
}

// Asking for access answers alike whether the issue exists, may be seen or is
// hidden, and only a hidden one reaches the project's administrators (#279).
func TestAskingForAccessTellsTheAskerNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := newAccessCrew(t, h, api, "askaccess")

	hidden := c.ask(c.issueKey, "I review the payments work.")
	if hidden.Status != http.StatusAccepted {
		t.Fatalf("asking = %d %s, want 202", hidden.Status, hidden.Raw)
	}
	for what, other := range map[string]response{
		"a missing issue of the project":      c.ask(c.key+"-999", "I review the payments work."),
		"an issue of a project nobody made":   c.ask(h.projectKey(t, "NOPE")+"-1", "I review the payments work."),
		"an issue the caller may already see": c.reader.post("/api/v1/issues/"+c.issueKey+"/access-requests", map[string]any{"message": "I review the payments work."}),
		"the same issue asked again":          c.ask(c.issueKey, "Still waiting."),
	} {
		if answer(other) != answer(hidden) {
			t.Errorf("%s is answered\n%s\nbut a hidden issue\n%s", what, answer(other), answer(hidden))
		}
	}
	want(t, c.ask("not a key", ""), http.StatusUnprocessableEntity, "a key that is not one")
	want(t, c.ask(c.issueKey, strings.Repeat("a", 1001)), http.StatusUnprocessableEntity, "a message too long")

	var rows int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM access_request WHERE requester_id = $1`, c.askerID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("the asker has %d requests (%v), want the one for the hidden issue, the second folded into it", rows, err)
	}

	mailer := &fakeMailer{}
	if n := fanOutAccess(t, h, mailer, c.askerID); n != 1 {
		t.Fatalf("%d access events, want one", n)
	}
	h.waitForPrimary(t)
	inbox := list(t, want(t, c.admin.get("/api/v1/notifications"), http.StatusOK, "the administrator's inbox"), "notifications")
	var told []map[string]any
	for _, item := range inbox {
		if n := item.(map[string]any); n["kind"] == notify.KindAccessRequested {
			told = append(told, n)
		}
	}
	if len(told) != 1 || !strings.Contains(told[0]["title"].(string), c.issueKey) || !strings.Contains(told[0]["body"].(string), "I review the payments work.") {
		t.Fatalf("the administrator was told %+v, want one notice naming the key and the message", told)
	}
	link, _ := told[0]["link"].(string)
	if !strings.HasPrefix(link, "http://app.test/access-requests/") {
		t.Fatalf("the notice links to %q", link)
	}
	mailed := false
	for _, m := range mailer.sent {
		mailed = mailed || (m.To == c.adminEmail && strings.Contains(m.Body, link))
	}
	if !mailed {
		t.Errorf("the administrator was not mailed the link: %+v", mailer.sent)
	}
	for _, other := range []*client{c.asker, c.reader} {
		for _, item := range list(t, want(t, other.get("/api/v1/notifications"), http.StatusOK, "another inbox"), "notifications") {
			if item.(map[string]any)["kind"] == notify.KindAccessRequested {
				t.Errorf("somebody who does not administer the project was told: %v", item)
			}
		}
	}

	// Only somebody who administers the project reads or grants it.
	path := strings.TrimPrefix(link, "http://app.test")
	for who, other := range map[string]*client{"the asker": c.asker, "a reader": c.reader} {
		want(t, other.get("/api/v1"+path), http.StatusNotFound, who+" reading the request")
		want(t, other.post("/api/v1"+path+"/grant", nil), http.StatusNotFound, who+" granting it")
	}
	read := obj(t, want(t, c.admin.get("/api/v1"+path), http.StatusOK, "the administrator reads it"), "accessRequest")
	if read["issueKey"] != c.issueKey || read["grantedAt"] != nil {
		t.Errorf("the request = %v", read)
	}
	want(t, c.asker.get("/api/v1/issues/"+c.issueKey), http.StatusNotFound, "the issue before the grant")
	granted := obj(t, want(t, c.admin.post("/api/v1"+path+"/grant", nil), http.StatusOK, "granting it"), "accessRequest")
	if granted["grantedAt"] == nil {
		t.Errorf("the granted request = %v", granted)
	}
	want(t, c.admin.post("/api/v1"+path+"/grant", nil), http.StatusOK, "granting it twice")
	h.waitForPrimary(t)
	want(t, c.asker.get("/api/v1/issues/"+c.issueKey), http.StatusOK, "the issue after the grant")
}

// Asking is braked per account, so a script cannot probe keys through it.
func TestAskingForAccessIsBraked(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	asker := api.client(t)
	asker.signIn(t, h.outsider(t, "brakedasker"))
	missing := h.projectKey(t, "BRAKE")
	for i := range 20 {
		want(t, asker.post("/api/v1/issues/"+missing+"-1/access-requests", nil), http.StatusAccepted, "ask "+string(rune('a'+i)))
	}
	refused := want(t, asker.post("/api/v1/issues/"+missing+"-1/access-requests", nil), http.StatusTooManyRequests, "the 21st ask")
	if refused.ErrorCode() != "too_many_requests" {
		t.Errorf("the refusal = %s", refused.Raw)
	}
}

// A project may say that its issues exist to people who may not see them; one
// that has not keeps a hidden issue and a missing one alike.
func TestAProjectMayRevealThatItsIssuesExist(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := newAccessCrew(t, h, api, "revealing")
	missing := c.key + "-999"

	hidden, absent := c.asker.get("/api/v1/issues/"+c.issueKey), c.asker.get("/api/v1/issues/"+missing)
	if hidden.Status != http.StatusNotFound || answer(hidden) != answer(absent) {
		t.Fatalf("before opting in, a hidden issue is\n%s\nand a missing one\n%s", answer(hidden), answer(absent))
	}
	var reveals bool
	if err := h.super.QueryRow(context.Background(), `SELECT reveals_issues FROM project WHERE key = $1`, c.key).Scan(&reveals); err != nil || reveals {
		t.Fatalf("a new project reveals its issues: %v %v", reveals, err)
	}

	want(t, c.reader.patch("/api/v1/projects/"+c.key, map[string]any{"revealsIssues": true}), http.StatusForbidden, "a reader turning it on")
	turned := obj(t, want(t, c.admin.patch("/api/v1/projects/"+c.key, map[string]any{"revealsIssues": true}), http.StatusOK, "the administrator turning it on"), "project")
	if turned["revealsIssues"] != true {
		t.Fatalf("the project = %v", turned)
	}
	h.waitForPrimary(t)
	restricted := want(t, c.asker.get("/api/v1/issues/"+c.issueKey), http.StatusForbidden, "a hidden issue of a project that says so")
	if restricted.ErrorCode() != "not_permitted" || restricted.Error()["project"] != "revealing" {
		t.Errorf("the answer = %s, want not_permitted naming the project", restricted.Raw)
	}
	want(t, c.asker.get("/api/v1/issues/"+c.issueKey+"/comments"), http.StatusForbidden, "its comments")
	want(t, c.asker.get("/api/v1/issues/"+missing), http.StatusNotFound, "a missing issue of it")
}

// The database keeps a note to the administrators short whatever writes it.
func TestTheDatabaseKeepsAnAccessRequestShort(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := newAccessCrew(t, h, api, "shortnote")
	_, err := h.super.Exec(context.Background(), `
		INSERT INTO access_request (project_id, requester_id, issue_key, message)
		SELECT id, $2, $1 || '-1', repeat('a', 1001) FROM project WHERE key = $1`, c.key, c.askerID)
	if err == nil || !strings.Contains(err.Error(), "access_request_message_short") {
		t.Errorf("a long message was taken: %v", err)
	}
}
