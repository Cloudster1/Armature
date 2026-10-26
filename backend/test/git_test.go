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
	// pulls numbers the pull requests it opens, as a host does.
	pulls int64
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
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			// GitHub's answer to opening one, which Gitea's matches.
			var asked struct{ Title, Head, Base string }
			_ = json.Unmarshal(body, &asked)
			s.mu.Lock()
			s.pulls++
			number := s.pulls
			s.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":` + strconv.FormatInt(number, 10) + `,"title":` + jsonString(asked.Title) +
				`,"html_url":"https://github.com/acme/portal/pull/` + strconv.FormatInt(number, 10) + `","state":"open","merged":false,` +
				`"created_at":"2026-10-22T09:00:00Z","user":{"login":"armature-bot"},"head":{"ref":` + jsonString(asked.Head) +
				`,"sha":"feed0001"},"base":{"ref":` + jsonString(asked.Base) + `}}`))
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
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "pushing")
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
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "merging")
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
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "gitlabbing")
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

func TestGiteaDeliveriesAreReadToo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "gitea")
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
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "telling")
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
	if err := sync.Handle(context.Background(), events.Event{Topic: events.TopicIssueTransitioned, Payload: payload}); err != nil {
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
		if err := sync.Handle(context.Background(), events.Event{Topic: events.TopicIssueTransitioned, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if got := len(stub.calledWith(http.MethodPost, "/issues/9/comments")); got != 1 {
			t.Errorf("the host now has %d comments, want still one", got)
		}
	})
}
