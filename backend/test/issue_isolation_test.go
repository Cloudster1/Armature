//go:build integration

package test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// TestIssueTenantIsolation extends the isolation guarantee to everything this
// milestone added. Each new table is another chance to leak, so each is checked
// rather than assumed to inherit the property.
func TestIssueTenantIsolation(t *testing.T) {
	h := newHarness(t)

	alpha := h.newWorkspace(t, "isoalpha")
	beta := h.newWorkspace(t, "isobeta")

	alphaIssue := alpha.newIssue(t, "alpha's private issue")
	betaIssue := beta.newIssue(t, "beta's private issue")

	if _, _, err := alpha.issues.AddComment(alpha.ctx, alphaIssue.Key,
		issue.TextDocument("alpha's private comment"), alpha.actor); err != nil {
		t.Fatal(err)
	}

	t.Run("each organization sees only its own configuration", func(t *testing.T) {
		// Bootstrap gives every organization its own statuses, types and
		// workflow, so the counts must not add up across tenants.
		count := func(ctx context.Context, table string) int {
			var n int
			err := h.cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
			})
			if err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			return n
		}
		for table, want := range map[string]int{
			"issue_status":    4,
			"issue_type":      6,
			"workflow":        1,
			"workflow_scheme": 1,
			"issue_link_type": 3,
		} {
			if got := count(alpha.ctx, table); got != want {
				t.Errorf("alpha sees %d rows in %s, want only its own %d", got, table, want)
			}
		}
	})

	t.Run("issues are invisible across organizations", func(t *testing.T) {
		if _, err := alpha.issues.ByKey(alpha.ctx, betaIssue.Key); !errors.Is(err, issue.ErrNotFound) {
			t.Errorf("alpha read beta's issue: %v", err)
		}
		if _, err := beta.issues.ByKey(beta.ctx, alphaIssue.Key); !errors.Is(err, issue.ErrNotFound) {
			t.Errorf("beta read alpha's issue: %v", err)
		}

		listed, err := alpha.issues.List(alpha.ctx, issue.Filter{}, issue.Page{Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range listed.Issues {
			if i.Key == betaIssue.Key {
				t.Error("beta's issue appeared in alpha's unfiltered listing")
			}
		}
	})

	t.Run("another organization's issue cannot be edited or moved", func(t *testing.T) {
		summary := "renamed by a stranger"
		if _, _, err := alpha.issues.Update(alpha.ctx, betaIssue.Key,
			issue.UpdateInput{Summary: &summary}, alpha.actor); !errors.Is(err, issue.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}

		if _, _, err := alpha.issues.Transition(alpha.ctx, betaIssue.Key,
			issue.TransitionInput{TransitionID: uuid.New()}, alpha.actor); !errors.Is(err, issue.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}

		// And beta's issue is untouched.
		after, err := beta.issues.ByKey(beta.ctx, betaIssue.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Summary != betaIssue.Summary {
			t.Errorf("beta's issue summary is now %q", after.Summary)
		}
	})

	t.Run("comments and history do not cross organizations", func(t *testing.T) {
		comments, err := beta.issues.Comments(beta.ctx, alphaIssue.Key, true)
		if err != nil && !errors.Is(err, issue.ErrNotFound) {
			t.Fatal(err)
		}
		if len(comments) != 0 {
			t.Errorf("beta read %d of alpha's comments", len(comments))
		}

		history, err := beta.issues.History(beta.ctx, alphaIssue.Key)
		if err != nil && !errors.Is(err, issue.ErrNotFound) {
			t.Fatal(err)
		}
		if len(history) != 0 {
			t.Errorf("beta read %d of alpha's changelog entries", len(history))
		}
	})

	t.Run("a project key can be reused in a different organization", func(t *testing.T) {
		// Keys are unique per organization, not globally: two customers must
		// both be able to have a project called PROJ.
		shared := "SHRD"
		if _, _, err := alpha.projects.Create(alpha.ctx, project.CreateInput{Name: "Shared name", Key: shared}, alpha.actor.UserID); err != nil {
			t.Fatalf("alpha could not take the key: %v", err)
		}
		if _, _, err := beta.projects.Create(beta.ctx, project.CreateInput{Name: "Shared name", Key: shared}, beta.actor.UserID); err != nil {
			t.Errorf("beta could not use the same key in its own organization: %v", err)
		}
	})

	t.Run("an issue cannot be assigned to somebody outside the organization", func(t *testing.T) {
		stranger := beta.actor.UserID
		_, _, err := alpha.issues.Update(alpha.ctx, alphaIssue.Key,
			issue.UpdateInput{Assignee: ptr(&stranger)}, alpha.actor)
		if err == nil {
			t.Fatal("an outsider was assigned an issue")
		}

		after, err := alpha.issues.ByKey(alpha.ctx, alphaIssue.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Assignee != nil {
			t.Errorf("assignee = %v, want it left unset", after.Assignee)
		}
	})
}

func TestIssueEditing(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "editing")

	t.Run("changed fields are recorded, unchanged ones are not", func(t *testing.T) {
		created := ws.newIssue(t, "original summary")

		summary := "a better summary"
		priority := issue.PriorityHigh
		updated, _, err := ws.issues.Update(ws.ctx, created.Key, issue.UpdateInput{
			Summary:  &summary,
			Priority: &priority,
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if updated.Summary != summary || updated.Priority != priority {
			t.Fatalf("update did not apply: %+v", updated)
		}

		history, err := ws.issues.History(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}
		// Newest first: the edit, then the creation.
		if len(history) != 2 {
			t.Fatalf("got %d changelog entries, want 2", len(history))
		}
		fields := map[string]issue.Change{}
		for _, c := range history[0].Changes {
			fields[c.Field] = c
		}
		if len(fields) != 2 {
			t.Errorf("the entry records %d changes, want exactly summary and priority: %+v", len(fields), history[0].Changes)
		}
		if fields["summary"].From != "original summary" || fields["summary"].To != summary {
			t.Errorf("summary change = %+v", fields["summary"])
		}
		if fields["priority"].From != "medium" || fields["priority"].To != "high" {
			t.Errorf("priority change = %+v", fields["priority"])
		}
	})

	// Clients resend whole forms. Recording an entry for a submission that
	// changed nothing would bury the real history in noise.
	t.Run("an update that changes nothing writes no changelog entry", func(t *testing.T) {
		created := ws.newIssue(t, "unchanged")

		same := "unchanged"
		if _, _, err := ws.issues.Update(ws.ctx, created.Key, issue.UpdateInput{Summary: &same}, ws.actor); err != nil {
			t.Fatal(err)
		}

		history, err := ws.issues.History(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 1 {
			t.Errorf("got %d changelog entries, want only the creation", len(history))
		}
	})

	t.Run("assigning and unassigning are both recorded", func(t *testing.T) {
		created := ws.newIssue(t, "assignment log")

		if _, _, err := ws.issues.Update(ws.ctx, created.Key,
			issue.UpdateInput{Assignee: ptr(&ws.actor.UserID)}, ws.actor); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.issues.Update(ws.ctx, created.Key,
			issue.UpdateInput{Assignee: ptr[*uuid.UUID](nil)}, ws.actor); err != nil {
			t.Fatal(err)
		}

		history, err := ws.issues.History(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 3 {
			t.Fatalf("got %d entries, want creation plus two assignment changes", len(history))
		}
		if c := history[0].Changes[0]; c.To != "Unassigned" {
			t.Errorf("latest change = %+v, want the unassignment", c)
		}
	})

	t.Run("the status cannot be edited directly", func(t *testing.T) {
		// UpdateInput has no status field at all, which is the enforcement: the
		// only way to change status is through a transition, so the workflow's
		// rules cannot be side-stepped. This test documents that as an
		// intentional property rather than an oversight.
		created := ws.newIssue(t, "status is workflow-only")
		before, err := ws.issues.ByKey(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}

		summary := "edited"
		if _, _, err := ws.issues.Update(ws.ctx, created.Key, issue.UpdateInput{Summary: &summary}, ws.actor); err != nil {
			t.Fatal(err)
		}

		after, err := ws.issues.ByKey(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status.ID != before.Status.ID {
			t.Error("an ordinary edit changed the status")
		}
	})

	t.Run("comments can be edited by their author and nobody else", func(t *testing.T) {
		created := ws.newIssue(t, "commentable")
		comment, _, err := ws.issues.AddComment(ws.ctx, created.Key, issue.TextDocument("first thoughts"), ws.actor)
		if err != nil {
			t.Fatal(err)
		}

		edited, _, err := ws.issues.EditComment(ws.ctx, comment.ID, issue.TextDocument("second thoughts"), ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if issue.PlainText(edited.Body) != "second thoughts" {
			t.Errorf("body = %q", issue.PlainText(edited.Body))
		}
		if edited.EditedAt == nil {
			t.Error("an edited comment is not marked as edited")
		}

		stranger := issue.Actor{UserID: uuid.New(), OrgRole: "member"}
		if _, _, err := ws.issues.EditComment(ws.ctx, comment.ID, issue.TextDocument("hijacked"), stranger); !errors.Is(err, issue.ErrNotYourComment) {
			t.Errorf("error = %v, want ErrNotYourComment", err)
		}
	})

	t.Run("a comment must be a document with content", func(t *testing.T) {
		created := ws.newIssue(t, "validated comments")
		for _, body := range []string{``, `null`, `{"type":"paragraph"}`, `{"type":"doc","content":[]}`, `"just a string"`} {
			if _, _, err := ws.issues.AddComment(ws.ctx, created.Key, []byte(body), ws.actor); err == nil {
				t.Errorf("comment body %q was accepted", body)
			}
		}
	})
}

func TestIssueFiltering(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "filtering")

	todo := ws.newIssue(t, "still to do")
	started := ws.newIssue(t, "being worked on")
	ws.move(t, h, started.Key, "Start progress")
	closed := ws.newIssue(t, "already closed")
	ws.move(t, h, closed.Key, "Close")

	keysOf := func(r *issue.Result) map[string]bool {
		out := map[string]bool{}
		for _, i := range r.Issues {
			out[i.Key] = true
		}
		return out
	}

	t.Run("by status category", func(t *testing.T) {
		open, err := ws.issues.List(ws.ctx, issue.Filter{
			ProjectKey: ws.project.Key,
			Categories: []workflow.StatusCategory{workflow.CategoryTodo, workflow.CategoryInProgress},
		}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		got := keysOf(open)
		if !got[todo.Key] || !got[started.Key] {
			t.Errorf("open issues = %v, want the to-do and in-progress ones", got)
		}
		if got[closed.Key] {
			t.Error("a done issue matched an open-only filter")
		}
	})

	t.Run("by assignee, and by nobody", func(t *testing.T) {
		mine, err := ws.issues.List(ws.ctx, issue.Filter{AssigneeID: &ws.actor.UserID}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		// Start progress assigned it, so exactly that one is mine.
		if got := keysOf(mine); !got[started.Key] || got[todo.Key] {
			t.Errorf("assigned to me = %v, want only %s", got, started.Key)
		}

		unassigned, err := ws.issues.List(ws.ctx, issue.Filter{Unassigned: true}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if got := keysOf(unassigned); got[started.Key] {
			t.Errorf("an assigned issue matched the unassigned filter: %v", got)
		}
	})

	t.Run("by text in the summary", func(t *testing.T) {
		found, err := ws.issues.List(ws.ctx, issue.Filter{Text: "worked"}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if got := keysOf(found); !got[started.Key] || len(got) != 1 {
			t.Errorf("text search = %v, want just %s", got, started.Key)
		}
	})

	t.Run("paging reports the full total", func(t *testing.T) {
		page, err := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: ws.project.Key},
			issue.Page{Limit: 2, OrderBy: "key"})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Issues) != 2 {
			t.Errorf("got %d issues on the page, want 2", len(page.Issues))
		}
		if page.Total != 3 {
			t.Errorf("total = %d, want 3", page.Total)
		}
	})

	// A filter naming a nonexistent project must return nothing, not
	// everything, or a typo silently widens the query.
	t.Run("an unknown project matches nothing", func(t *testing.T) {
		none, err := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: "NOSUCH"}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if none.Total != 0 {
			t.Errorf("total = %d, want 0", none.Total)
		}
	})
}
