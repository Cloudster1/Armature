//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/httpapi"
	"github.com/armature/armature/backend/internal/perm"
)

// A partner such as Rotor clones and pushes through Armature as the person: the
// host sees Armature's token alone, and only the issue's own branch moves.
func TestAPartnerClonesAndPushesThroughTheGitProxy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	gitea := stackGitea(t)
	front := observed(t, api)
	key := h.projectKey(t, "GPX")

	admin := api.client(t)
	admin.signIn(t, h.person(t, "gitproxy", auth.RoleAdmin))
	want(t, admin.post("/api/v1/projects", map[string]any{"name": "Proxied " + key, "key": key}), http.StatusCreated, "a project")
	issueKey := obj(t, want(t, admin.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Accept a plus sign"}), http.StatusCreated, "an issue"), "issue")["key"].(string)
	repoName, hostToken := gitea.repository(t, strings.ToLower(h.name(t, "proxied")))
	repoID := idOf(t, want(t, admin.post("/api/v1/projects/"+key+"/repositories", map[string]any{
		"host": "gitea", "name": repoName, "url": gitea.url + "/" + repoName, "apiBaseUrl": gitea.url + "/api/v1", "accessToken": hostToken,
	}), http.StatusCreated, "connecting the repository"), "repository")
	branch := obj(t, want(t, admin.post("/api/v1/issues/"+issueKey+"/branches", map[string]any{"repository": repoName}),
		http.StatusCreated, "the issue's branch"), "branch")["name"].(string)

	rotor := register(t, api, h.name(t, "Rotor"), "read issue.write git.write")
	want(t, admin.post("/api/v1/oauth/clients/"+rotor.id+"/approve", nil), http.StatusOK, "approving Rotor")
	developer := appToken(t, rotor, api.asRole(t, h, admin, key, perm.User), "read issue.write git.write").(string)
	unscoped := appToken(t, rotor, api.asRole(t, h, admin, key, perm.User), "read issue.write").(string)
	reader := appToken(t, rotor, api.asRole(t, h, admin, key, perm.Reader), "read issue.write git.write").(string)

	remote := front + "/api/v1/git/" + key + "/" + repoName + ".git"
	header := func(token string) []string { return []string{"-c", "http.extraHeader=Authorization: Bearer " + token} }
	work := t.TempDir()
	checkout := filepath.Join(work, "checkout")
	withToken := func(token string, args ...string) []string { return append(header(token), args...) }

	t.Run("a clone through Armature", func(t *testing.T) {
		gitOK(t, work, withToken(developer, "clone", "-q", remote, checkout)...)
		if out := gitOK(t, checkout, "ls-files"); !strings.Contains(out, "README.md") {
			t.Errorf("the clone holds %q, want the repository's README", out)
		}
		// git also sends a token as a password, which is what a credential helper does.
		withPassword := strings.Replace(remote, "://", "://rotor:"+developer+"@", 1)
		gitOK(t, work, "clone", "-q", withPassword, filepath.Join(work, "by-password"))
	})

	t.Run("a push to the issue's branch reaches the host", func(t *testing.T) {
		gitOK(t, checkout, "checkout", "-q", "-b", branch, "origin/"+branch)
		head := commitFile(t, checkout, "plus.go", "package plus\n")
		gitOK(t, checkout, withToken(developer, "push", "-q", "origin", branch)...)
		if got := gitea.head(t, repoName, hostToken, branch); got != head {
			t.Errorf("the host has %s at %q, want %q", branch, got, head)
		}
		var data []byte
		if err := h.super.QueryRow(context.Background(), `
			SELECT data FROM audit_log WHERE action = 'git.push' AND target_id = $1 AND data->>'outcome' = 'forwarded'`, repoID).Scan(&data); err != nil {
			t.Fatalf("the push is not in the audit log: %v", err)
		}
		var logged struct {
			App  struct{ Name string }
			Refs []git.PushedRef
		}
		_ = json.Unmarshal(data, &logged)
		if logged.App.Name != rotor.name || len(logged.Refs) != 1 || logged.Refs[0].Ref != "refs/heads/"+branch || logged.Refs[0].Issue != issueKey || logged.Refs[0].New != head {
			t.Errorf("the audit log says %s", data)
		}
	})

	mainBefore := gitea.head(t, repoName, hostToken, "main")
	t.Run("the default branch is refused", func(t *testing.T) {
		gitOK(t, checkout, "checkout", "-q", "main")
		commitFile(t, checkout, "straight.txt", "to main\n")
		gitRefused(t, checkout, "main is the default branch", withToken(developer, "push", "origin", "main")...)
		if got := gitea.head(t, repoName, hostToken, "main"); got != mainBefore {
			t.Errorf("main moved on the host to %s", got)
		}
	})

	t.Run("a branch not made for an issue is refused", func(t *testing.T) {
		gitRefused(t, checkout, "was not made for an issue", withToken(developer, "push", "origin", "HEAD:refs/heads/somebodys-branch")...)
		if gitea.head(t, repoName, hostToken, "somebodys-branch") != "" {
			t.Error("the branch was made on the host")
		}
	})

	t.Run("a force push, a tag and a deletion are refused", func(t *testing.T) {
		gitOK(t, checkout, "checkout", "-q", branch)
		pushed := gitOK(t, checkout, "rev-parse", "HEAD")
		gitOK(t, checkout, "commit", "-q", "--amend", "-m", "Rewritten")
		gitRefused(t, checkout, "never force-pushes", withToken(developer, "push", "--force", "origin", branch)...)
		if got := gitea.head(t, repoName, hostToken, branch); got != pushed {
			t.Errorf("the force push moved %s to %s", branch, got)
		}
		gitOK(t, checkout, "tag", "v1")
		gitRefused(t, checkout, "Tags are not pushed", withToken(developer, "push", "origin", "v1")...)
		gitRefused(t, checkout, "does not delete branches", withToken(developer, "push", "origin", ":"+branch)...)
	})

	t.Run("without git.write the app clones and does not push", func(t *testing.T) {
		gitOK(t, work, withToken(unscoped, "clone", "-q", remote, filepath.Join(work, "unscoped"))...)
		gitRefused(t, checkout, "allow it to push to issue branches", withToken(unscoped, "push", "origin", branch)...)
	})

	t.Run("a reader clones and does not push", func(t *testing.T) {
		gitOK(t, work, withToken(reader, "clone", "-q", remote, filepath.Join(work, "reader"))...)
		gitRefused(t, checkout, "You may not edit the issues of "+key, withToken(reader, "push", "origin", branch)...)
	})

	t.Run("a read-only key does not push", func(t *testing.T) {
		made := want(t, admin.post("/api/v1/tokens", map[string]any{"name": h.name(t, "reads"), "scopes": []string{"read"}}), http.StatusCreated, "a read-only key")
		gitRefused(t, checkout, "can only read", withToken(principalField(t, made, "token", "secret").(string), "push", "origin", branch)...)
	})

	t.Run("nobody, an outsider and another project are refused", func(t *testing.T) {
		gitRefused(t, work, "could not read Username", "clone", "-q", remote, filepath.Join(work, "anonymous"))
		outsider := api.client(t)
		outsider.signIn(t, h.outsider(t, "gitstranger"))
		stranger := principalField(t, want(t, outsider.post("/api/v1/tokens", map[string]any{"name": h.name(t, "stranger")}), http.StatusCreated, "a key"), "token", "secret").(string)
		gitRefused(t, work, "was not found in "+key, withToken(stranger, "clone", "-q", remote, filepath.Join(work, "stranger"))...)
	})

	t.Run("pushes are braked per token", func(t *testing.T) {
		c := bearer(api, t, developer)
		post := func() int {
			req, _ := http.NewRequest(http.MethodPost, api.URL+"/api/v1/git/"+key+"/"+repoName+".git/git-receive-pack", strings.NewReader("0000"))
			req.Header.Set("Authorization", "Bearer "+developer)
			resp, _ := c.raw(req)
			return resp.StatusCode
		}
		// The pushes above count too, so the brake holds within the limit.
		for range httpapi.GitPushesPerMinute {
			if post() == http.StatusTooManyRequests {
				return
			}
		}
		t.Errorf("%d pushes in a minute were all let through", httpapi.GitPushesPerMinute+1)
	})
}
