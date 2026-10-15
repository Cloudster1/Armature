//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/issue"
)

// The portal's answers, key by key: a request, its people, its public
// conversation, its files and its followers, as a customer reads them.

var (
	portalRequestMust = []string{"key", "summary", "description", "status", "priority", "requestTypeName", "company", "sharedWithCompany", "assignee", "reporter", "createdAt", "updatedAt", "commentCount"}
	portalRequestMay  = []string{"team", "resolvedAt"}
	portalPersonMust  = []string{"id", "name"}
	portalPersonMay   = []string{"avatarUrl"}
	portalCommentMust = []string{"id", "author", "body", "createdAt"}
	portalFileMust    = []string{"id", "uploader", "fileName", "contentType", "size", "createdAt"}
	portalFollowMust  = []string{"userId", "name"}
	portalFollowMay   = []string{"email"}
)

// shapeOf checks that an answer carries every key it must and none other than
// those it may, and hands it back as an object.
func shapeOf(t *testing.T, what string, v any, must, may []string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want an object", what, v)
	}
	allowed := map[string]bool{}
	for _, k := range append(append([]string{}, must...), may...) {
		allowed[k] = true
	}
	var extra []string
	for k := range m {
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("%s carries %v, which the portal does not answer with", what, extra)
	}
	for _, k := range must {
		if _, ok := m[k]; !ok {
			t.Errorf("%s has no %q", what, k)
		}
	}
	return m
}

func portalPerson(t *testing.T, what string, v any) {
	t.Helper()
	shapeOf(t, what, v, portalPersonMust, portalPersonMay)
}

func portalRequestShape(t *testing.T, what string, v any) map[string]any {
	t.Helper()
	r := shapeOf(t, what, v, portalRequestMust, portalRequestMay)
	shapeOf(t, what+"'s status", r["status"], []string{"name", "category"}, nil)
	shapeOf(t, what+"'s company", r["company"], []string{"id", "name"}, nil)
	portalPerson(t, what+"'s assignee", r["assignee"])
	portalPerson(t, what+"'s reporter", r["reporter"])
	if team, ok := r["team"]; ok {
		shapeOf(t, what+"'s team", team, []string{"id", "name"}, nil)
	}
	return r
}

func portalCommentShape(t *testing.T, what string, v any) {
	t.Helper()
	c := shapeOf(t, what, v, portalCommentMust, nil)
	portalPerson(t, what+"'s author", c["author"])
}

func portalFileShape(t *testing.T, what string, v any) {
	t.Helper()
	f := shapeOf(t, what, v, portalFileMust, nil)
	portalPerson(t, what+"'s uploader", f["uploader"])
}

// anyKeyNamed says whether a key by that name sits anywhere in a decoded body.
func anyKeyNamed(v any, key string) bool {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			if k == key || anyKeyNamed(inner, key) {
				return true
			}
		}
	case []any:
		for _, inner := range v {
			if anyKeyNamed(inner, key) {
				return true
			}
		}
	}
	return false
}

// Every portal answer, from the reporter's side and a colleague's, carries
// what a customer reads and no field beyond it.
func TestPortalAnswersCarryWhatACustomerReads(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	key := h.projectKey(t, "PSH")
	api := newAPIServer(t, h)
	owner := api.client(t)
	ownerEmail := h.person(t, "deskagent", auth.RoleOwner)
	ownerID := principalField(t, owner.signIn(t, ownerEmail), "principal", "user", "id").(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Help", "key": key, "template": "service-desk"}), http.StatusCreated, "desk")
	companyID := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/companies", map[string]any{"name": "Globex"}), http.StatusCreated, "a company"), "company")

	reporterEmail := h.person(t, "reporter", auth.RoleCustomer)
	colleagueEmail := h.person(t, "colleague", auth.RoleCustomer)
	friendEmail := h.person(t, "friend", auth.RoleCustomer)
	for _, email := range []string{reporterEmail, colleagueEmail} {
		want(t, owner.post("/api/v1/companies/"+companyID+"/members", map[string]any{"email": email}), http.StatusCreated, "putting a customer in")
	}
	addresses := []string{ownerEmail, reporterEmail, colleagueEmail, friendEmail}
	h.waitForPrimary(t)

	reporter := api.client(t)
	reporter.signIn(t, reporterEmail)
	var typeID any
	for _, raw := range list(t, want(t, reporter.get("/api/v1/portal/desks"), http.StatusOK, "the desks"), "desks") {
		if d := raw.(map[string]any); d["projectKey"] == key {
			typeID = d["requestTypes"].([]any)[0].(map[string]any)["id"]
		}
	}
	raised := want(t, reporter.post("/api/v1/portal/requests", map[string]any{"requestTypeId": typeID, "summary": "The VPN is down", "description": "Since nine.", "shareWithCompany": true}), http.StatusCreated, "raise")
	shapeOf(t, "the raise answer", raised.Body, []string{"request"}, nil)
	made := shapeOf(t, "a raised request", raised.Body["request"], []string{"key", "summary", "status", "priority", "reporter", "company", "sharedWithCompany", "createdAt", "updatedAt", "commentCount"}, append(append([]string{}, portalRequestMust...), portalRequestMay...))
	portalPerson(t, "a raised request's reporter", made["reporter"])
	requestKey := made["key"].(string)
	h.waitForPrimary(t)

	// The desk answers, notes, attaches and watches; the reporter adds a
	// follower from outside the company.
	want(t, owner.patch("/api/v1/issues/"+requestKey, map[string]any{"assigneeId": ownerID}), http.StatusOK, "assign it")
	want(t, owner.post("/api/v1/issues/"+requestKey+"/comments", map[string]any{"text": "We are on it."}), http.StatusCreated, "a reply from the desk")
	want(t, owner.post("/api/v1/issues/"+requestKey+"/notes", map[string]any{"text": "their gateway again"}), http.StatusCreated, "a note")
	want(t, owner.upload("/api/v1/issues/"+requestKey+"/attachments", "file", "desk.txt", "text/plain", []byte("from the desk")), http.StatusCreated, "the desk's file")
	want(t, owner.post("/api/v1/issues/"+requestKey+"/watchers", map[string]any{}), http.StatusCreated, "the agent watches")
	followed := want(t, reporter.post("/api/v1/portal/requests/"+requestKey+"/watchers", map[string]any{"email": friendEmail}), http.StatusCreated, "follow")
	if added := shapeOf(t, "an added follower", obj(t, followed, "watcher"), portalFollowMust, portalFollowMay); added["email"] != friendEmail {
		t.Errorf("the follower the reporter added = %v, want them by the address typed", added)
	}
	h.waitForPrimary(t)

	friend := api.client(t)
	friend.signIn(t, friendEmail)
	replied := want(t, friend.post("/api/v1/portal/requests/"+requestKey+"/replies", map[string]any{"text": "Same for me."}), http.StatusCreated, "the follower replies")
	shapeOf(t, "the reply answer", replied.Body, []string{"comment"}, nil)
	portalCommentShape(t, "a reply", replied.Body["comment"])
	uploaded := want(t, friend.upload("/api/v1/portal/requests/"+requestKey+"/attachments", "file", "trace.txt", "text/plain", []byte("timeout")), http.StatusCreated, "the follower attaches")
	shapeOf(t, "the upload answer", uploaded.Body, []string{"attachment"}, nil)
	portalFileShape(t, "an uploaded file", uploaded.Body["attachment"])
	h.waitForPrimary(t)

	t.Run("the reporter's lists", func(t *testing.T) {
		listed := want(t, reporter.get("/api/v1/portal/requests"), http.StatusOK, "the lists")
		shapeOf(t, "the lists", listed.Body, []string{"requests", "shared"}, nil)
		mine := list(t, listed, "requests")
		if len(mine) != 1 {
			t.Fatalf("the reporter's requests = %v, want the one", mine)
		}
		if r := portalRequestShape(t, "a listed request", mine[0]); r["commentCount"] != float64(2) {
			t.Errorf("the listed request counts %v comments, want the two public ones", r["commentCount"])
		}
	})

	t.Run("the reporter's read", func(t *testing.T) {
		read := want(t, reporter.get("/api/v1/portal/requests/"+requestKey), http.StatusOK, "the read")
		shapeOf(t, "a read", read.Body, []string{"request", "comments"}, []string{"throughCompany"})
		if r := portalRequestShape(t, "a read request", read.Body["request"]); r["commentCount"] != float64(2) {
			t.Errorf("the read counts %v comments, want the two public ones", r["commentCount"])
		}
		comments := list(t, read, "comments")
		if len(comments) != 2 {
			t.Fatalf("the reporter reads %d comments, want the desk's reply and the follower's", len(comments))
		}
		for _, c := range comments {
			portalCommentShape(t, "a comment", c)
		}
	})

	t.Run("the files", func(t *testing.T) {
		files := list(t, want(t, reporter.get("/api/v1/portal/requests/"+requestKey+"/attachments"), http.StatusOK, "the files"), "attachments")
		if len(files) != 2 {
			t.Fatalf("files = %v, want the desk's and the follower's", files)
		}
		for _, f := range files {
			portalFileShape(t, "a file", f)
		}
	})

	t.Run("followers the reporter added by address, the desk by name", func(t *testing.T) {
		followers := list(t, want(t, reporter.get("/api/v1/portal/requests/"+requestKey+"/watchers"), http.StatusOK, "followers"), "watchers")
		if len(followers) != 2 {
			t.Fatalf("followers = %v, want the friend and the agent", followers)
		}
		for _, raw := range followers {
			f := shapeOf(t, "a follower", raw, portalFollowMust, portalFollowMay)
			switch f["userId"] {
			case ownerID:
				if _, ok := f["email"]; ok {
					t.Errorf("the agent following is shown as %v, want by name", f)
				}
			default:
				if f["email"] != friendEmail {
					t.Errorf("the follower the reporter added is shown as %v, want their address", f)
				}
			}
		}
	})

	t.Run("a colleague's reads name people by id, name and avatar", func(t *testing.T) {
		colleague := api.client(t)
		colleague.signIn(t, colleagueEmail)
		listed := want(t, colleague.get("/api/v1/portal/requests"), http.StatusOK, "the colleague's lists")
		shared := list(t, listed, "shared")
		if len(shared) != 1 {
			t.Fatalf("shared with the colleague = %v, want the one", shared)
		}
		portalRequestShape(t, "a shared request", shared[0])
		read := want(t, colleague.get("/api/v1/portal/requests/"+requestKey), http.StatusOK, "the colleague's read")
		portalRequestShape(t, "the colleague's read", read.Body["request"])
		files := want(t, colleague.get("/api/v1/portal/requests/"+requestKey+"/attachments"), http.StatusOK, "the colleague's files")
		for what, r := range map[string]response{"the lists": listed, "the read": read, "the files": files} {
			if anyKeyNamed(r.Body, "email") {
				t.Errorf("%s carry an address field: %s", what, r.Raw)
			}
			for _, address := range addresses {
				if strings.Contains(r.Raw, address) {
					t.Errorf("%s name %s by address", what, address)
				}
			}
		}
	})
}

// A colleague reading what was shared, after a follower from outside the
// company replied, is told who is on it by id, name and avatar.
func TestAColleaguesReadNamesPeopleByIDNameAndAvatar(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "colleaguesread")
	d, p := ws.aDesk(t, h, "Support")
	reporter := ws.customerOf(t, h, "reporter")
	colleague := ws.customerOf(t, h, "colleague")
	outside := ws.customerOf(t, h, "outside")
	aCompany(t, ws, d, p.Key, "Globex", reporter, colleague)
	shared := raiseAt(t, ws, d, p.Key, "The VPN is down", true, reporter)

	if _, _, _, err := ws.issues.AddWatcher(ws.ctx, shared.Key, outside.UserID, reporter); err != nil {
		t.Fatal(err)
	}
	agent := &ws.actor.UserID
	if _, _, err := ws.issues.Update(ws.ctx, shared.Key, issue.UpdateInput{Assignee: &agent}, ws.actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.issues.AddComment(ws.ctx, shared.Key, issue.TextDocument("We are looking."), ws.actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.issues.AddNote(ws.ctx, shared.Key, issue.TextDocument("their gateway again"), ws.actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Reply(ws.ctx, shared.Key, "Mine too.", outside); err != nil {
		t.Fatalf("the follower's reply: %v", err)
	}
	read, err := d.Request(ws.ctx, shared.Key, colleague.UserID)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := d.SharedRequests(ws.ctx, colleague.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for what, answer := range map[string]any{"the read": read, "the shared list": listed} {
		raw, err := json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		var body any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if anyKeyNamed(body, "email") || strings.Contains(string(raw), "@") {
			t.Errorf("%s = %s, want people by id, name and avatar", what, raw)
		}
	}
	if len(read.Comments) != 2 {
		t.Errorf("the colleague reads %d comments, want the desk's reply and the follower's", len(read.Comments))
	}
}
