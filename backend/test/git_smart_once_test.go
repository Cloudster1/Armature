//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/git"
)

// A squash or merge commit copies the branch's messages into a commit with a
// new SHA. What the branch already asked for is not asked again.
func TestASquashCommitDoesNotRepeatTheBranchsCommands(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "squashing")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "", "")
	found := ws.newIssue(t, "a typo on the sign-in page")
	email := ws.owner.Principal.User.Email

	commentsOn := func(t *testing.T, key string) []string {
		t.Helper()
		comments, err := ws.issues.Comments(ws.ctx, key, true)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range comments {
			out = append(out, string(c.Body))
		}
		return out
	}

	receipt := deliver(t, svc, repo, "push", push(found.Key+"-typo", "5a000001",
		found.Key+" #start-progress\n\n"+found.Key+" #comment fixed typo", email))
	if len(receipt.Transitions) != 1 || len(receipt.Refused) != 0 {
		t.Fatalf("the branch's commit did not act: %+v", receipt)
	}
	if got := commentsOn(t, found.Key); len(got) != 1 {
		t.Fatalf("comments after the branch's commit = %v, want one", got)
	}

	t.Run("the squash commit's copy of a line is not carried out again", func(t *testing.T) {
		squash := "Fix the typo on the sign-in page (#12)\n\n* " + found.Key + " #start-progress\n\n* " + found.Key + " #comment fixed typo"
		receipt := deliver(t, svc, repo, "push", push("main", "5a000002", squash, email))
		if receipt.Commits != 1 {
			t.Errorf("the squash commit was not recorded: %+v", receipt)
		}
		if len(receipt.Transitions) != 0 || len(receipt.Refused) != 0 {
			t.Errorf("receipt = %+v, want the copied commands left alone", receipt)
		}
		if got := commentsOn(t, found.Key); len(got) != 1 {
			t.Errorf("comments = %v, want the one comment the branch posted", got)
		}
	})

	t.Run("a new command in the squash commit still runs", func(t *testing.T) {
		squash := "Tidy the sign-in page (#13)\n\n* " + found.Key + " #comment fixed typo\n* " + found.Key + " #comment checked the other pages too"
		deliver(t, svc, repo, "push", push("main", "5a000003", squash, email))
		got := commentsOn(t, found.Key)
		if len(got) != 2 {
			t.Fatalf("comments = %v, want the new one beside the first", got)
		}
		if !strings.Contains(got[0]+got[1], "checked the other pages too") {
			t.Errorf("comments = %v, want the new command's text", got)
		}
	})

	t.Run("the same line about another issue is that issue's command", func(t *testing.T) {
		other := ws.newIssue(t, "the same typo elsewhere")
		deliver(t, svc, repo, "push", push("main", "5a000004", other.Key+" #comment fixed typo", email))
		if got := commentsOn(t, other.Key); len(got) != 1 {
			t.Errorf("comments on %s = %v, want its own comment", other.Key, got)
		}
	})

	t.Run("a command the workflow refused may be asked again later", func(t *testing.T) {
		refused := deliver(t, svc, repo, "push", push(found.Key+"-typo", "5a000005", found.Key+" #approve", email))
		if len(refused.Refused) != 1 {
			t.Fatalf("receipt = %+v, want approving from In Progress refused", refused)
		}
		deliver(t, svc, repo, "push", push(found.Key+"-typo", "5a000006", found.Key+" #ready-for-review", email))
		squash := "Fix the review notes (#14)\n\n* " + found.Key + " #approve"
		receipt := deliver(t, svc, repo, "push", push("main", "5a000007", squash, email))
		if len(receipt.Transitions) != 1 || receipt.Transitions[0] != found.Key+": Approve" {
			t.Errorf("receipt = %+v, want the approval taken now the issue is in review", receipt)
		}
	})

	t.Run("the database keeps one record of a command", func(t *testing.T) {
		ctx := context.Background()
		var issueID, hash string
		if err := h.super.QueryRow(ctx, `
			SELECT issue_id, command_hash FROM git_smart_command WHERE repository_id = $1 LIMIT 1`, repo.ID).
			Scan(&issueID, &hash); err != nil {
			t.Fatalf("no command was recorded: %v", err)
		}
		refusedAs := func(name, code string, err error) {
			t.Helper()
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != code {
				t.Errorf("%s: got %v, want SQLSTATE %s", name, err, code)
			}
		}

		_, err := h.super.Exec(ctx, `
			INSERT INTO git_smart_command (repository_id, issue_id, command_hash) VALUES ($1, $2, $3)`, repo.ID, issueID, hash)
		refusedAs("the same command twice", "23505", err)

		_, err = h.super.Exec(ctx, `
			INSERT INTO git_smart_command (repository_id, issue_id, command_hash) VALUES ($1, $2, $3)`, repo.ID, issueID, "fixed typo")
		refusedAs("the command's text in place of its hash", "23514", err)

	})
}
