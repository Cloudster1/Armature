//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/filter"
)

// Changing somebody else's private filter answers as for one that does not
// exist; a shared one they can see is refused as not theirs (#414).
func TestAnotherPersonsFilterIsNotFoundUnlessItIsShared(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "filterowner", auth.RoleMember))
	other := api.client(t)
	other.signIn(t, h.person(t, "filterother", auth.RoleMember))

	made := func(name string, shared bool) string {
		t.Helper()
		r := want(t, owner.post("/api/v1/filters", map[string]any{"name": name, "query": "project = X", "shared": shared}), http.StatusCreated, "save "+name)
		return obj(t, r, "filter")["id"].(string)
	}
	unchanged := func(t *testing.T, id, wantName string) {
		t.Helper()
		var name string
		if err := h.super.QueryRow(context.Background(), `SELECT name FROM saved_filter WHERE id = $1`, id).Scan(&name); err != nil {
			t.Fatalf("the filter is gone: %v", err)
		}
		if name != wantName {
			t.Errorf("the filter is named %q, want %q untouched", name, wantName)
		}
	}

	private := made("private", false)
	want(t, other.patch("/api/v1/filters/"+private, map[string]any{"name": "Taken over"}), http.StatusNotFound, "changing a private filter of somebody else")
	want(t, other.delete("/api/v1/filters/"+private), http.StatusNotFound, "deleting a private filter of somebody else")
	want(t, other.patch("/api/v1/filters/"+uuid.NewString(), map[string]any{"name": "Taken over"}), http.StatusNotFound, "changing a filter that does not exist")
	unchanged(t, private, "private")

	shared := made("shared", true)
	want(t, other.patch("/api/v1/filters/"+shared, map[string]any{"name": "Taken over"}), http.StatusForbidden, "changing a shared filter of somebody else")
	want(t, other.delete("/api/v1/filters/"+shared), http.StatusForbidden, "deleting a shared filter of somebody else")
	unchanged(t, shared, "shared")

	want(t, owner.delete("/api/v1/filters/"+private), http.StatusNoContent, "the owner deletes their own")
}

// A subscription its reader may no longer see is removed the first time it is
// due, and is not counted as sent on that pass or any after (#413).
func TestASubscriptionNobodyMayReadIsDroppedNotSent(t *testing.T) {
	h := newHarness(t)
	ws := h.newProject(t, "orphansub")
	filters := filter.NewService(h.cluster)
	me := ws.actor.UserID
	bobEmail := h.email(t, "bobsub")
	bob := h.joinExisting(t, bobEmail, "member")
	carolEmail := h.email(t, "carolsub")
	carol := h.joinExisting(t, carolEmail, "member")

	shared, _, err := filters.Create(ws.ctx, me, filter.Input{Name: ptr("Everything open"), Query: ptr("statusCategory != done"), Shared: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	for _, who := range []uuid.UUID{bob, carol} {
		if _, _, err := filters.Subscribe(ws.ctx, shared.ID, who, filter.Subscription{Schedule: filter.Daily, Hour: 0}); err != nil {
			t.Fatal(err)
		}
	}
	// Bob stops being shown it; Carol leaves the installation.
	if _, _, err := filters.Update(ws.ctx, shared.ID, me, filter.Input{Shared: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET standing = NULL WHERE id = $1`, carol); err != nil {
		t.Fatal(err)
	}

	mailer := &fakeMailer{}
	subs := filter.NewSubscriptions(h.cluster, ws.issues, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := subs.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM saved_filter_subscription WHERE filter_id = $1`, shared.ID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d subscriptions nobody may read are still there, want them removed", left)
	}
	// Everything due went out in the first pass, so a second one at once sends
	// nothing; an orphan kept would be counted again here.
	if again, err := subs.Once(context.Background()); err != nil || again != 0 {
		t.Errorf("the second pass counted %d sent (%v), want 0", again, err)
	}
	mailer.mu.Lock()
	defer mailer.mu.Unlock()
	for _, m := range mailer.sent {
		if m.To == bobEmail || m.To == carolEmail {
			t.Errorf("a subscriber who may no longer see the filter was mailed: %+v", m)
		}
	}
}
