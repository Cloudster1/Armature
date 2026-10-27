//go:build integration

package test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/perm"
)

// A repository is behind its project's wall: somebody who holds a role in
// another project neither sees it nor changes it.
func TestARepositoryIsBehindItsProjectsWall(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	walled, elsewhere := h.projectKey(t, "WAL"), h.projectKey(t, "ELSW")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "repowall", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Walled", "key": walled}), http.StatusCreated, "the walled project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Elsewhere", "key": elsewhere}), http.StatusCreated, "another project")
	repoID := idOf(t, want(t, owner.post("/api/v1/projects/"+walled+"/repositories", map[string]any{"host": "github", "name": "acme/walled"}), http.StatusCreated, "connect"), "repository")
	stranger := api.asRole(t, h, owner, elsewhere, perm.ProjectAdministrator)

	want(t, stranger.get("/api/v1/projects/"+walled+"/repositories"), http.StatusNotFound, "listing the walled project's repositories")
	want(t, stranger.patch("/api/v1/repositories/"+repoID, map[string]any{"host": "github", "name": "acme/walled", "defaultBranch": "main"}), http.StatusNotFound, "editing the repository")
	want(t, stranger.post("/api/v1/repositories/"+repoID+"/rotate-secret", nil), http.StatusNotFound, "rotating its secret")
	want(t, stranger.delete("/api/v1/repositories/"+repoID), http.StatusNotFound, "disconnecting it")
	if listed := list(t, want(t, owner.get("/api/v1/projects/"+walled+"/repositories"), http.StatusOK, "the owner's list"), "repositories"); len(listed) != 1 {
		t.Errorf("the owner's project has %d repositories after the attempts, want its one", len(listed))
	}

	// A delivery to a repository that does not exist is not found, and says
	// nothing about which projects do.
	ws := h.newProject(t, "hookless")
	svc := git.NewService(h.cluster, ws.issues)
	r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewBufferString(`{}`))
	if _, err := svc.Receive(context.Background(), uuid.New(), r, []byte(`{}`)); !errors.Is(err, git.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestRepositoriesOverTheAPI(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	prtKey := h.projectKey(t, "PRT")
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signIn(t, h.person(t, "apigit", auth.RoleOwner))

	made := c.post("/api/v1/projects", map[string]any{"name": "Portal", "key": prtKey})
	if made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	filed := c.post("/api/v1/issues", map[string]any{"projectKey": prtKey, "summary": "over the api"})
	if filed.Status != http.StatusCreated {
		t.Fatalf("create issue: %d %s", filed.Status, filed.Raw)
	}
	key := filed.Body["issue"].(map[string]any)["key"].(string)

	connected := c.post("/api/v1/projects/"+prtKey+"/repositories", map[string]any{"host": "github", "name": "acme/portal"})
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

	listed := c.get("/api/v1/projects/" + prtKey + "/repositories")
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
	// The delivery carries no session, so nothing holds this client's reads to
	// its writes; a replica may not have replayed the transition yet.
	h.waitForPrimary(t)

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
