package git

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
)

// giteaHost speaks Gitea's webhooks and API. Gitea sends GitHub's payloads
// under its own header names, so the reader is shared; the API for making a
// branch is its own.
type giteaHost struct{}

func (giteaHost) Kind() HostKind            { return Gitea }
func (giteaHost) DefaultAPIBaseURL() string { return "https://gitea.com/api/v1" }

// Authenticate checks the HMAC Gitea puts in X-Gitea-Signature, which is the
// same digest GitHub sends, without the algorithm prefix.
func (giteaHost) Authenticate(r *http.Request, body []byte, secret string) error {
	header := strings.TrimPrefix(r.Header.Get("X-Gitea-Signature"), "sha256=")
	if header == "" {
		return ErrUnauthenticated
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(header)) {
		return ErrUnauthenticated
	}
	return nil
}

// SignGitea produces the header value Gitea would send for a body.
func SignGitea(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Parse reads the same events GitHub sends, named by X-Gitea-Event.
func (giteaHost) Parse(r *http.Request, body []byte, repo *Repository) (*Delivery, error) {
	return parseGitHubDialect(r.Header.Get("X-Gitea-Event"), body, func(branch string) string {
		return repo.URL + "/src/branch/" + branch
	})
}

func (giteaHost) headers(repo *Repository) map[string]string {
	return map[string]string{"Authorization": "token " + repo.token}
}

func (giteaHost) repository(repo *Repository) string {
	return strings.TrimRight(repo.APIBaseURL, "/") + "/repos/" + repo.Name
}

// CreateBranch asks Gitea to branch off another, which it does in one call.
func (h giteaHost) CreateBranch(ctx context.Context, client *http.Client, repo *Repository, name, from string) (string, string, error) {
	var made struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	err := callHost(ctx, client, http.MethodPost, h.repository(repo)+"/branches", h.headers(repo),
		map[string]string{"new_branch_name": name, "old_branch_name": from}, &made)
	if refusedAs(err, http.StatusConflict) {
		return "", "", ErrBranchExists
	}
	if err != nil {
		return "", "", err
	}
	return repo.URL + "/src/branch/" + name, made.Commit.ID, nil
}

// CommentOnPullRequest posts to the issue comments endpoint, as on GitHub.
func (h giteaHost) CommentOnPullRequest(ctx context.Context, client *http.Client, repo *Repository, number int64, body string) error {
	url := h.repository(repo) + "/issues/" + strconv.FormatInt(number, 10) + "/comments"
	return callHost(ctx, client, http.MethodPost, url, h.headers(repo), map[string]string{"body": body}, nil)
}
