//go:build integration

package test

import (
	"testing"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/git"
)

// deleteBranch is GitHub's push for a branch removed on the host.
func deleteBranch(branch, before string) string {
	return `{"ref":"refs/heads/` + branch + `","before":"` + before + `","after":"0000000000000000000000000000000000000000","created":false,"deleted":true,"commits":[]}`
}

func TestAMergeMovesTheIssueOnce(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "mergeonce")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "", "Close")
	found := ws.newIssue(t, "ship the export")
	branch := found.Key + "-export"
	title := found.Key + " ship the export"

	status := func(t *testing.T) string {
		t.Helper()
		after, err := ws.issues.ByKey(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		return after.Status.Name
	}

	deliver(t, svc, repo, "push", pushTo(branch, "ba5e0000", "e0000001", "start the export"))
	deliver(t, svc, repo, "pull_request", pull("opened", 21, title, branch, "e0000001", false))
	merge := pull("closed", 21, title, branch, "e0000001", true)

	t.Run("the merge itself takes the repository's transition", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "pull_request", merge)
		if len(receipt.Transitions) != 1 || receipt.Transitions[0] != found.Key+": Close" {
			t.Fatalf("transitions = %v (refused %v), want the merge to close the issue", receipt.Transitions, receipt.Refused)
		}
		if got := status(t); got != bootstrap.StatusDone {
			t.Fatalf("status = %q, want Done", got)
		}
	})

	ws.move(t, h, found.Key, "Reopen")

	t.Run("an edit of the merged pull request leaves the reopened issue open", func(t *testing.T) {
		for _, action := range []string{"edited", "labeled"} {
			receipt := deliver(t, svc, repo, "pull_request", pull(action, 21, title, branch, "e0000001", true))
			if len(receipt.Transitions) != 0 || len(receipt.Refused) != 0 {
				t.Errorf("%s: receipt = %+v, want no transition tried", action, receipt)
			}
		}
		if got := status(t); got != bootstrap.StatusToDo {
			t.Errorf("status = %q, want the reopened issue left in To Do", got)
		}
	})

	t.Run("a redelivered merge does nothing the second time", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "pull_request", merge)
		if len(receipt.Transitions) != 0 || len(receipt.Refused) != 0 {
			t.Errorf("receipt = %+v, want no transition tried", receipt)
		}
		if got := status(t); got != bootstrap.StatusToDo {
			t.Errorf("status = %q, want the reopened issue left in To Do", got)
		}
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.PullRequests) != 1 || dev.PullRequests[0].State != git.PullMerged {
			t.Errorf("pull requests = %+v, want the one, still merged", dev.PullRequests)
		}
	})

	t.Run("a branch made again under the merged name is not marked merged by the old pull request", func(t *testing.T) {
		deliver(t, svc, repo, "push", deleteBranch(branch, "e0000001"))
		deliver(t, svc, repo, "push", pushTo(branch, "0000000000000000000000000000000000000000", "e0000002", "start over"))
		deliver(t, svc, repo, "pull_request", pull("edited", 21, title, branch, "e0000001", true))
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.Branches) != 1 || dev.Branches[0].HeadSHA != "e0000002" || dev.Branches[0].MergedAt != nil {
			t.Errorf("branches = %+v, want the new branch unmerged", dev.Branches)
		}
	})

	t.Run("a pull request first seen merged still takes the transition", func(t *testing.T) {
		other := ws.newIssue(t, "merged before we looked")
		otherTitle := other.Key + " merged before we looked"
		receipt := deliver(t, svc, repo, "pull_request", pull("closed", 22, otherTitle, other.Key+"-late", "e0000003", true))
		if len(receipt.Transitions) != 1 || receipt.Transitions[0] != other.Key+": Close" {
			t.Errorf("transitions = %v (refused %v), want the merge to close the issue", receipt.Transitions, receipt.Refused)
		}
	})
}
