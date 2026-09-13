//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/git"
)

// stubHost stands in for GitHub and GitLab: it answers the few API calls the
// integration makes and remembers what it was asked, so a test can assert on
// what would have happened on the real host.
type stubHost struct {
	*httptest.Server
	mu    sync.Mutex
	calls []hostCall
}

type hostCall struct {
	Method, Path, Auth, Body string
}

func newStubHost(t *testing.T) *stubHost {
	t.Helper()
	s := &stubHost{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = r.Header.Get("PRIVATE-TOKEN")
		}
		s.mu.Lock()
		s.calls = append(s.calls, hostCall{Method: r.Method, Path: r.URL.EscapedPath() + queryOf(r), Auth: auth, Body: string(body)})
		s.mu.Unlock()

		// A host that is not given a token refuses, as the real ones do.
		if auth == "" || auth == "Bearer " {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			_, _ = w.Write([]byte(`{"object":{"sha":"ba5e0000"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs") && strings.Contains(string(body), "refs/heads/taken"):
			// GitHub's answer for a ref that is already there.
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/repository/branches"):
			_, _ = w.Write([]byte(`{"web_url":"https://gitlab.example/acme/portal/-/tree/made","commit":{"id":"91ab0000"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/branches"):
			// Gitea answers with the branch and the commit it starts at.
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"made","commit":{"id":"c0ffee00"}}`))
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func queryOf(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return ""
	}
	return "?" + r.URL.RawQuery
}

func (s *stubHost) calledWith(method, pathPart string) []hostCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []hostCall
	for _, c := range s.calls {
		if c.Method == method && strings.Contains(c.Path, pathPart) {
			out = append(out, c)
		}
	}
	return out
}

// connect attaches a repository at the stub host to the workspace's project.
func (ws *workspace) connect(t *testing.T, h *harness, kind git.HostKind, stub *stubHost, token, onMerge string) (*git.Service, *git.Repository) {
	t.Helper()
	svc := git.NewService(h.cluster, ws.issues)
	svc.UseHTTPClient(stub.Client())
	repo, _, err := svc.Connect(ws.ctx, ws.project.Key, git.ConnectInput{
		Host: kind, Name: "acme/portal", APIBaseURL: stub.URL, AccessToken: token, TransitionOnMerge: onMerge,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("connect repository: %v", err)
	}
	return svc, repo
}

// deliver hands a webhook to the service the way the endpoint would, signed
// for the host in question.
func deliver(t *testing.T, svc *git.Service, repo *git.Repository, event, body string) *git.Receipt {
	t.Helper()
	receipt, err := svc.Receive(context.Background(), repo.ID, signed(repo, event, body), []byte(body))
	if err != nil {
		t.Fatalf("deliver %s: %v", event, err)
	}
	return receipt
}

func signed(repo *git.Repository, event, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/git/webhooks/"+repo.ID.String(), bytes.NewBufferString(body))
	switch repo.Host {
	case git.GitLab:
		r.Header.Set("X-Gitlab-Event", event)
		r.Header.Set("X-Gitlab-Token", repo.WebhookSecret)
	case git.Gitea:
		r.Header.Set("X-Gitea-Event", event)
		r.Header.Set("X-Gitea-Signature", git.SignGitea([]byte(body), repo.WebhookSecret))
	default:
		r.Header.Set("X-GitHub-Event", event)
		r.Header.Set("X-Hub-Signature-256", git.SignGitHub([]byte(body), repo.WebhookSecret))
	}
	return r
}

func push(branch, sha, message, email string) string {
	return `{"ref":"refs/heads/` + branch + `","created":true,"commits":[{"id":"` + sha + `","message":` + jsonString(message) +
		`,"url":"https://github.com/acme/portal/commit/` + sha + `","timestamp":"2026-09-01T10:00:00Z","author":{"name":"Somebody","email":"` + email + `"}}]}`
}

// pushTo is an ordinary push to a branch that already exists: not a creation,
// and with the head the branch is left at.
func pushTo(branch, before, after, message string) string {
	return `{"ref":"refs/heads/` + branch + `","before":"` + before + `","after":"` + after + `","created":false,"deleted":false,"commits":[{"id":"` + after +
		`","message":` + jsonString(message) + `,"url":"https://github.com/acme/portal/commit/` + after + `","timestamp":"2026-09-02T10:00:00Z","author":{"name":"Somebody","email":"somebody@elsewhere.example"}}]}`
}

func pull(action string, number int64, title, head, sha string, merged bool) string {
	state, mergedAt := "open", "null"
	if action == "closed" {
		state = "closed"
	}
	if merged {
		mergedAt = `"2026-09-03T10:00:00Z"`
	}
	return `{"action":"` + action + `","pull_request":{"number":` + strconv.FormatInt(number, 10) + `,"title":` + jsonString(title) +
		`,"html_url":"https://github.com/acme/portal/pull/` + strconv.FormatInt(number, 10) + `","state":"` + state + `","merged":` + strconv.FormatBool(merged) +
		`,"merged_at":` + mergedAt + `,"created_at":"2026-09-02T12:00:00Z","user":{"login":"somebody"},"head":{"ref":"` + head + `","sha":"` + sha + `"},"base":{"ref":"main"}}}`
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestAPushLinksCommitsAndDrivesTheWorkflow(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "pushing")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "", "")
	found := ws.newIssue(t, "fix the validator")

	t.Run("a wrong signature is refused before anything is read", func(t *testing.T) {
		body := push("main", "bad00001", found.Key+" #start-progress", ws.owner.Principal.User.Email)
		r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(body))
		r.Header.Set("X-GitHub-Event", "push")
		r.Header.Set("X-Hub-Signature-256", git.SignGitHub([]byte(body), "not the secret"))
		_, err := svc.Receive(context.Background(), repo.ID, r, []byte(body))
		if !errors.Is(err, git.ErrUnauthenticated) {
			t.Fatalf("err = %v, want ErrUnauthenticated", err)
		}
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.Commits) != 0 {
			t.Errorf("an unauthenticated delivery still recorded %d commits", len(dev.Commits))
		}
	})

	t.Run("a commit naming the issue is linked and its command carried out", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "push", push(found.Key+"-fix", "abc00001", found.Key+" #start-progress fix it", ws.owner.Principal.User.Email))
		if receipt.Commits != 1 || len(receipt.Linked) != 1 || receipt.Linked[0] != found.Key {
			t.Errorf("receipt = %+v", receipt)
		}
		if len(receipt.Transitions) != 1 || receipt.Transitions[0] != found.Key+": Start progress" {
			t.Errorf("transitions = %v, want the commit's command", receipt.Transitions)
		}

		after, err := ws.issues.ByKey(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status.Name != bootstrap.StatusInProgress {
			t.Errorf("status = %q, want In Progress", after.Status.Name)
		}
		// The post-function ran: starting progress assigns the author.
		if after.Assignee == nil || after.Assignee.ID != ws.actor.UserID {
			t.Errorf("assignee = %+v, want the commit's author", after.Assignee)
		}

		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.Commits) != 1 || dev.Commits[0].SHA != "abc00001" || dev.Commits[0].Repository != "acme/portal" {
			t.Errorf("commits = %+v", dev.Commits)
		}
		if len(dev.Branches) != 1 || dev.Branches[0].Name != found.Key+"-fix" {
			t.Errorf("a branch named for the issue was not attached: %+v", dev.Branches)
		}
	})

	t.Run("the same commit pushed again is not a second command", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "push", push("main", "abc00001", found.Key+" #start-progress fix it", ws.owner.Principal.User.Email))
		if len(receipt.Transitions) != 0 || len(receipt.Refused) != 0 {
			t.Errorf("receipt = %+v, want nothing acted on", receipt)
		}
	})

	t.Run("a command the workflow refuses is reported, not lost", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "push", push("main", "abc00002", found.Key+" #approve not from here", ws.owner.Principal.User.Email))
		if receipt.Commits != 1 {
			t.Errorf("the commit was not recorded: %+v", receipt)
		}
		if len(receipt.Refused) != 1 || !strings.Contains(receipt.Refused[0], "approve") {
			t.Errorf("refused = %v, want the missing transition named", receipt.Refused)
		}
	})

	t.Run("a comment command comments", func(t *testing.T) {
		deliver(t, svc, repo, "push", push("main", "abc00003", found.Key+" #comment reproduced on staging", ws.owner.Principal.User.Email))
		comments, err := ws.issues.Comments(ws.ctx, found.Key, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(comments) != 1 || !strings.Contains(string(comments[0].Body), "reproduced on staging") {
			t.Errorf("comments = %+v", comments)
		}
	})

	t.Run("a stranger's commit acts as whoever connected the repository", func(t *testing.T) {
		other := ws.newIssue(t, "somebody else's fix")
		receipt := deliver(t, svc, repo, "push", push("main", "abc00004", other.Key+" #start-progress", "stranger@elsewhere.example"))
		if len(receipt.Transitions) != 1 {
			t.Errorf("receipt = %+v, want the transition taken as the connector", receipt)
		}
	})
}

func TestAPullRequestLinksAndMergingMovesTheIssue(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "merging")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "", "Close")
	found := ws.newIssue(t, "add the export")

	pr := func(state string, merged bool) string {
		mergedAt := "null"
		if merged {
			mergedAt = `"2026-09-02T10:00:00Z"`
		}
		return `{"action":"x","pull_request":{"number":7,"title":"` + found.Key + ` add the export","html_url":"https://github.com/acme/portal/pull/7",` +
			`"state":"` + state + `","merged":` + boolString(merged) + `,"merged_at":` + mergedAt + `,"created_at":"2026-09-01T10:00:00Z",` +
			`"head":{"ref":"` + found.Key + `-export","sha":"head0001"},"base":{"ref":"main"},"user":{"login":"ada"}}}`
	}

	deliver(t, svc, repo, "pull_request", pr("open", false))
	dev, err := svc.ForIssue(ws.ctx, found.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.PullRequests) != 1 || dev.PullRequests[0].State != git.PullOpen || dev.PullRequests[0].Number != 7 {
		t.Fatalf("pull requests = %+v", dev.PullRequests)
	}
	if dev.PullRequests[0].CI != nil {
		t.Error("a pull request with no runs claims one")
	}

	t.Run("a check run against its head is its CI answer", func(t *testing.T) {
		deliver(t, svc, repo, "check_run", `{"check_run":{"id":11,"name":"build","status":"completed","conclusion":"failure",`+
			`"html_url":"https://github.com/acme/portal/runs/11","head_sha":"head0001","check_suite":{"head_branch":"`+found.Key+`-export"}}}`)
		deliver(t, svc, repo, "check_run", `{"check_run":{"id":11,"name":"build","status":"completed","conclusion":"success",`+
			`"html_url":"https://github.com/acme/portal/runs/11","head_sha":"head0001","check_suite":{"head_branch":"`+found.Key+`-export"}}}`)
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(dev.Runs) != 1 {
			t.Fatalf("runs = %+v, want the one run, updated rather than repeated", dev.Runs)
		}
		if dev.PullRequests[0].CI == nil || dev.PullRequests[0].CI.Status != git.CISuccess {
			t.Errorf("pull request CI = %+v, want the rerun's success", dev.PullRequests[0].CI)
		}
	})

	t.Run("merging takes the repository's transition", func(t *testing.T) {
		receipt := deliver(t, svc, repo, "pull_request", pr("closed", true))
		if len(receipt.Transitions) != 1 || receipt.Transitions[0] != found.Key+": Close" {
			t.Errorf("transitions = %v (refused %v)", receipt.Transitions, receipt.Refused)
		}
		after, err := ws.issues.ByKey(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status.Name != bootstrap.StatusDone {
			t.Errorf("status = %q, want Done", after.Status.Name)
		}
		dev, err := svc.ForIssue(ws.ctx, found.Key)
		if err != nil {
			t.Fatal(err)
		}
		if dev.PullRequests[0].State != git.PullMerged || dev.PullRequests[0].MergedAt == nil {
			t.Errorf("pull request = %+v, want merged", dev.PullRequests[0])
		}
	})
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestGitLabDeliveriesAreReadToo(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "gitlabbing")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitLab, stub, "", "")
	found := ws.newIssue(t, "the gitlab one")

	deliver(t, svc, repo, "Push Hook", `{"ref":"refs/heads/`+found.Key+`-work","before":"0000000000000000000000000000000000000000","after":"a1000001",`+
		`"commits":[{"id":"a1000001","message":"`+found.Key+` #start-progress","url":"u","timestamp":"2026-09-01T10:00:00Z","author":{"name":"Ada","email":"`+ws.owner.Principal.User.Email+`"}}],`+
		`"project":{"web_url":"https://gitlab.com/acme/portal"}}`)
	deliver(t, svc, repo, "Merge Request Hook", `{"user":{"name":"Ada"},"object_attributes":{"iid":3,"title":"`+found.Key+` work","url":"u","state":"opened",`+
		`"source_branch":"`+found.Key+`-work","target_branch":"main","last_commit":{"id":"a1000001"}}}`)
	deliver(t, svc, repo, "Pipeline Hook", `{"object_attributes":{"id":5,"ref":"`+found.Key+`-work","sha":"a1000001","status":"running"},"project":{"web_url":"https://gitlab.com/acme/portal"}}`)

	dev, err := svc.ForIssue(ws.ctx, found.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.Commits) != 1 || len(dev.PullRequests) != 1 || len(dev.Runs) != 1 {
		t.Fatalf("development = %+v", dev)
	}
	if dev.PullRequests[0].CI == nil || dev.PullRequests[0].CI.Status != git.CIPending {
		t.Errorf("merge request CI = %+v, want the running pipeline", dev.PullRequests[0].CI)
	}
	after, err := ws.issues.ByKey(ws.ctx, found.Key)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status.Name != bootstrap.StatusInProgress {
		t.Errorf("status = %q, want In Progress from the commit", after.Status.Name)
	}

	t.Run("the wrong token is refused", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(`{}`))
		r.Header.Set("X-Gitlab-Event", "Push Hook")
		r.Header.Set("X-Gitlab-Token", "guess")
		if _, err := svc.Receive(context.Background(), repo.ID, r, []byte(`{}`)); !errors.Is(err, git.ErrUnauthenticated) {
			t.Errorf("err = %v, want ErrUnauthenticated", err)
		}
	})
}

func TestABranchIsMadeOnTheHost(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "branching")
	stub := newStubHost(t)
	found := ws.newIssue(t, "Sign-in rejects a plus sign")

	t.Run("without a token the tracker cannot write to the host", func(t *testing.T) {
		svc, repo := ws.connect(t, h, git.GitHub, stub, "", "")
		if _, _, err := svc.CreateBranch(ws.ctx, found.Key, repo.ID, "", "", ws.actor.UserID); !errors.Is(err, git.ErrNoToken) {
			t.Errorf("err = %v, want ErrNoToken", err)
		}
		if _, err := svc.Disconnect(ws.ctx, repo.ID); err != nil {
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
		other := h.newWorkspace(t, "branchgl")
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
		other := h.newWorkspace(t, "branchgt")
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
	h := newHarness(t)
	ws := h.newWorkspace(t, "following")
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

func TestGiteaDeliveriesAreReadToo(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "gitea")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.Gitea, stub, "", "")
	found := ws.newIssue(t, "wire the portal")

	receipt := deliver(t, svc, repo, "create", `{"ref":"`+found.Key+`-wire","ref_type":"branch","sha":"c0ffee01"}`)
	if receipt.Branches != 1 {
		t.Errorf("receipt = %+v, want the branch", receipt)
	}
	receipt = deliver(t, svc, repo, "push", `{"ref":"refs/heads/`+found.Key+`-wire","before":"c0ffee01","after":"c0ffee02","commits":[{"id":"c0ffee02","message":"wire it","url":"http://localhost:3001/acme/portal/commit/c0ffee02","timestamp":"2026-09-01T10:00:00Z","author":{"name":"Ada","email":"ada@elsewhere.example"}}]}`)
	if receipt.Commits != 1 || len(receipt.Linked) != 1 {
		t.Errorf("receipt = %+v, want the commit linked through the branch", receipt)
	}
	deliver(t, svc, repo, "pull_request", pull("closed", 2, "wire it", found.Key+"-wire", "c0ffee02", true))

	dev, err := svc.ForIssue(ws.ctx, found.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(dev.Branches) != 1 || dev.Branches[0].HeadSHA != "c0ffee02" || dev.Branches[0].MergedAt == nil ||
		!strings.HasSuffix(dev.Branches[0].URL, "/src/branch/"+found.Key+"-wire") {
		t.Errorf("branch = %+v", dev.Branches)
	}
	if len(dev.PullRequests) != 1 || dev.PullRequests[0].State != git.PullMerged {
		t.Errorf("pull requests = %+v", dev.PullRequests)
	}

	t.Run("the wrong signature is refused", func(t *testing.T) {
		body := `{"ref":"x","ref_type":"branch"}`
		r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(body))
		r.Header.Set("X-Gitea-Event", "create")
		r.Header.Set("X-Gitea-Signature", git.SignGitea([]byte(body), "wrong"))
		if _, err := svc.Receive(context.Background(), repo.ID, r, []byte(body)); !errors.Is(err, git.ErrUnauthenticated) {
			t.Errorf("err = %v, want ErrUnauthenticated", err)
		}
	})
}

func TestATransitionTellsTheOpenPullRequest(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "telling")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "ghp_token", "")
	found := ws.newIssue(t, "tell the pull request")

	deliver(t, svc, repo, "pull_request", `{"action":"opened","pull_request":{"number":9,"title":"`+found.Key+` work","html_url":"u","state":"open","merged":false,`+
		`"created_at":"2026-09-01T10:00:00Z","head":{"ref":"x","sha":"h"},"base":{"ref":"main"},"user":{"login":"ada"}}}`)

	ws.move(t, h, found.Key, "Start progress")

	sync := git.NewSync(h.cluster, nil, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	sync.UseHTTPClient(stub.Client())
	payload, _ := json.Marshal(map[string]any{
		"key": found.Key, "transition": "Start progress", "fromStatus": "To Do", "toStatus": "In Progress",
	})
	if err := sync.Handle(context.Background(), events.Event{OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: payload}); err != nil {
		t.Fatalf("handle: %v", err)
	}

	comments := stub.calledWith(http.MethodPost, "/repos/acme/portal/issues/9/comments")
	if len(comments) != 1 {
		t.Fatalf("the host got %d comments, want one on pull request 9", len(comments))
	}
	if !strings.Contains(comments[0].Body, found.Key) || !strings.Contains(comments[0].Body, "In Progress") {
		t.Errorf("comment = %s, want the issue and where it moved to", comments[0].Body)
	}

	t.Run("a merged pull request is not told", func(t *testing.T) {
		deliver(t, svc, repo, "pull_request", `{"action":"closed","pull_request":{"number":9,"title":"`+found.Key+` work","html_url":"u","state":"closed","merged":true,`+
			`"merged_at":"2026-09-02T10:00:00Z","head":{"ref":"x","sha":"h"},"base":{"ref":"main"},"user":{"login":"ada"}}}`)
		if err := sync.Handle(context.Background(), events.Event{OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if got := len(stub.calledWith(http.MethodPost, "/issues/9/comments")); got != 1 {
			t.Errorf("the host now has %d comments, want still one", got)
		}
	})
}

func TestRepositoriesAreTenantScoped(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "mine")
	other := h.newWorkspace(t, "theirs")
	stub := newStubHost(t)
	svc, repo := ws.connect(t, h, git.GitHub, stub, "ghp", "")

	theirs := git.NewService(h.cluster, other.issues)
	if _, err := theirs.ByID(other.ctx, repo.ID); !errors.Is(err, git.ErrNotFound) {
		t.Errorf("another organization can read the repository: %v", err)
	}
	branch := "main"
	if _, _, err := theirs.Update(other.ctx, repo.ID, git.UpdateInput{DefaultBranch: &branch}, other.actor.UserID); !errors.Is(err, git.ErrNotFound) {
		t.Errorf("another organization can edit the repository: %v", err)
	}
	if _, err := theirs.Disconnect(other.ctx, repo.ID); !errors.Is(err, git.ErrNotFound) {
		t.Errorf("another organization can disconnect the repository: %v", err)
	}

	// A delivery to a repository that does not exist is not found, and says
	// nothing about which organizations do.
	r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(`{}`))
	if _, err := svc.Receive(context.Background(), uuid.New(), r, []byte(`{}`)); !errors.Is(err, git.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestRepositoriesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "apigit")

	made := c.post("/api/v1/projects", map[string]any{"name": "Portal", "key": "PRT"})
	if made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	filed := c.post("/api/v1/issues", map[string]any{"projectKey": "PRT", "summary": "over the api"})
	if filed.Status != http.StatusCreated {
		t.Fatalf("create issue: %d %s", filed.Status, filed.Raw)
	}
	key := filed.Body["issue"].(map[string]any)["key"].(string)

	connected := c.post("/api/v1/projects/PRT/repositories", map[string]any{"host": "github", "name": "acme/portal"})
	if connected.Status != http.StatusCreated {
		t.Fatalf("connect: %d %s", connected.Status, connected.Raw)
	}
	repo := connected.Body["repository"].(map[string]any)
	secret, _ := repo["webhookSecret"].(string)
	if secret == "" {
		t.Fatal("the connecting response carries no webhook secret")
	}
	if url, _ := connected.Body["webhookUrl"].(string); !strings.HasSuffix(url, "/api/v1/git/webhooks/"+repo["id"].(string)) {
		t.Errorf("webhook url = %q", url)
	}
	if _, has := repo["accessToken"]; has {
		t.Error("the response carries the access token field")
	}

	listed := c.get("/api/v1/projects/PRT/repositories")
	first := listed.Body["repositories"].([]any)[0].(map[string]any)
	if s, _ := first["webhookSecret"].(string); s != "" {
		t.Error("listing repositories hands the secret out again")
	}

	// The webhook itself, over HTTP with no session.
	body := push("main", "ab100001", key+" #start-progress", "nobody@example.test")
	req, _ := http.NewRequest(http.MethodPost, api.URL+"/api/v1/git/webhooks/"+repo["id"].(string), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", git.SignGitHub([]byte(body), secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: %d %s", resp.StatusCode, raw)
	}

	dev := c.get("/api/v1/issues/" + key + "/development")
	if dev.Status != http.StatusOK || len(dev.Body["commits"].([]any)) != 1 {
		t.Errorf("development = %d %s", dev.Status, dev.Raw)
	}
	shown := c.get("/api/v1/issues/" + key)
	if status := shown.Body["issue"].(map[string]any)["status"].(map[string]any)["name"]; status != bootstrap.StatusInProgress {
		t.Errorf("status over the api = %v, want In Progress", status)
	}

	t.Run("a bad signature is 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, api.URL+"/api/v1/git/webhooks/"+repo["id"].(string), bytes.NewBufferString(body))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", "sha256=nope")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("rotating the secret hands out a new one once", func(t *testing.T) {
		rotated := c.post("/api/v1/repositories/"+repo["id"].(string)+"/rotate-secret", nil)
		if rotated.Status != http.StatusOK {
			t.Fatalf("rotate: %d %s", rotated.Status, rotated.Raw)
		}
		fresh, _ := rotated.Body["repository"].(map[string]any)["webhookSecret"].(string)
		if fresh == "" || fresh == secret {
			t.Errorf("rotated secret = %q", fresh)
		}
	})
}
