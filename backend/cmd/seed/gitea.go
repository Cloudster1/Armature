package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/issue"
)

// giteaDemo is how the seed reaches the stack's own git host. All of it comes
// from the environment, and none of it set means the stack runs without one.
type giteaDemo struct {
	// url is where the api reaches Gitea; publicURL is where a browser does.
	url, publicURL string
	user, password string
	// webhookBase is where Gitea reaches the api.
	webhookBase string
	repository  string
	client      *http.Client
}

const giteaDemoRepository = "customer-portal"

func giteaFromEnv() *giteaDemo {
	url := strings.TrimRight(os.Getenv("ARMATURE_SEED_GITEA_URL"), "/")
	if url == "" {
		return nil
	}
	public := strings.TrimRight(os.Getenv("ARMATURE_SEED_GITEA_PUBLIC_URL"), "/")
	if public == "" {
		public = url
	}
	return &giteaDemo{
		url: url, publicURL: public,
		user: os.Getenv("ARMATURE_SEED_GITEA_USER"), password: os.Getenv("ARMATURE_SEED_GITEA_PASSWORD"),
		webhookBase: strings.TrimRight(os.Getenv("ARMATURE_SEED_WEBHOOK_BASE_URL"), "/"),
		repository:  giteaDemoRepository,
		client:      &http.Client{Timeout: 20 * time.Second},
	}
}

// call sends one request to Gitea as the demo user and decodes the answer.
// The statuses listed as tolerated are returned as a status without an error,
// which is how "already there" is told apart from "refused".
func (g *giteaDemo) call(ctx context.Context, method, path string, in, out any, tolerated ...int) (int, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.url+"/api/v1"+path, body)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(g.user, g.password)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	for _, status := range tolerated {
		if resp.StatusCode == status {
			return resp.StatusCode, nil
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("gitea %s %s answered %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("gitea %s %s: unreadable answer", method, path)
		}
	}
	return resp.StatusCode, nil
}

// seedGitea connects the demo project to a repository on the stack's own
// Gitea, so that making a branch from a ticket makes a real one and pushing
// to it comes back through a real webhook. Without a Gitea in the environment
// it does nothing and says so. It is safe to run again.
func seedGitea(ctx context.Context, cluster *db.Cluster, issues *issue.Service, projectKey string, keys map[string]string, actor issue.Actor, log *slog.Logger) error {
	g := giteaFromEnv()
	if g == nil {
		log.Info("no gitea in the environment, the demo repository stays a recording")
		return nil
	}
	if err := g.waitReady(ctx); err != nil {
		return err
	}

	// The repository, with a first commit so there is a main to branch from.
	status, err := g.call(ctx, http.MethodPost, "/user/repos", map[string]any{
		"name": g.repository, "auto_init": true, "default_branch": "main",
		"description": "The account area customers sign in to.",
	}, nil, http.StatusConflict)
	if err != nil {
		return fmt.Errorf("make the demo repository on gitea: %w", err)
	}
	fresh := status != http.StatusConflict

	// A token of our own for the tracker to write with. Tokens cannot be read
	// back, so an earlier run's is replaced rather than reused.
	const tokenName = "armature"
	if _, err := g.call(ctx, http.MethodDelete, "/users/"+g.user+"/tokens/"+tokenName, nil, nil, http.StatusNotFound, http.StatusUnprocessableEntity); err != nil {
		return fmt.Errorf("replace the tracker's gitea token: %w", err)
	}
	var token struct {
		SHA1 string `json:"sha1"`
	}
	if _, err := g.call(ctx, http.MethodPost, "/users/"+g.user+"/tokens", map[string]any{
		"name": tokenName, "scopes": []string{"write:repository", "write:issue"},
	}, &token); err != nil {
		return fmt.Errorf("make the tracker's gitea token: %w", err)
	}

	repos := git.NewService(cluster, issues)
	name := g.user + "/" + g.repository
	repo, _, err := repos.Connect(ctx, projectKey, git.ConnectInput{
		Host: git.Gitea, Name: name, URL: g.publicURL + "/" + name, APIBaseURL: g.url + "/api/v1",
		AccessToken: token.SHA1, TransitionOnMerge: "Close",
	}, actor.UserID)
	if errors.Is(err, git.ErrNameTaken) {
		repo, err = existingRepository(ctx, repos, projectKey, name)
		if err == nil {
			t := token.SHA1
			repo, _, err = repos.Update(ctx, repo.ID, git.UpdateInput{AccessToken: &t}, actor.UserID)
		}
	}
	if err != nil {
		return fmt.Errorf("connect the gitea repository: %w", err)
	}
	// The secret is handed out once, on connecting or rotating; the webhook
	// on the host is made afresh with a fresh secret each run, so the two
	// never drift apart.
	if repo.WebhookSecret == "" {
		repo, _, err = repos.RotateSecret(ctx, repo.ID, actor.UserID)
		if err != nil {
			return fmt.Errorf("rotate the gitea repository's secret: %w", err)
		}
	}
	if err := g.pointWebhookAt(ctx, g.webhookBase+"/api/v1/git/webhooks/"+repo.ID.String(), repo.WebhookSecret); err != nil {
		return err
	}

	// A branch for one demo issue, and a commit on it, so the ticket shows
	// what the connection does the moment the stack is up. The webhook the
	// commit fires reaches the api if it is running, which on `make seed` it
	// is; the branch itself is recorded here either way.
	if fresh {
		if err := g.demoBranch(ctx, repos, repo, keys["Sign-in rejects valid passwords containing a plus sign"], actor); err != nil {
			return err
		}
	}
	log.Info("connected the demo project to gitea", "repository", name, "url", g.publicURL+"/"+name)
	return nil
}

// waitReady gives Gitea a moment: on a fresh stack it may still be starting.
func (g *giteaDemo) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		var me struct {
			Login string `json:"login"`
		}
		_, err := g.call(ctx, http.MethodGet, "/user", nil, &me)
		if err == nil && me.Login != "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gitea at %s did not answer as %s: %w", g.url, g.user, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// pointWebhookAt leaves exactly one webhook of ours on the repository, aimed
// at the api with the secret the tracker holds.
func (g *giteaDemo) pointWebhookAt(ctx context.Context, url, secret string) error {
	path := "/repos/" + g.user + "/" + g.repository + "/hooks"
	var hooks []struct {
		ID     int64 `json:"id"`
		Config struct {
			URL string `json:"url"`
		} `json:"config"`
	}
	if _, err := g.call(ctx, http.MethodGet, path, nil, &hooks); err != nil {
		return fmt.Errorf("list the repository's webhooks: %w", err)
	}
	for _, h := range hooks {
		if strings.Contains(h.Config.URL, "/api/v1/git/webhooks/") {
			if _, err := g.call(ctx, http.MethodDelete, fmt.Sprintf("%s/%d", path, h.ID), nil, nil); err != nil {
				return fmt.Errorf("remove the old webhook: %w", err)
			}
		}
	}
	_, err := g.call(ctx, http.MethodPost, path, map[string]any{
		"type": "gitea", "active": true,
		"events": []string{"push", "create", "delete", "pull_request"},
		"config": map[string]string{"url": url, "content_type": "json", "secret": secret},
	}, nil)
	if err != nil {
		return fmt.Errorf("point the repository's webhook at the api: %w", err)
	}
	return nil
}

// demoBranch makes the branch a person would make from the ticket, then
// commits on it as they would.
func (g *giteaDemo) demoBranch(ctx context.Context, repos *git.Service, repo *git.Repository, key string, actor issue.Actor) error {
	if key == "" {
		return nil
	}
	made, _, err := repos.CreateBranch(ctx, key, repo.ID, "", "", actor.UserID)
	if errors.Is(err, git.ErrBranchExists) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("make the demo branch: %w", err)
	}
	content := base64.StdEncoding.EncodeToString([]byte("The validator now accepts a plus sign in a password.\n"))
	_, err = g.call(ctx, http.MethodPost, "/repos/"+g.user+"/"+g.repository+"/contents/CHANGES.md", map[string]any{
		"content": content, "branch": made.Name,
		"message": "Stop the validator rejecting a plus sign\n\nA plus sign is a valid character in a password.",
	}, nil, http.StatusUnprocessableEntity)
	if err != nil {
		return fmt.Errorf("commit on the demo branch: %w", err)
	}
	return nil
}

// existingRepository finds a repository already connected by name.
func existingRepository(ctx context.Context, repos *git.Service, projectKey, name string) (*git.Repository, error) {
	found, err := repos.List(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	for i := range found {
		if strings.EqualFold(found[i].Name, name) {
			return &found[i], nil
		}
	}
	return nil, git.ErrNotFound
}
