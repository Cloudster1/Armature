package git

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func request(t *testing.T, headers map[string]string, body string) (*http.Request, []byte) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r, []byte(body)
}

var repo = &Repository{Name: "acme/portal", URL: "https://github.com/acme/portal"}

func TestGitHubAuthenticatesBySignature(t *testing.T) {
	body := `{"zen":"hi"}`
	r, raw := request(t, map[string]string{"X-Hub-Signature-256": SignGitHub([]byte(body), "s3cret")}, body)
	if err := (githubHost{}).Authenticate(r, raw, "s3cret"); err != nil {
		t.Errorf("a correct signature was refused: %v", err)
	}
	if err := (githubHost{}).Authenticate(r, raw, "other"); err == nil {
		t.Error("a signature made with another secret was accepted")
	}
	r, raw = request(t, nil, body)
	if err := (githubHost{}).Authenticate(r, raw, "s3cret"); err == nil {
		t.Error("a delivery with no signature was accepted")
	}
}

func TestGitHubPushBecomesCommitsAndBranches(t *testing.T) {
	r, raw := request(t, map[string]string{"X-GitHub-Event": "push"}, `{
	  "ref": "refs/heads/CP-4-fix", "created": true, "deleted": false,
	  "commits": [{"id": "abc1234", "message": "CP-4 #start-progress fix it", "url": "https://github.com/acme/portal/commit/abc1234",
	               "timestamp": "2026-09-01T10:00:00Z", "author": {"name": "Ada", "email": "ada@armature.test"}}]
	}`)
	d, err := (githubHost{}).Parse(r, raw, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Commits) != 1 || d.Commits[0].SHA != "abc1234" || d.Commits[0].Branch != "CP-4-fix" || d.Commits[0].AuthorEmail != "ada@armature.test" {
		t.Errorf("commits = %+v", d.Commits)
	}
	if len(d.Branches) != 1 || d.Branches[0].Name != "CP-4-fix" || !strings.HasSuffix(d.Branches[0].URL, "/tree/CP-4-fix") {
		t.Errorf("branches = %+v", d.Branches)
	}
}

func TestGitHubPullRequestStates(t *testing.T) {
	for _, tc := range []struct {
		body string
		want PullState
	}{
		{`{"action":"opened","pull_request":{"number":7,"title":"CP-4 fix","state":"open","merged":false,"head":{"ref":"CP-4-fix","sha":"abc"},"base":{"ref":"main"},"user":{"login":"ada"}}}`, PullOpen},
		{`{"action":"closed","pull_request":{"number":7,"title":"CP-4 fix","state":"closed","merged":true,"merged_at":"2026-09-02T10:00:00Z","head":{"ref":"CP-4-fix","sha":"abc"},"base":{"ref":"main"},"user":{"login":"ada"}}}`, PullMerged},
		{`{"action":"closed","pull_request":{"number":7,"title":"CP-4 fix","state":"closed","merged":false,"head":{"ref":"CP-4-fix","sha":"abc"},"base":{"ref":"main"},"user":{"login":"ada"}}}`, PullClosed},
	} {
		r, raw := request(t, map[string]string{"X-GitHub-Event": "pull_request"}, tc.body)
		d, err := (githubHost{}).Parse(r, raw, repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.PullRequests) != 1 || d.PullRequests[0].State != tc.want {
			t.Errorf("state = %+v, want %s", d.PullRequests, tc.want)
		}
		if tc.want == PullMerged && d.PullRequests[0].MergedAt == nil {
			t.Error("a merged pull request has no merged time")
		}
	}
}

func TestGitHubCheckRunOutcomes(t *testing.T) {
	cases := map[string]CIStatus{
		`{"check_run":{"id":1,"name":"build","status":"completed","conclusion":"success","head_sha":"abc"}}`:   CISuccess,
		`{"check_run":{"id":2,"name":"build","status":"in_progress","conclusion":null,"head_sha":"abc"}}`:      CIPending,
		`{"check_run":{"id":3,"name":"build","status":"completed","conclusion":"timed_out","head_sha":"abc"}}`: CIFailure,
		`{"check_run":{"id":4,"name":"build","status":"completed","conclusion":"cancelled","head_sha":"abc"}}`: CICancelled,
	}
	for body, want := range cases {
		r, raw := request(t, map[string]string{"X-GitHub-Event": "check_run"}, body)
		d, err := (githubHost{}).Parse(r, raw, repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Runs) != 1 || d.Runs[0].Status != want {
			t.Errorf("%s -> %+v, want %s", body, d.Runs, want)
		}
	}
}

func TestGitHubPingAndUnknownEvents(t *testing.T) {
	r, raw := request(t, map[string]string{"X-GitHub-Event": "ping"}, `{"zen":"hi"}`)
	if d, err := (githubHost{}).Parse(r, raw, repo); err != nil || len(d.Commits) != 0 {
		t.Errorf("ping = %+v, %v; want an empty delivery", d, err)
	}
	r, raw = request(t, map[string]string{"X-GitHub-Event": "star"}, `{}`)
	if _, err := (githubHost{}).Parse(r, raw, repo); err != ErrUnknownEvent {
		t.Errorf("an event we do not read gave %v, want ErrUnknownEvent", err)
	}
}

func TestGitLabAuthenticatesByToken(t *testing.T) {
	r, raw := request(t, map[string]string{"X-Gitlab-Token": "s3cret"}, `{}`)
	if err := (gitlabHost{}).Authenticate(r, raw, "s3cret"); err != nil {
		t.Errorf("the right token was refused: %v", err)
	}
	if err := (gitlabHost{}).Authenticate(r, raw, "other"); err == nil {
		t.Error("the wrong token was accepted")
	}
}

func TestGitLabHooks(t *testing.T) {
	gl := &Repository{Name: "acme/portal", URL: "https://gitlab.com/acme/portal"}

	r, raw := request(t, map[string]string{"X-Gitlab-Event": "Push Hook"}, `{
	  "ref": "refs/heads/CP-4-fix", "before": "0000000000000000000000000000000000000000", "after": "abc1234",
	  "commits": [{"id": "abc1234", "message": "CP-4 #close done", "url": "u", "timestamp": "2026-09-01T10:00:00+02:00",
	               "author": {"name": "Ada", "email": "ada@armature.test"}}],
	  "project": {"web_url": "https://gitlab.com/acme/portal"}
	}`)
	d, err := (gitlabHost{}).Parse(r, raw, gl)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Commits) != 1 || len(d.Branches) != 1 || d.Branches[0].Name != "CP-4-fix" {
		t.Errorf("push = %+v", d)
	}

	r, raw = request(t, map[string]string{"X-Gitlab-Event": "Merge Request Hook"}, `{
	  "user": {"name": "Ada"},
	  "object_attributes": {"iid": 3, "title": "CP-4 fix", "url": "u", "state": "merged", "source_branch": "CP-4-fix",
	                        "target_branch": "main", "merged_at": "2026-09-02 10:00:00 UTC", "last_commit": {"id": "abc1234"}}
	}`)
	d, err = (gitlabHost{}).Parse(r, raw, gl)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.PullRequests) != 1 || d.PullRequests[0].State != PullMerged || d.PullRequests[0].Number != 3 || d.PullRequests[0].MergedAt == nil {
		t.Errorf("merge request = %+v", d.PullRequests)
	}

	r, raw = request(t, map[string]string{"X-Gitlab-Event": "Pipeline Hook"}, `{
	  "object_attributes": {"id": 99, "ref": "CP-4-fix", "sha": "abc1234", "status": "failed"},
	  "project": {"web_url": "https://gitlab.com/acme/portal"}
	}`)
	d, err = (gitlabHost{}).Parse(r, raw, gl)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Runs) != 1 || d.Runs[0].Status != CIFailure || d.Runs[0].URL != "https://gitlab.com/acme/portal/-/pipelines/99" {
		t.Errorf("pipeline = %+v", d.Runs)
	}
}

func TestGiteaSpeaksGitHubsDialectUnderItsOwnHeaders(t *testing.T) {
	gitea := &Repository{Name: "demo/portal", URL: "http://localhost:3001/demo/portal"}
	body := `{"ref":"refs/heads/CP-4-fix","before":"0000000000000000000000000000000000000000","after":"abc1234abc1234",
	  "commits":[{"id":"abc1234abc1234","message":"fix it","url":"http://localhost:3001/demo/portal/commit/abc1234abc1234",
	  "timestamp":"2026-09-01T10:00:00Z","author":{"name":"Ada","email":"ada@armature.test"}}]}`
	r, raw := request(t, map[string]string{"X-Gitea-Event": "push", "X-Gitea-Signature": SignGitea([]byte(body), "s3cret")}, body)
	if err := (giteaHost{}).Authenticate(r, raw, "s3cret"); err != nil {
		t.Errorf("a correct signature was refused: %v", err)
	}
	if err := (giteaHost{}).Authenticate(r, raw, "other"); err == nil {
		t.Error("a signature made with another secret was accepted")
	}
	d, err := (giteaHost{}).Parse(r, raw, gitea)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Branches) != 1 || !d.Branches[0].created || d.Branches[0].HeadSHA != "abc1234abc1234" ||
		d.Branches[0].URL != "http://localhost:3001/demo/portal/src/branch/CP-4-fix" {
		t.Errorf("a first push is the branch's creation, at the host's address: %+v", d.Branches)
	}
	if len(d.Commits) != 1 || d.Commits[0].Branch != "CP-4-fix" {
		t.Errorf("commits = %+v", d.Commits)
	}

	r, raw = request(t, map[string]string{"X-Gitea-Event": "create"}, `{"ref":"CP-5-other","ref_type":"branch","sha":"def5678def5678"}`)
	d, err = (giteaHost{}).Parse(r, raw, gitea)
	if err != nil || len(d.Branches) != 1 || d.Branches[0].Name != "CP-5-other" || !d.Branches[0].created {
		t.Errorf("a create event is a new branch: %+v %v", d, err)
	}
	r, raw = request(t, map[string]string{"X-Gitea-Event": "create"}, `{"ref":"v1.0","ref_type":"tag"}`)
	if d, err = (giteaHost{}).Parse(r, raw, gitea); err != nil || len(d.Branches) != 0 {
		t.Errorf("a tag is not a branch: %+v %v", d, err)
	}
	r, raw = request(t, map[string]string{"X-Gitea-Event": "delete"}, `{"ref":"CP-5-other","ref_type":"branch"}`)
	if d, err = (giteaHost{}).Parse(r, raw, gitea); err != nil || len(d.DeletedBranches) != 1 {
		t.Errorf("a delete event removes the branch: %+v %v", d, err)
	}
	r, raw = request(t, map[string]string{"X-Gitea-Event": "pull_request"},
		`{"action":"closed","pull_request":{"number":3,"title":"fix","state":"closed","merged":true,"merged_at":"2026-09-02T10:00:00Z","head":{"ref":"CP-4-fix","sha":"abc"},"base":{"ref":"main"},"user":{"login":"ada"}}}`)
	if d, err = (giteaHost{}).Parse(r, raw, gitea); err != nil || len(d.PullRequests) != 1 || d.PullRequests[0].State != PullMerged {
		t.Errorf("a merged pull request reads as merged: %+v %v", d, err)
	}
}

func TestAPushToAnyBranchCarriesItsHead(t *testing.T) {
	r, raw := request(t, map[string]string{"X-GitHub-Event": "push"},
		`{"ref":"refs/heads/main","before":"aaaaaaa","after":"bbbbbbb","created":false,"deleted":false,"commits":[]}`)
	d, err := (githubHost{}).Parse(r, raw, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Branches) != 1 || d.Branches[0].created || d.Branches[0].HeadSHA != "bbbbbbb" {
		t.Errorf("an ordinary push names the branch and its new head without claiming to have made it: %+v", d.Branches)
	}
}

func TestBranchNamesFollowGitsRules(t *testing.T) {
	for _, ok := range []string{"CP-4-fix", "feature/CP-4", "a.b", "release-1.0", "x/y/z", "UPPER_case"} {
		if !ValidBranchName(ok) {
			t.Errorf("%q should be a valid branch name", ok)
		}
	}
	for _, bad := range []string{"", "-lead", "/lead", "trail/", "has space", "two..dots", "a//b", ".hidden", "x/.y", "end.lock", "tab\tin", "q?", "star*", "brack[et", "car^et", "til~de", "col:on", "back\\slash", "at@{x", "@", "end."} {
		if ValidBranchName(bad) {
			t.Errorf("%q should not be a valid branch name", bad)
		}
	}
}
