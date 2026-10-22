//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/perm"
)

// mcpTool calls one tool over POST /mcp and answers its result.
func mcpTool(t *testing.T, c *client, name string, args map[string]any) map[string]any {
	t.Helper()
	resp := c.post("/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args}})
	if resp.Status != http.StatusOK {
		t.Fatalf("%s answered %d: %s", name, resp.Status, resp.Raw)
	}
	if e, ok := resp.Body["error"]; ok && e != nil {
		t.Fatalf("%s: rpc error %v", name, e)
	}
	return resp.Body["result"].(map[string]any)
}

func toolText(res map[string]any) string {
	return res["content"].([]any)[0].(map[string]any)["text"].(string)
}

// toolDone is a tool result that is not an error, with what it answered.
func toolDone(t *testing.T, res map[string]any, what string) map[string]any {
	t.Helper()
	if res["isError"] == true {
		t.Fatalf("%s: %s", what, toolText(res))
	}
	return res["structuredContent"].(map[string]any)
}

// toolRefused is a tool result that is an error saying the phrase.
func toolRefused(t *testing.T, res map[string]any, phrase, what string) {
	t.Helper()
	if res["isError"] != true || !strings.Contains(toolText(res), phrase) {
		t.Errorf("%s: %v, want it refused saying %q", what, res, phrase)
	}
}

// A partner such as Rotor branches and opens a pull request for an issue as
// the person, and the host only ever sees the repository's token.
func TestAPartnerBranchesAndOpensAPullRequestOverMCP(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	stub := newStubHost(t)
	api.api.Git.UseHTTPClient(stub.Client())
	key, elsewhere := h.projectKey(t, "GMC"), h.projectKey(t, "GMX")

	admin := api.client(t)
	admin.signIn(t, h.person(t, "gitmcp", auth.RoleAdmin))
	for _, p := range []string{key, elsewhere} {
		want(t, admin.post("/api/v1/projects", map[string]any{"name": "Git " + p, "key": p}), http.StatusCreated, "a project")
	}
	issueKey := obj(t, want(t, admin.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Accept a plus sign"}), http.StatusCreated, "an issue"), "issue")["key"].(string)
	attach := func(project, host, name, token string) string {
		return idOf(t, want(t, admin.post("/api/v1/projects/"+project+"/repositories", map[string]any{
			"host": host, "name": name, "apiBaseUrl": stub.URL, "accessToken": token,
		}), http.StatusCreated, "connecting "+name), "repository")
	}
	attach(key, "github", "acme/portal", "ghp_token")
	attach(key, "gitea", "acme/tokenless", "")
	theirs := attach(elsewhere, "github", "acme/theirs", "ghp_theirs")

	rotor := register(t, api, h.name(t, "Rotor"), "read issue.write")
	want(t, admin.post("/api/v1/oauth/clients/"+rotor.id+"/approve", nil), http.StatusOK, "approving Rotor")
	developer := bearer(api, t, appToken(t, rotor, api.asRole(t, h, admin, key, perm.User), "read issue.write"))
	reader := bearer(api, t, appToken(t, rotor, api.asRole(t, h, admin, key, perm.Reader), "read issue.write"))
	readOnly := bearer(api, t, appToken(t, rotor, api.asRole(t, h, admin, key, perm.User), "read"))

	t.Run("the repositories are listed without their tokens", func(t *testing.T) {
		res := mcpTool(t, developer, "list_repositories", map[string]any{"projectKey": key})
		listed := toolDone(t, res, "list_repositories")["repositories"].([]any)
		hasToken := map[string]any{}
		for _, each := range listed {
			r := each.(map[string]any)
			hasToken[r["name"].(string)] = r["hasToken"]
		}
		if len(listed) != 2 || hasToken["acme/portal"] != true || hasToken["acme/tokenless"] != false {
			t.Errorf("repositories = %v", hasToken)
		}
		if strings.Contains(toolText(res), "ghp_token") || strings.Contains(toolText(res), "webhookSecret") {
			t.Errorf("the list carries a secret: %s", toolText(res))
		}
	})

	var branch string
	t.Run("a branch is made for the issue with the repository's token", func(t *testing.T) {
		made := toolDone(t, mcpTool(t, developer, "create_issue_branch", map[string]any{"issueKey": issueKey, "repository": "acme/portal"}), "create_issue_branch")
		branch = made["branch"].(map[string]any)["name"].(string)
		if branch != issueKey+"-accept-a-plus-sign" {
			t.Errorf("branch = %q, want the name the issue suggests", branch)
		}
		calls := stub.calledWith(http.MethodPost, "/repos/acme/portal/git/refs")
		if len(calls) != 1 || calls[0].Auth != "Bearer ghp_token" {
			t.Errorf("the host was asked %+v, want one ref made with the repository's token", calls)
		}
	})

	t.Run("the default branch is never the issue's", func(t *testing.T) {
		toolRefused(t, mcpTool(t, developer, "create_issue_branch", map[string]any{"issueKey": issueKey, "repository": "acme/portal", "name": "main"}),
			"default branch", "a branch named main")
		toolRefused(t, mcpTool(t, developer, "open_pull_request", map[string]any{"issueKey": issueKey, "repository": "acme/portal", "branch": "main", "title": "Straight to main"}),
			"default branch", "a pull request from main")
	})

	t.Run("a branch not made for the issue takes no pull request", func(t *testing.T) {
		toolRefused(t, mcpTool(t, developer, "open_pull_request", map[string]any{"issueKey": issueKey, "repository": "acme/portal", "branch": "somebodys-branch", "title": "Not ours"}),
			"not made for this issue", "a pull request from another branch")
		if calls := stub.calledWith(http.MethodPost, "/pulls"); len(calls) != 0 {
			t.Errorf("the host was asked to open %d pull requests anyway", len(calls))
		}
	})

	t.Run("the pull request is opened and recorded on the issue", func(t *testing.T) {
		opened := toolDone(t, mcpTool(t, developer, "open_pull_request", map[string]any{
			"issueKey": issueKey, "repository": "acme/portal", "branch": branch, "title": issueKey + " Accept a plus sign", "body": "Fixes the validator.", "draft": true,
		}), "open_pull_request")["pullRequest"].(map[string]any)
		if opened["state"] != "open" || opened["sourceBranch"] != branch || opened["targetBranch"] != "main" || opened["repository"] != "acme/portal" || opened["url"] == "" {
			t.Errorf("pull request = %v", opened)
		}
		calls := stub.calledWith(http.MethodPost, "/repos/acme/portal/pulls")
		if len(calls) != 1 || calls[0].Auth != "Bearer ghp_token" {
			t.Fatalf("the host was asked %+v, want one pull request opened with the repository's token", calls)
		}
		var asked map[string]any
		if err := json.Unmarshal([]byte(calls[0].Body), &asked); err != nil {
			t.Fatal(err)
		}
		if asked["head"] != branch || asked["base"] != "main" || asked["draft"] != true || asked["body"] != "Fixes the validator." {
			t.Errorf("the host was asked for %v", asked)
		}

		dev := toolDone(t, mcpTool(t, developer, "get_issue_development", map[string]any{"issueKey": issueKey}), "get_issue_development")
		pulls := dev["pullRequests"].([]any)
		if len(pulls) != 1 || pulls[0].(map[string]any)["number"] != opened["number"] {
			t.Errorf("the issue's pull requests = %v, want the one just opened", pulls)
		}
		if branches := dev["branches"].([]any); len(branches) != 1 || branches[0].(map[string]any)["name"] != branch {
			t.Errorf("the issue's branches = %v", branches)
		}
	})

	t.Run("over HTTP too, by the repository's id", func(t *testing.T) {
		repos := list(t, want(t, developer.get("/api/v1/projects/"+key+"/repositories"), http.StatusOK, "the repositories"), "repositories")
		portal := find(t, repos, "name", "acme/portal")["id"].(string)
		second := obj(t, want(t, developer.post("/api/v1/issues/"+issueKey+"/branches", map[string]any{"repositoryId": portal, "name": "second-try"}), http.StatusCreated, "a second branch"), "branch")
		resp := want(t, developer.post("/api/v1/issues/"+issueKey+"/pull-requests", map[string]any{
			"repositoryId": portal, "branch": second["name"], "title": "A second try",
		}), http.StatusCreated, "a pull request over HTTP")
		if obj(t, resp, "pullRequest")["sourceBranch"] != "second-try" {
			t.Errorf("pull request = %s", resp.Raw)
		}
		refused := developer.post("/api/v1/issues/"+issueKey+"/pull-requests", map[string]any{"repositoryId": portal, "repository": "acme/portal", "branch": "second-try", "title": "Twice named"})
		if refused.Status != http.StatusBadRequest {
			t.Errorf("a repository named twice: %d %s", refused.Status, refused.Raw)
		}
	})

	t.Run("a repository without a token says so", func(t *testing.T) {
		for tool, args := range map[string]map[string]any{
			"create_issue_branch": {"issueKey": issueKey, "repository": "acme/tokenless"},
			"open_pull_request":   {"issueKey": issueKey, "repository": "acme/tokenless", "branch": branch, "title": "No token"},
		} {
			toolRefused(t, mcpTool(t, developer, tool, args), "This repository has no access token, so Armature cannot write to it.", tool)
		}
	})

	t.Run("another project's repository is not there", func(t *testing.T) {
		toolRefused(t, mcpTool(t, developer, "open_pull_request", map[string]any{"issueKey": issueKey, "repositoryId": theirs, "branch": branch, "title": "Elsewhere"}),
			"That repository was not found.", "a pull request into another project's repository")
		toolRefused(t, mcpTool(t, developer, "create_issue_branch", map[string]any{"issueKey": issueKey, "repository": "acme/theirs"}),
			"That repository was not found.", "a branch in another project's repository by name")
	})

	t.Run("a reader is refused, and so is an app allowed to read only", func(t *testing.T) {
		before := len(stub.calledWith(http.MethodPost, ""))
		toolDone(t, mcpTool(t, reader, "get_issue_development", map[string]any{"issueKey": issueKey}), "a reader reading")
		toolRefused(t, mcpTool(t, reader, "create_issue_branch", map[string]any{"issueKey": issueKey, "repository": "acme/portal", "name": "readers-branch"}),
			"issue.write permission", "a reader making a branch")
		toolRefused(t, mcpTool(t, reader, "open_pull_request", map[string]any{"issueKey": issueKey, "repository": "acme/portal", "branch": branch, "title": "From a reader"}),
			"issue.write permission", "a reader opening a pull request")
		toolRefused(t, mcpTool(t, readOnly, "open_pull_request", map[string]any{"issueKey": issueKey, "repository": "acme/portal", "branch": branch, "title": "Read only"}),
			"allowed to read only", "an app allowed to read only")
		if resp := reader.post("/api/v1/issues/"+issueKey+"/pull-requests", map[string]any{"repository": "acme/portal", "branch": branch, "title": "From a reader"}); resp.Status != http.StatusForbidden {
			t.Errorf("a reader over HTTP: %d %s", resp.Status, resp.Raw)
		}
		if after := len(stub.calledWith(http.MethodPost, "")); after != before {
			t.Errorf("the host was written to %d times by somebody refused", after-before)
		}
	})

	// The rule lives in the database: the default branch cannot be mapped to
	// an issue whichever way the row is written.
	t.Run("the database refuses the default branch as the issue's", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO git_branch (repository_id, name, issue_id)
			SELECT r.id, r.default_branch, i.id FROM git_repository r
			JOIN project p ON p.id = r.project_id JOIN issue i ON i.project_id = p.id
			WHERE p.key = $1 AND r.name = 'acme/portal'`, key)
		if err == nil || !strings.Contains(err.Error(), "never an issue's branch") {
			t.Errorf("the default branch was written as the issue's: %v", err)
		}
	})
}

// appToken walks the consent for scope as browser and answers the access token.
func appToken(t *testing.T, p partner, browser *client, scope string) any {
	t.Helper()
	return connect(t, p, browser, scope)["access_token"]
}
