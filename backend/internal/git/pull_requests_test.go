package git

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// recordedHost answers every call with one recorded response and keeps what
// it was sent, so a test reads the request each adapter makes.
type recordedHost struct {
	*httptest.Server
	mu       sync.Mutex
	method   string
	path     string
	headers  http.Header
	body     map[string]any
	status   int
	response string
}

func newRecordedHost(t *testing.T, status int, response string) *recordedHost {
	t.Helper()
	h := &recordedHost{status: status, response: response}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.method, h.path, h.headers = r.Method, r.URL.EscapedPath(), r.Header.Clone()
		h.body = map[string]any{}
		_ = json.Unmarshal(raw, &h.body)
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(h.status)
		_, _ = w.Write([]byte(h.response))
	}))
	t.Cleanup(h.Close)
	return h
}

// sent is what the host was asked last.
func (h *recordedHost) sent() recordedCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return recordedCall{method: h.method, path: h.path, headers: h.headers, body: h.body}
}

type recordedCall struct {
	method, path string
	headers      http.Header
	body         map[string]any
}

func (h *recordedHost) repo(kind HostKind, url string) *Repository {
	return &Repository{Host: kind, Name: "acme/portal", URL: url, APIBaseURL: h.URL, DefaultBranch: "main", token: "s3cret"}
}

var openFixBranch = NewPullRequest{Branch: "CP-4-fix", Base: "main", Title: "CP-4 Accept a plus sign", Body: "Fixes the validator."}

// What GitHub answers POST /repos/{owner}/{repo}/pulls with, cut to the fields read.
const githubOpened = `{
  "url": "https://api.github.com/repos/acme/portal/pulls/42", "id": 1, "node_id": "PR_x",
  "html_url": "https://github.com/acme/portal/pull/42", "number": 42, "state": "open", "locked": false,
  "title": "CP-4 Accept a plus sign", "user": {"login": "armature-bot", "id": 7, "type": "Bot"},
  "body": "Fixes the validator.", "created_at": "2026-10-22T09:00:00Z", "updated_at": "2026-10-22T09:00:00Z",
  "closed_at": null, "merged_at": null, "draft": false, "merged": false,
  "head": {"label": "acme:CP-4-fix", "ref": "CP-4-fix", "sha": "6dcb09b5b57875f334f61aebed695e2e4193db5e"},
  "base": {"label": "acme:main", "ref": "main", "sha": "9049f1265b7d61be4a8904a9a27120d2064dab3b"}
}`

func TestGitHubOpensAPullRequest(t *testing.T) {
	host := newRecordedHost(t, http.StatusCreated, githubOpened)
	in := openFixBranch
	in.Draft = true
	pr, err := (githubHost{}).OpenPullRequest(context.Background(), host.Client(), host.repo(GitHub, "https://github.com/acme/portal"), in)
	if err != nil {
		t.Fatal(err)
	}
	sent := host.sent()
	if sent.method != http.MethodPost || sent.path != "/repos/acme/portal/pulls" {
		t.Errorf("asked %s %s, want POST /repos/acme/portal/pulls", sent.method, sent.path)
	}
	if got := sent.headers.Get("Authorization"); got != "Bearer s3cret" {
		t.Errorf("authorization = %q, want the repository's token", got)
	}
	want := map[string]any{"title": in.Title, "head": "CP-4-fix", "base": "main", "body": in.Body, "draft": true}
	for k, v := range want {
		if sent.body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, sent.body[k], v)
		}
	}
	if pr.Number != 42 || pr.URL != "https://github.com/acme/portal/pull/42" || pr.State != PullOpen ||
		pr.SourceBranch != "CP-4-fix" || pr.TargetBranch != "main" || pr.HeadSHA != "6dcb09b5b57875f334f61aebed695e2e4193db5e" ||
		pr.AuthorName != "armature-bot" || pr.OpenedAt.IsZero() {
		t.Errorf("pull request = %+v", pr)
	}
}

func TestGitHubTellsAnOpenPullRequestFromAnotherRefusal(t *testing.T) {
	exists := newRecordedHost(t, http.StatusUnprocessableEntity,
		`{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"A pull request already exists for acme:CP-4-fix."}]}`)
	_, err := (githubHost{}).OpenPullRequest(context.Background(), exists.Client(), exists.repo(GitHub, "https://github.com/acme/portal"), openFixBranch)
	if !errors.Is(err, ErrPullRequestExists) {
		t.Errorf("err = %v, want ErrPullRequestExists", err)
	}
	empty := newRecordedHost(t, http.StatusUnprocessableEntity,
		`{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"No commits between main and CP-4-fix"}]}`)
	_, err = (githubHost{}).OpenPullRequest(context.Background(), empty.Client(), empty.repo(GitHub, "https://github.com/acme/portal"), openFixBranch)
	if errors.Is(err, ErrPullRequestExists) || !errors.Is(err, ErrHost) {
		t.Errorf("err = %v, want the host's refusal", err)
	}
}

// What GitLab answers POST /projects/:id/merge_requests with, cut to the fields read.
const gitlabOpened = `{
  "id": 9001, "iid": 7, "project_id": 3, "title": "Draft: CP-4 Accept a plus sign", "description": "Fixes the validator.",
  "state": "opened", "created_at": "2026-10-22T09:00:00.000Z", "updated_at": "2026-10-22T09:00:00.000Z",
  "merged_at": null, "target_branch": "main", "source_branch": "CP-4-fix", "draft": true,
  "author": {"id": 5, "username": "armature-bot", "name": "Armature"},
  "sha": "8888888888888888888888888888888888888888", "web_url": "https://gitlab.example/acme/portal/-/merge_requests/7"
}`

func TestGitLabOpensAMergeRequest(t *testing.T) {
	host := newRecordedHost(t, http.StatusCreated, gitlabOpened)
	in := openFixBranch
	in.Draft = true
	pr, err := (gitlabHost{}).OpenPullRequest(context.Background(), host.Client(), host.repo(GitLab, "https://gitlab.example/acme/portal"), in)
	if err != nil {
		t.Fatal(err)
	}
	sent := host.sent()
	if sent.method != http.MethodPost || sent.path != "/projects/acme%2Fportal/merge_requests" {
		t.Errorf("asked %s %s, want POST /projects/acme%%2Fportal/merge_requests", sent.method, sent.path)
	}
	if got := sent.headers.Get("PRIVATE-TOKEN"); got != "s3cret" {
		t.Errorf("PRIVATE-TOKEN = %q, want the repository's token", got)
	}
	want := map[string]any{"title": "Draft: " + in.Title, "source_branch": "CP-4-fix", "target_branch": "main", "description": in.Body}
	for k, v := range want {
		if sent.body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, sent.body[k], v)
		}
	}
	if pr.Number != 7 || pr.URL != "https://gitlab.example/acme/portal/-/merge_requests/7" || pr.State != PullOpen ||
		pr.SourceBranch != "CP-4-fix" || pr.TargetBranch != "main" || pr.AuthorName != "Armature" ||
		pr.HeadSHA != "8888888888888888888888888888888888888888" {
		t.Errorf("merge request = %+v", pr)
	}

	exists := newRecordedHost(t, http.StatusConflict, `{"message":["Another open merge request already exists for this source branch: !7"]}`)
	if _, err := (gitlabHost{}).OpenPullRequest(context.Background(), exists.Client(), exists.repo(GitLab, ""), openFixBranch); !errors.Is(err, ErrPullRequestExists) {
		t.Errorf("err = %v, want ErrPullRequestExists", err)
	}
}

func TestGitLabTellsABranchThatIsThereFromAnotherRefusal(t *testing.T) {
	taken := newRecordedHost(t, http.StatusBadRequest, `{"message":"Branch already exists"}`)
	if _, _, err := (gitlabHost{}).CreateBranch(context.Background(), taken.Client(), taken.repo(GitLab, ""), "CP-4-fix", "main"); !errors.Is(err, ErrBranchExists) {
		t.Errorf("err = %v, want ErrBranchExists", err)
	}
	invalid := newRecordedHost(t, http.StatusBadRequest, `{"message":"Invalid reference name: main"}`)
	if _, _, err := (gitlabHost{}).CreateBranch(context.Background(), invalid.Client(), invalid.repo(GitLab, ""), "CP-4-fix", "nowhere"); errors.Is(err, ErrBranchExists) || !errors.Is(err, ErrHost) {
		t.Errorf("err = %v, want the host's refusal", err)
	}
}

// What Gitea answers POST /repos/{owner}/{repo}/pulls with, cut to the fields read.
const giteaOpened = `{
  "id": 11, "url": "http://gitea.example/acme/portal/pulls/3", "number": 3,
  "user": {"id": 1, "login": "demo", "full_name": ""}, "title": "WIP: CP-4 Accept a plus sign", "body": "Fixes the validator.",
  "state": "open", "html_url": "http://gitea.example/acme/portal/pulls/3", "mergeable": true, "merged": false, "merged_at": null,
  "base": {"label": "main", "ref": "main", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
  "head": {"label": "CP-4-fix", "ref": "CP-4-fix", "sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
  "created_at": "2026-10-22T11:00:00+02:00", "updated_at": "2026-10-22T11:00:00+02:00"
}`

func TestGiteaOpensAPullRequest(t *testing.T) {
	host := newRecordedHost(t, http.StatusCreated, giteaOpened)
	in := openFixBranch
	in.Draft = true
	pr, err := (giteaHost{}).OpenPullRequest(context.Background(), host.Client(), host.repo(Gitea, "http://gitea.example/acme/portal"), in)
	if err != nil {
		t.Fatal(err)
	}
	sent := host.sent()
	if sent.method != http.MethodPost || sent.path != "/repos/acme/portal/pulls" {
		t.Errorf("asked %s %s, want POST /repos/acme/portal/pulls", sent.method, sent.path)
	}
	if got := sent.headers.Get("Authorization"); got != "token s3cret" {
		t.Errorf("authorization = %q, want the repository's token", got)
	}
	want := map[string]any{"title": "WIP: " + in.Title, "head": "CP-4-fix", "base": "main", "body": in.Body}
	for k, v := range want {
		if sent.body[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, sent.body[k], v)
		}
	}
	if pr.Number != 3 || pr.URL != "http://gitea.example/acme/portal/pulls/3" || pr.State != PullOpen ||
		pr.SourceBranch != "CP-4-fix" || pr.HeadSHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || pr.AuthorName != "demo" {
		t.Errorf("pull request = %+v", pr)
	}

	exists := newRecordedHost(t, http.StatusConflict, `{"message":"pull request already exists for these targets [id: 11, issue_id: 3, head_repo_id: 1, base_repo_id: 1, head_branch: CP-4-fix, base_branch: main]"}`)
	if _, err := (giteaHost{}).OpenPullRequest(context.Background(), exists.Client(), exists.repo(Gitea, ""), openFixBranch); !errors.Is(err, ErrPullRequestExists) {
		t.Errorf("err = %v, want ErrPullRequestExists", err)
	}
}

func TestADraftIsMarkedOnce(t *testing.T) {
	if got := draftTitle("WIP: half done", giteaDraftPrefix, true); got != "WIP: half done" {
		t.Errorf("a title already marked was marked again: %q", got)
	}
	if got := draftTitle("done", gitlabDraftPrefix, false); got != "done" {
		t.Errorf("a pull request ready for review was marked a draft: %q", got)
	}
}
