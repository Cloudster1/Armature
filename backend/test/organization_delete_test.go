//go:build integration

package test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/tenant"
	"github.com/armature/armature/backend/internal/workflow"
)

func TestDeletingAnOrganization(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	owner := api.client(t)
	signedUp := owner.signup(t, h, "doomed")
	orgID := obj(t, signedUp, "principal", "org")["id"].(string)
	slug := obj(t, signedUp, "principal", "org")["slug"].(string)

	// Enough of everything that a foreign key refusing to cascade would show.
	key := "DMD" + strings.ToUpper(uuid.New().String()[:3])
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Doomed", "key": key, "template": "scrum"}), http.StatusCreated, "scrum project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Doomed desk", "key": "DSK" + strings.ToUpper(uuid.New().String()[:3]), "template": "service-desk"}), http.StatusCreated, "desk")
	issued := want(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "goes with it"}), http.StatusCreated, "issue")
	issueKey := issued.Body["issue"].(map[string]any)["key"].(string)
	want(t, owner.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"text": "so does this"}), http.StatusCreated, "comment")
	want(t, owner.post("/api/v1/projects/"+key+"/sprints", map[string]any{"name": "Last sprint"}), http.StatusCreated, "sprint")
	want(t, owner.post("/api/v1/groups", map[string]any{"name": "Leavers"}), http.StatusCreated, "group")
	want(t, owner.post("/api/v1/tokens", map[string]any{"name": "script"}), http.StatusCreated, "token")
	uploaded := owner.upload("/api/v1/issues/"+issueKey+"/attachments", "file", "evidence.txt", "text/plain", []byte("left in the bucket"))
	if uploaded.Status != http.StatusCreated {
		t.Fatalf("upload: %d %s", uploaded.Status, uploaded.Raw)
	}
	var objectKey string
	if err := h.super.QueryRow(ctx, `SELECT object_key FROM attachment WHERE org_id = $1`, orgID).Scan(&objectKey); err != nil {
		t.Fatal(err)
	}

	// An administrator who is not an owner, in the same organization.
	invite := want(t, owner.post("/api/v1/invites", map[string]string{"email": h.email(t, "doomedadmin"), "role": "admin"}), http.StatusCreated, "invite")
	admin := api.client(t)
	want(t, admin.post("/api/v1/auth/invites/accept", map[string]string{"token": invite.Body["token"].(string), "name": "Not An Owner", "password": testPassword}), http.StatusOK, "accept")

	// A bystander organization that must not notice.
	bystander := api.client(t)
	bystanderOrg := obj(t, bystander.signup(t, h, "bystander"), "principal", "org")["id"].(string)

	orgRows := func(id string) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM org WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("an administrator who is not an owner cannot", func(t *testing.T) {
		refused := admin.delete("/api/v1/organization?confirm=" + slug)
		if refused.Status != http.StatusForbidden {
			t.Errorf("admin delete = %d %s, want 403", refused.Status, refused.Raw)
		}
		if orgRows(orgID) != 1 {
			t.Fatal("the organization went anyway")
		}
	})

	t.Run("an owner who does not type the address back cannot", func(t *testing.T) {
		for _, confirm := range []string{"", "something-else", strings.ToUpper(slug)} {
			refused := owner.delete("/api/v1/organization?confirm=" + confirm)
			if refused.Status != http.StatusUnprocessableEntity || refused.ErrorCode() != "validation_failed" {
				t.Errorf("confirm %q = %d %s, want a validation error", confirm, refused.Status, refused.Raw)
			}
		}
		if orgRows(orgID) != 1 {
			t.Fatal("the organization went without its address typed back")
		}
	})

	// The database refuses it too: an application connection scoped to the
	// bystander cannot see, let alone delete, another tenant's organization.
	t.Run("a tenant cannot delete another tenant's organization through SQL", func(t *testing.T) {
		scoped := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: uuid.MustParse(bystanderOrg)}))
		_, err := h.cluster.Write(scoped, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `DELETE FROM org WHERE id = $1`, orgID)
			if err == nil && tag.RowsAffected() != 0 {
				t.Errorf("row level security let a tenant delete %d other organizations", tag.RowsAffected())
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if orgRows(orgID) != 1 {
			t.Fatal("the organization went through another tenant's connection")
		}
	})

	t.Run("the owner deletes it, with everything in it", func(t *testing.T) {
		want(t, owner.delete("/api/v1/organization?confirm="+slug), http.StatusNoContent, "delete organization")

		if orgRows(orgID) != 0 {
			t.Fatal("the organization is still there")
		}
		for _, table := range []string{"org_member", "project", "issue", "sprint", "user_group", "api_token", "attachment", "audit_log", "role_assignment"} {
			var n int
			if err := h.super.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE org_id = $1`, orgID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%d rows of %s outlived their organization", n, table)
			}
		}

		// The bytes are still in the bucket; the tombstone that says so must
		// not have gone with the organization, or nothing would remove them.
		var orphaned bool
		if err := h.super.QueryRow(ctx, `SELECT org_id IS NULL FROM attachment_tombstone WHERE object_key = $1`, objectKey).Scan(&orphaned); err != nil {
			t.Fatalf("the attachment's tombstone went with the organization: %v", err)
		}
		if !orphaned {
			t.Error("the tombstone still names a deleted organization")
		}

		// The accounts stay; their sessions simply have no organization now.
		for who, c := range map[string]*client{"owner": owner, "admin": admin} {
			me := c.get("/api/v1/auth/me")
			if me.Status != http.StatusOK {
				t.Fatalf("%s: me after deletion = %d %s", who, me.Status, me.Raw)
			}
			if principal := me.Body["principal"].(map[string]any); principal["org"] != nil {
				t.Errorf("%s's session still points at %v", who, principal["org"])
			}
		}

		// And the reaper, which reads tombstones across every organization,
		// takes the bytes out of the bucket even though nobody owns them now.
		store := h.attachmentStore(t)
		if _, err := store.Get(ctx, objectKey); err != nil {
			t.Fatalf("the object should still be in the bucket before the reaper runs: %v", err)
		}
		engine := workflow.NewEngine(workflow.NewDefaultRegistry())
		reaper := attachment.NewReaper(attachment.NewService(h.cluster, store, issue.NewService(h.cluster, engine, workflow.NewStore())), slog.New(slog.NewTextHandler(io.Discard, nil)))
		var left int
		// Other tests leave tombstones too, and a pass takes a batch of them.
		for range 20 {
			if _, err := reaper.Once(ctx); err != nil {
				t.Fatal(err)
			}
			if err := h.super.QueryRow(ctx, `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, objectKey).Scan(&left); err != nil {
				t.Fatal(err)
			}
			if left == 0 {
				break
			}
		}
		if left != 0 {
			t.Fatal("the reaper never took the deleted organization's tombstone")
		}
		if _, err := store.Get(ctx, objectKey); err != attachment.ErrNoObject {
			t.Errorf("the deleted organization's file is still in the bucket: %v", err)
		}

		if orgRows(bystanderOrg) != 1 {
			t.Error("the bystander organization went too")
		}
		want(t, bystander.get("/api/v1/projects"), http.StatusOK, "bystander still works")
	})
}
