//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// giteaServer is the stack's Gitea, reached as the administrator the stack made,
// so the git proxy is tested against a host that speaks real git.
type giteaServer struct {
	url, user, password string
	http                *http.Client
}

// giteaCallTimeout bounds one call to Gitea's API.
const giteaCallTimeout = 15 * time.Second

func stackGitea(t *testing.T) *giteaServer {
	t.Helper()
	g := &giteaServer{
		url: os.Getenv("ARMATURE_TEST_GITEA_URL"), user: os.Getenv("ARMATURE_TEST_GITEA_USER"),
		password: os.Getenv("ARMATURE_TEST_GITEA_PASSWORD"), http: &http.Client{Timeout: giteaCallTimeout},
	}
	if g.url == "" || g.user == "" {
		t.Skip("ARMATURE_TEST_GITEA_URL and ARMATURE_TEST_GITEA_USER are not set")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("the git proxy is tested with git itself, which this image lacks; make test-integration runs one that has it")
	}
	return g
}

// call sends one request to Gitea's API, signed in with the password or a token.
func (g *giteaServer) call(t *testing.T, method, path, token string, in any, out any) int {
	t.Helper()
	var body io.Reader
	if in != nil {
		raw, _ := json.Marshal(in)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, g.url+"/api/v1"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	} else {
		req.SetBasicAuth(g.user, g.password)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		t.Fatalf("gitea %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// repository makes a repository with one commit on main, and a token that
// writes to it, both gone when the test ends.
func (g *giteaServer) repository(t *testing.T, name string) (fullName, token string) {
	t.Helper()
	var made struct {
		SHA1 string `json:"sha1"`
	}
	if status := g.call(t, http.MethodPost, "/users/"+g.user+"/tokens", "", map[string]any{
		"name": name, "scopes": []string{"write:repository", "write:user"},
	}, &made); status != http.StatusCreated {
		t.Fatalf("a gitea token: %d", status)
	}
	if status := g.call(t, http.MethodPost, "/user/repos", made.SHA1, map[string]any{
		"name": name, "auto_init": true, "default_branch": "main",
	}, nil); status != http.StatusCreated {
		t.Fatalf("a gitea repository: %d", status)
	}
	t.Cleanup(func() {
		g.call(t, http.MethodDelete, "/repos/"+g.user+"/"+name, made.SHA1, nil, nil)
		g.call(t, http.MethodDelete, "/users/"+g.user+"/tokens/"+name, "", nil, nil)
	})
	return g.user + "/" + name, made.SHA1
}

// head is the commit a branch is at on the host, or empty when it is not there.
func (g *giteaServer) head(t *testing.T, fullName, token, branch string) string {
	t.Helper()
	var found struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if g.call(t, http.MethodGet, "/repos/"+fullName+"/branches/"+url.PathEscape(branch), token, nil, &found) != http.StatusOK {
		return ""
	}
	return found.Commit.ID
}

// gitRun runs git in dir with nothing of the machine's configuration and no
// prompt, answering what it printed.
func gitRun(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Rotor", "GIT_AUTHOR_EMAIL=rotor@armature.test",
		"GIT_COMMITTER_NAME=Rotor", "GIT_COMMITTER_EMAIL=rotor@armature.test",
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// gitOK runs git and fails the test when it does.
func gitOK(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitRun(t, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

// gitRefused runs git, wanting it to fail saying the phrase.
func gitRefused(t *testing.T, dir, phrase string, args ...string) {
	t.Helper()
	out, err := gitRun(t, dir, args...)
	if err == nil || !strings.Contains(out, phrase) {
		t.Errorf("git %s: %v, want it refused saying %q:\n%s", strings.Join(args, " "), err, phrase, out)
	}
}

// commitFile writes a file in a checkout and commits it.
func commitFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	gitOK(t, dir, "add", name)
	gitOK(t, dir, "commit", "-q", "-m", "Write "+name)
	return gitOK(t, dir, "rev-parse", "HEAD")
}

// observed puts the contract between git and the api: every request git makes
// is checked against the document, as the test client's are.
func observed(t *testing.T, api *apiServer) string {
	t.Helper()
	target, err := url.Parse(api.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(resp *http.Response) error {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()
		apiContract.observe(t, resp.Request.Method, resp.Request.URL.RequestURI(), resp.StatusCode, resp.Header.Get("Content-Type"), body)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	front := httptest.NewServer(proxy)
	t.Cleanup(front.Close)
	return front.URL
}
