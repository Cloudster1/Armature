//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/git"
)

// deleteBranch is GitHub's delete event for a branch, which is what it sends
// when a merged pull request's head branch is deleted for you.
func deleteBranch(name string) string {
	return `{"ref":` + jsonString(name) + `,"ref_type":"branch","pusher_type":"user"}`
}

// noCommit is how a push names the side of a branch that does not exist.
const noCommit = "0000000000000000000000000000000000000000"

// pushDeleting is the push GitHub sends alongside: the branch moved to nothing.
func pushDeleting(name, before string) string {
	return `{"ref":"refs/heads/` + name + `","before":"` + before + `","after":"` + noCommit + `","created":false,"deleted":true,"commits":[]}`
}

// wire is a branch as a client reads it, so a mark is checked where the panel
// sees it and not only in the Go value.
func wire(t *testing.T, b git.Branch) map[string]any {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// GitHub and GitLab both delete a merged branch on their own, so the merge
// must outlive the branch or the issue forgets how far its work came.
func TestAMergedBranchOutlivesItsDeletion(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "branchgone")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "", "")
	found := ws.newIssue(t, "Accept a plus sign")
	name := found.Key + "-plus-sign"

	deliver(t, svc, repo, "push", pushTo(name, noCommit, "f0110001", "accept plus signs"))
	deliver(t, svc, repo, "pull_request", pull("closed", 4, "Accept plus signs", name, "f0110001", true))
	deliver(t, svc, repo, "delete", deleteBranch(name))
	deliver(t, svc, repo, "push", pushDeleting(name, "f0110001"))

	var mergedID uuid.UUID

	t.Run("merge then delete keeps the branch with both marks", func(t *testing.T) {
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.Branches) != 1 {
			t.Fatalf("branches = %+v, want the merged branch kept after the host deleted it", dev.Branches)
		}
		b := dev.Branches[0]
		mergedID = b.ID
		if b.Name != name || b.MergedAt == nil || b.HeadSHA != "f0110001" {
			t.Errorf("branch = %+v, want it merged at the head it was merged with", b)
		}
		if got := wire(t, b); got["deletedAt"] == nil || got["mergedAt"] == nil {
			t.Errorf("the panel reads %v, want both mergedAt and deletedAt", got)
		}

		var rows, deleted int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*), count(deleted_at) FROM git_branch WHERE repository_id = $1 AND name = $2`,
			repo.ID, name).Scan(&rows, &deleted); err != nil {
			t.Fatal(err)
		}
		if rows != 1 || deleted != 1 {
			t.Errorf("%d rows, %d deleted, want the one row marked deleted once", rows, deleted)
		}
	})

	t.Run("the same name pushed again is a new branch", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "push", pushTo(name, noCommit, "f0110002", "plus signs, again"))
		if receipt.Branches != 1 {
			t.Errorf("receipt = %+v, want the new branch recorded", receipt)
		}
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.Branches) != 2 {
			t.Fatalf("branches = %+v, want the new branch beside the merged one", dev.Branches)
		}
		var fresh, old *git.Branch
		for i := range dev.Branches {
			if dev.Branches[i].ID == mergedID {
				old = &dev.Branches[i]
			} else {
				fresh = &dev.Branches[i]
			}
		}
		if old == nil || fresh == nil {
			t.Fatalf("branches = %+v, want the merged one still there under its own id", dev.Branches)
		}
		if fresh.HeadSHA != "f0110002" || fresh.CommitCount != 1 || fresh.MergedAt != nil || wire(t, *fresh)["deletedAt"] != nil {
			t.Errorf("new branch = %+v, want one commit and neither mark", *fresh)
		}
		if old.HeadSHA != "f0110001" || old.MergedAt == nil || wire(t, *old)["deletedAt"] == nil || old.CommitCount != 1 {
			t.Errorf("old branch = %+v, want it untouched by the new push", *old)
		}
	})

	t.Run("the new branch is merged and deleted in its own right", func(t *testing.T) {
		deliver(t, svc, repo, "pull_request", pull("closed", 5, "Plus signs, again", name, "f0110002", true))
		var merged int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM git_branch WHERE repository_id = $1 AND name = $2 AND merged_at IS NOT NULL`,
			repo.ID, name).Scan(&merged); err != nil {
			t.Fatal(err)
		}
		if merged != 2 {
			t.Errorf("%d branches merged, want both: the old one and the new one", merged)
		}
		deliver(t, svc, repo, "delete", deleteBranch(name))
		dev, _ := svc.ForIssue(ws.ctx, found.Key)
		if len(dev.Branches) != 2 {
			t.Fatalf("branches = %+v, want both merged branches kept", dev.Branches)
		}
		for _, b := range dev.Branches {
			if b.MergedAt == nil || wire(t, b)["deletedAt"] == nil {
				t.Errorf("branch = %+v, want it merged and deleted", b)
			}
		}
	})

	t.Run("a branch deleted without being merged is removed", func(t *testing.T) {
		other := ws.newIssue(t, "Try something")
		tried := other.Key + "-try"
		deliver(t, svc, repo, "push", push(tried, "c0de0001", "try it", "somebody@elsewhere.example"))
		dev, _ := svc.ForIssue(ws.ctx, other.Key)
		if len(dev.Branches) != 1 {
			t.Fatalf("branches = %+v, want the branch before it is deleted", dev.Branches)
		}

		deliver(t, svc, repo, "delete", deleteBranch(tried))
		dev, _ = svc.ForIssue(ws.ctx, other.Key)
		if len(dev.Branches) != 0 {
			t.Errorf("branches = %+v, want an unmerged branch gone with the host's", dev.Branches)
		}
		if len(dev.Commits) != 1 {
			t.Errorf("commits = %+v, want the commit kept: it is still in the history", dev.Commits)
		}
		var rows int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM git_branch WHERE repository_id = $1 AND name = $2`, repo.ID, tried).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Errorf("%d rows left for the deleted branch, want none", rows)
		}
	})

	t.Run("the database keeps one live branch per name and a deleted mark only on a merged one", func(t *testing.T) {
		refusedAs := func(t *testing.T, err error, code string) {
			t.Helper()
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != code {
				t.Errorf("err = %v, want the database to refuse with %s", err, code)
			}
		}
		ctx := context.Background()
		const insert = `INSERT INTO git_branch (repository_id, name) VALUES ($1, $2) RETURNING id`

		// Two deleted ones of this name exist; a live one beside them is fine.
		var live uuid.UUID
		if err := h.super.QueryRow(ctx, insert, repo.ID, name).Scan(&live); err != nil {
			t.Fatalf("a live branch beside deleted ones of its name was refused: %v", err)
		}
		_, err := h.super.Exec(ctx, insert, repo.ID, name)
		refusedAs(t, err, "23505")

		_, err = h.super.Exec(ctx, `UPDATE git_branch SET deleted_at = now() WHERE id = $1`, live)
		refusedAs(t, err, "23514")
		if _, err := h.super.Exec(ctx, `UPDATE git_branch SET merged_at = now(), deleted_at = now() WHERE id = $1`, live); err != nil {
			t.Errorf("marking a merged branch deleted was refused: %v", err)
		}
	})
}
