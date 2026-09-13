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
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
)

// A follower is told about a request, and a mail address is enough to be one.
// The reporter names them, the mail carries a way out, and whoever follows
// sees the request in their own portal.
func TestAFollowerIsToldAndCanStop(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "following")
	d, p := ws.aDesk(t, h, "Follow desk")
	customer := ws.customerOf(t, h, "reporter")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the invoice?"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	benAddress := h.email(t, "ben")
	ben, err := h.authService().UserForAddress(context.Background(), benAddress)
	if err != nil {
		t.Fatal(err)
	}
	var passwordless bool
	if err := h.super.QueryRow(context.Background(), `SELECT password_hash IS NULL FROM app_user WHERE id = $1`, ben.ID).Scan(&passwordless); err != nil {
		t.Fatal(err)
	}
	if !passwordless || ben.Name != strings.Split(benAddress, "@")[0] {
		t.Errorf("ben = %+v (passwordless %v), want a person made from the address alone", ben, passwordless)
	}

	added, token, _, err := ws.issues.AddWatcher(ws.ctx, raised.Key, ben.ID, customer)
	if err != nil {
		t.Fatal(err)
	}
	if added.Email != benAddress || token == "" {
		t.Fatalf("added = %+v, token %q, want ben with a way out", added, token)
	}
	if again, second, _, err := ws.issues.AddWatcher(ws.ctx, raised.Key, ben.ID, customer); err != nil || second != "" || again.UserID != ben.ID {
		t.Errorf("adding twice: %+v %q %v, want the same watcher and no new token", again, second, err)
	}

	mailer := &fakeMailer{}
	notifier := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	handle := func(topic string, payload map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if err := notifier.Handle(context.Background(), events.Event{OrgID: ws.orgID, Topic: topic, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	recipients := func() []string {
		var out []string
		for _, m := range mailer.sent {
			out = append(out, m.To)
		}
		return out
	}

	t.Run("being added is mailed, with the way out", func(t *testing.T) {
		handle(events.TopicWatcherAdded, map[string]any{"key": raised.Key, "userId": ben.ID, "actorId": customer.UserID, "token": token})
		if len(mailer.sent) != 1 || mailer.sent[0].To != benAddress {
			t.Fatalf("mails = %+v, want one to ben", mailer.sent)
		}
		if body := mailer.sent[0].Body; !strings.Contains(body, "http://app.test/unwatch?token="+token) || !strings.Contains(body, raised.Key) {
			t.Errorf("body = %q, want the key and the stop link", body)
		}
		mailer.sent = nil
	})

	t.Run("a reply and an assignment reach the reporter and the follower, not the actor", func(t *testing.T) {
		reply, _, err := ws.issues.AddComment(ws.ctx, raised.Key, issue.TextDocument("It is under Billing."), ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		handle(events.TopicCommentAdded, map[string]any{"key": raised.Key, "commentId": reply.ID, "internal": false, "actorId": ws.actor.UserID})
		if got := recipients(); len(got) != 2 || !contains(got, benAddress) {
			t.Errorf("reply went to %v, want the reporter and ben", got)
		}
		mailer.sent = nil
		handle(events.TopicIssueUpdated, map[string]any{"key": raised.Key, "actorId": ws.actor.UserID,
			"changes": []map[string]string{{"field": "assignee", "from": "", "to": "Grace Hopper"}}})
		if got := recipients(); len(got) != 2 || !strings.Contains(mailer.sent[0].Body, "Grace Hopper") {
			t.Errorf("assignment went to %v with %q, want both told who is handling it", got, mailer.sent[0].Body)
		}
		mailer.sent = nil
		handle(events.TopicIssueUpdated, map[string]any{"key": raised.Key, "actorId": ws.actor.UserID,
			"changes": []map[string]string{{"field": "priority", "from": "low", "to": "high"}}})
		if len(mailer.sent) != 0 {
			t.Errorf("a priority change was mailed: %+v", mailer.sent)
		}
	})

	t.Run("a follower sees the request as their own, a stranger does not", func(t *testing.T) {
		mine, err := d.MyRequests(ws.ctx, ben.ID)
		if err != nil || len(mine) != 1 || mine[0].Key != raised.Key {
			t.Errorf("ben's requests = %v, %v; want the one he follows", mine, err)
		}
		if _, err := d.Request(ws.ctx, raised.Key, ben.ID); err != nil {
			t.Errorf("ben cannot read the request: %v", err)
		}
		stranger := ws.customerOf(t, h, "stranger")
		if _, err := d.Request(ws.ctx, raised.Key, stranger.UserID); !errors.Is(err, desk.ErrNotFound) {
			t.Errorf("a stranger got %v, want not found", err)
		}
		if err := d.MayFollow(ws.ctx, raised.Key, ben.ID); !errors.Is(err, desk.ErrNotYourRequest) {
			t.Errorf("a follower adding followers got %v, want refused", err)
		}
	})

	t.Run("the token ends the following, once", func(t *testing.T) {
		if _, err := ws.issues.Unwatch(context.Background(), token); err != nil {
			t.Fatal(err)
		}
		if watching, _ := ws.issues.Watches(ws.ctx, raised.Key, ben.ID); watching {
			t.Error("ben still watches after the token was used")
		}
		if _, err := ws.issues.Unwatch(context.Background(), token); !errors.Is(err, issue.ErrNotWatching) {
			t.Errorf("a used token got %v, want not watching", err)
		}
	})

	t.Run("watchers are the organization's, through SQL", func(t *testing.T) {
		if _, _, _, err := ws.issues.AddWatcher(ws.ctx, raised.Key, ben.ID, customer); err != nil {
			t.Fatal(err)
		}
		other := h.newWorkspace(t, "elsewhere")
		var theirs int
		err := h.cluster.Read(other.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM issue_watcher`).Scan(&theirs)
		})
		if err != nil {
			t.Fatal(err)
		}
		if theirs != 0 {
			t.Errorf("another organization sees %d watchers", theirs)
		}
	})
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The same, over the API: the agents' panel, the requester's followers and the
// way out a mail carries.
func TestFollowersOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	agent.signup(t, h, "followapi")
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Follow", "key": "FLW", "template": "service-desk"}), http.StatusCreated, "desk")
	types := list(t, agent.get("/api/v1/projects/FLW/request-types"), "requestTypes")

	customerEmail := h.email(t, "customer")
	invited := want(t, agent.post("/api/v1/invites", map[string]any{"email": customerEmail, "role": "customer"}), http.StatusCreated, "invite")
	customer := api.client(t)
	want(t, customer.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Sam Customer", "password": testPassword}), http.StatusOK, "joins")
	raised := want(t, customer.post("/api/v1/portal/requests", map[string]any{"requestTypeId": types[0].(map[string]any)["id"], "summary": "Badge reader dead"}), http.StatusCreated, "raise")
	key := obj(t, raised, "request")["key"].(string)

	// The requester names a follower by address; the follower is a person now.
	benAddress := h.email(t, "ben")
	followed := want(t, customer.post("/api/v1/portal/requests/"+key+"/watchers", map[string]any{"email": benAddress}), http.StatusCreated, "follow")
	benID := obj(t, followed, "watcher")["userId"].(string)
	want(t, customer.post("/api/v1/portal/requests/"+key+"/watchers", map[string]any{"email": "not an address"}), http.StatusUnprocessableEntity, "not an address")
	followers := list(t, want(t, customer.get("/api/v1/portal/requests/"+key+"/watchers"), http.StatusOK, "followers"), "watchers")
	if len(followers) != 1 {
		t.Fatalf("followers = %v, want ben", followers)
	}
	stranger := api.client(t)
	strangerInvite := want(t, agent.post("/api/v1/invites", map[string]any{"email": h.email(t, "stranger"), "role": "customer"}), http.StatusCreated, "invite")
	want(t, stranger.post("/api/v1/auth/invites/accept", map[string]any{"token": strangerInvite.Body["token"], "name": "A Stranger", "password": testPassword}), http.StatusOK, "joins")
	want(t, stranger.get("/api/v1/portal/requests/"+key+"/watchers"), http.StatusNotFound, "not theirs to see")
	want(t, stranger.delete("/api/v1/portal/requests/"+key+"/watchers/"+benID), http.StatusNotFound, "nor to change")

	// The agents' side: the panel, oneself, a colleague by id, an address.
	want(t, agent.get("/api/v1/issues/"+key+"/watchers"), http.StatusOK, "watchers")
	want(t, agent.get("/api/v1/issues/FLW-999/watchers"), http.StatusNotFound, "no such issue")
	want(t, agent.post("/api/v1/issues/"+key+"/watchers", map[string]any{}), http.StatusCreated, "watch it myself")
	want(t, agent.post("/api/v1/issues/"+key+"/watchers", map[string]any{"email": h.email(t, "carol")}), http.StatusCreated, "an address")
	want(t, agent.delete("/api/v1/issues/"+key+"/watchers/"+benID), http.StatusNoContent, "remove ben")
	want(t, agent.delete("/api/v1/issues/"+key+"/watchers/"+benID), http.StatusNotFound, "ben was not watching any more")
	want(t, customer.get("/api/v1/issues/"+key+"/watchers"), http.StatusForbidden, "customers use the portal")

	// The requester removes and re-adds their follower.
	want(t, customer.post("/api/v1/portal/requests/"+key+"/watchers", map[string]any{"email": benAddress}), http.StatusCreated, "follow again")
	want(t, customer.delete("/api/v1/portal/requests/"+key+"/watchers/"+benID), http.StatusNoContent, "the requester removes a follower")

	// The way out a mail carries: the token rides in the event, so the test
	// reads it where the notifier would.
	want(t, customer.post("/api/v1/portal/requests/"+key+"/watchers", map[string]any{"email": benAddress}), http.StatusCreated, "and adds them once more")
	var token string
	if err := h.super.QueryRow(context.Background(), `
		SELECT payload->>'token' FROM outbox_event WHERE topic = $1 AND payload->>'key' = $2 ORDER BY created_at DESC LIMIT 1`,
		events.TopicWatcherAdded, key).Scan(&token); err != nil {
		t.Fatal(err)
	}
	anyone := api.client(t)
	want(t, anyone.post("/api/v1/unwatch", map[string]any{"token": "not-a-token-" + uuid.NewString()}), http.StatusNotFound, "an unknown token")
	want(t, anyone.post("/api/v1/unwatch", map[string]any{"token": token}), http.StatusNoContent, "the token stops it")
	want(t, customer.delete("/api/v1/portal/requests/"+key+"/watchers/"+benID), http.StatusNotFound, "ben is gone already")
}
