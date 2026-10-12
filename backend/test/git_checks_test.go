//go:build integration

package test

import (
	"strconv"
	"testing"

	"github.com/armature/armature/backend/internal/git"
)

// checkRun is GitHub's delivery for one job's check run against a commit.
func checkRun(id int64, name, conclusion, sha, startedAt string) string {
	return `{"check_run":{"id":` + strconv.FormatInt(id, 10) + `,"name":"` + name + `","status":"completed","conclusion":"` + conclusion + `",` +
		`"html_url":"https://github.com/acme/portal/runs/` + strconv.FormatInt(id, 10) + `","head_sha":"` + sha + `",` +
		`"started_at":"` + startedAt + `","completed_at":"` + startedAt + `","check_suite":{"head_branch":"work"}}}`
}

// A pull request's CI answer is every check against its head, not whichever
// one started last: a failing test must not hide behind a passing lint.
func TestAPullRequestsCIReadsFailedWhenAnyCheckFailed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newWorkspace(t, "checking")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "", "")
	found := ws.newIssue(t, "make the checks honest")
	const head = "bead0002"

	deliver(t, svc, repo, "push", push(found.Key+"-checks", head, found.Key+" make the checks honest", ws.owner.Principal.User.Email))
	deliver(t, svc, repo, "pull_request", pull("opened", 8, found.Key+" honest checks", found.Key+"-checks", head, false))
	deliver(t, svc, repo, "check_run", checkRun(21, "test", "failure", head, "2026-09-02T10:00:00Z"))
	deliver(t, svc, repo, "check_run", checkRun(22, "lint", "success", head, "2026-09-02T10:01:00Z"))
	// A failure on some other commit says nothing about this head.
	deliver(t, svc, repo, "check_run", checkRun(20, "lint", "failure", "01de0001", "2026-09-02T11:00:00Z"))

	dev, err := svc.ForIssue(ws.ctx, found.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.PullRequests) != 1 {
		t.Fatalf("pull requests = %+v", dev.PullRequests)
	}
	ci := dev.PullRequests[0].CI
	if ci == nil || ci.Status != git.CIFailure {
		t.Fatalf("pull request CI = %+v, want failure while test has failed", ci)
	}
	if len(ci.Checks) != 2 || ci.Checks[0].Name != "lint" || ci.Checks[1].Name != "test" {
		t.Errorf("checks = %+v, want lint and test behind the answer", ci.Checks)
	}
	if len(dev.Commits) != 1 || dev.Commits[0].CI == nil || dev.Commits[0].CI.Status != git.CIFailure {
		t.Errorf("commits = %+v, want the head commit to read failed too", dev.Commits)
	}

	t.Run("a rerun that passes clears the failure", func(t *testing.T) {
		deliver(t, svc, repo, "check_run", checkRun(23, "test", "success", head, "2026-09-02T10:05:00Z"))
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		ci := dev.PullRequests[0].CI
		if ci == nil || ci.Status != git.CISuccess {
			t.Fatalf("pull request CI = %+v, want success once test was rerun green", ci)
		}
		if len(ci.Checks) != 2 {
			t.Errorf("checks = %+v, want one per check name", ci.Checks)
		}
	})

	t.Run("a check still running holds the answer at pending", func(t *testing.T) {
		deliver(t, svc, repo, "check_run", `{"check_run":{"id":24,"name":"e2e","status":"in_progress","conclusion":null,`+
			`"html_url":"https://github.com/acme/portal/runs/24","head_sha":"`+head+`","started_at":"2026-09-02T09:00:00Z","check_suite":{"head_branch":"work"}}}`)
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if ci := dev.PullRequests[0].CI; ci == nil || ci.Status != git.CIPending || len(ci.Checks) != 3 {
			t.Errorf("pull request CI = %+v, want pending with three checks", ci)
		}
	})
}
