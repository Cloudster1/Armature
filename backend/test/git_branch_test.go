//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/git"
)

func TestABranchIsMadeOnTheHost(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "branching")
	stub := newStubHost(t)
	found := ws.newIssue(t, "Sign-in rejects a plus sign")

	t.Run("without a token the tracker cannot write to the host", func(t *testing.T) {
		svc, repo := ws.connect(t, h, git.GitHub, stub, "", "")
		if _, _, err := svc.CreateBranch(ws.ctx, found.Key, repo.ID, "", "", ws.actor.UserID); !errors.Is(err, git.ErrNoToken) {
			t.Errorf("err = %v, want ErrNoToken", err)
		}
		if _, err := svc.Disconnect(ws.ctx, repo.ID, ws.actor.UserID, ""); err != nil {
			t.Fatal(err)
		}
	})

	svc, repo := ws.connect(t, h, git.GitHub, stub, "ghp_token", "")
	made, _, err := svc.CreateBranch(ws.ctx, found.Key, repo.ID, "", "", ws.actor.UserID)
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if made.Name != found.Key+"-sign-in-rejects-a-plus-sign" {
		t.Errorf("branch name = %q", made.Name)
	}

	created := stub.calledWith(http.MethodPost, "/repos/acme/portal/git/refs")
	if len(created) != 1 {
		t.Fatalf("the host was asked to create %d refs, want one", len(created))
	}
	if created[0].Auth != "Bearer ghp_token" {
		t.Errorf("the host was called with %q, want the repository's token", created[0].Auth)
	}
	if !strings.Contains(created[0].Body, `"refs/heads/`+made.Name+`"`) || !strings.Contains(created[0].Body, `"ba5e0000"`) {
		t.Errorf("ref request = %s, want the branch off the default branch's sha", created[0].Body)
	}

	dev, err := svc.ForIssue(ws.ctx, found.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.Branches) != 1 || dev.Branches[0].Name != made.Name || dev.Branches[0].HeadSHA != "ba5e0000" {
		t.Errorf("branches = %+v, want the one just made, at the base branch's commit", dev.Branches)
	}
	if dev.BranchName != found.Key+"-sign-in-rejects-a-plus-sign" {
		t.Errorf("the issue suggests %q for its branch", dev.BranchName)
	}

	t.Run("a name git would refuse is refused here first", func(t *testing.T) {
		if _, _, err := svc.CreateBranch(ws.ctx, found.Key, repo.ID, "has space", "", ws.actor.UserID); !errors.Is(err, git.ErrBadBranchName) {
			t.Errorf("err = %v, want ErrBadBranchName", err)
		}
		if len(stub.calledWith(http.MethodPost, "/git/refs")) != 1 {
			t.Error("the host was asked anyway")
		}
	})

	t.Run("a branch the host already has is told apart from a host that is down", func(t *testing.T) {
		if _, _, err := svc.CreateBranch(ws.ctx, found.Key, repo.ID, "taken", "", ws.actor.UserID); !errors.Is(err, git.ErrBranchExists) {
			t.Errorf("err = %v, want ErrBranchExists", err)
		}
	})

	t.Run("the branch can start somewhere other than the default", func(t *testing.T) {
		if _, _, err := svc.CreateBranch(ws.ctx, found.Key, repo.ID, "off-release", "release/2", ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		if len(stub.calledWith(http.MethodGet, "/git/ref/heads/release/2")) != 1 {
			t.Error("the host was not asked where release/2 points")
		}
	})

	t.Run("and on gitlab", func(t *testing.T) {
		other := h.newProject(t, "branchgl")
		gl, glRepo := other.connect(t, h, git.GitLab, stub, "glpat", "")
		theirs := other.newIssue(t, "their fix")
		made, _, err := gl.CreateBranch(other.ctx, theirs.Key, glRepo.ID, "custom-name", "", other.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		calls := stub.calledWith(http.MethodPost, "/projects/acme%2Fportal/repository/branches")
		if len(calls) != 1 || !strings.Contains(calls[0].Path, "branch=custom-name") || calls[0].Auth != "glpat" {
			t.Errorf("gitlab calls = %+v", calls)
		}
		if made.URL != "https://gitlab.example/acme/portal/-/tree/made" || made.HeadSHA != "91ab0000" {
			t.Errorf("branch = %+v, want the host's answer", made)
		}
	})

	t.Run("and on gitea", func(t *testing.T) {
		other := h.newProject(t, "branchgt")
		gt, gtRepo := other.connect(t, h, git.Gitea, stub, "gitea_tok", "")
		theirs := other.newIssue(t, "their fix")
		made, _, err := gt.CreateBranch(other.ctx, theirs.Key, gtRepo.ID, "", "", other.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		calls := stub.calledWith(http.MethodPost, "/repos/acme/portal/branches")
		if len(calls) != 1 || calls[0].Auth != "token gitea_tok" ||
			!strings.Contains(calls[0].Body, `"new_branch_name":"`+theirs.Key+`-their-fix"`) || !strings.Contains(calls[0].Body, `"old_branch_name":"main"`) {
			t.Errorf("gitea calls = %+v", calls)
		}
		if made.URL != "https://gitea.com/acme/portal/src/branch/"+theirs.Key+"-their-fix" || made.HeadSHA != "c0ffee00" {
			t.Errorf("branch = %+v, want gitea's address and the commit it named", made)
		}
	})
}

// A branch made from an issue is the issue's whatever it is called: the work
// pushed to it and the pull request from it reach the issue without the key
// being repeated in every message.
func TestABranchIsFollowedFromTheIssue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "following")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "ghp_token", "Close")
	found := ws.newIssue(t, "Accept a plus sign")

	made, _, err := svc.CreateBranch(ws.ctx, found.Key, repo.ID, "plus-sign-fix", "", ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a push to the branch is the issue's, key or no key", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "push", pushTo(made.Name, "ba5e0000", "f0110001", "tidy the validator"))
		if receipt.Commits != 1 || len(receipt.Linked) != 1 || receipt.Linked[0] != found.Key {
			t.Errorf("receipt = %+v, want the commit linked through its branch", receipt)
		}
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.Commits) != 1 || dev.Commits[0].SHA != "f0110001" {
			t.Errorf("commits = %+v", dev.Commits)
		}
		if len(dev.Branches) != 1 || dev.Branches[0].HeadSHA != "f0110001" || dev.Branches[0].CommitCount != 1 || dev.Branches[0].PushedAt == nil {
			t.Errorf("branch = %+v, want its head moved, one commit counted and the push timed", dev.Branches)
		}

		deliver(t, svc, repo, "push", pushTo(made.Name, "f0110001", "f0110002", "and again"))
		dev, _ = svc.ForIssue(ws.ctx, found.Key)
		if dev.Branches[0].HeadSHA != "f0110002" || dev.Branches[0].CommitCount != 2 || len(dev.Commits) != 2 {
			t.Errorf("after a second push: %+v, %d commits", dev.Branches[0], len(dev.Commits))
		}
	})

	t.Run("a push to a branch nobody made for an issue is not remembered", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "push", pushTo("main", "ba5e0000", "0a100001", "tidy the readme"))
		if receipt.Branches != 0 || len(receipt.Linked) != 0 {
			t.Errorf("receipt = %+v, want nothing linked and no branch row", receipt)
		}
		var rows int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM git_branch WHERE repository_id = $1 AND name = 'main'`, repo.ID).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Error("main got a row of its own")
		}
	})

	t.Run("the pull request from the branch is the issue's too", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "pull_request", pull("opened", 9, "Accept plus signs", made.Name, "f0110002", false))
		if len(receipt.Linked) != 1 || receipt.Linked[0] != found.Key {
			t.Errorf("receipt = %+v, want the pull request linked through its branch", receipt)
		}
		dev, _ := svc.ForIssue(ws.ctx, found.Key)
		if len(dev.PullRequests) != 1 || dev.PullRequests[0].State != git.PullOpen || dev.Branches[0].MergedAt != nil {
			t.Errorf("development = %+v", dev)
		}
	})

	t.Run("merging it merges the branch and takes the repository's transition", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "pull_request", pull("closed", 9, "Accept plus signs", made.Name, "f0110002", true))
		if len(receipt.Transitions) != 1 || receipt.Transitions[0] != found.Key+": Close" {
			t.Errorf("transitions = %v, want the merge to close the issue", receipt.Transitions)
		}
		dev, _ := svc.ForIssue(ws.ctx, found.Key)
		if dev.PullRequests[0].State != git.PullMerged || dev.Branches[0].MergedAt == nil {
			t.Errorf("development = %+v, want the pull request and the branch merged", dev)
		}
		after, _ := ws.issues.ByKey(ws.ctx, found.Key)
		if !after.IsDone() {
			t.Errorf("status = %q, want done", after.Status.Name)
		}
	})

	t.Run("a branch named for an issue is followed even if nobody made it here", func(t *testing.T) {
		other := ws.newIssue(t, "another")
		deliver(t, svc, repo, "push", pushTo(other.Key+"-by-hand", "ba5e0000", "ab000001", "started by hand"))
		dev, _ := svc.ForIssue(ws.ctx, other.Key)
		if len(dev.Branches) != 1 || dev.Branches[0].HeadSHA != "ab000001" || len(dev.Commits) != 1 {
			t.Errorf("development = %+v, want the branch and its commit", dev)
		}
	})
}
