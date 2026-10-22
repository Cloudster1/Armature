package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Host is what this product needs from a git host: to trust and read what it
// sends, and to make two kinds of change on it.
type Host interface {
	Kind() HostKind
	// DefaultAPIBaseURL is where the public instance's API answers.
	DefaultAPIBaseURL() string
	// Authenticate checks that a delivery came from a host holding the secret.
	Authenticate(r *http.Request, body []byte, secret string) error
	// Parse translates a delivery into commits, branches, pull requests and
	// runs. ErrUnknownEvent means the kind of event is not one we read.
	Parse(r *http.Request, body []byte, repo *Repository) (*Delivery, error)
	// CreateBranch makes a branch off another and returns where to see it and
	// the commit it starts at, when the host says.
	CreateBranch(ctx context.Context, client *http.Client, repo *Repository, name, from string) (url, head string, err error)
	// CommentOnPullRequest posts a comment on a pull or merge request.
	CommentOnPullRequest(ctx context.Context, client *http.Client, repo *Repository, number int64, body string) error
	// OpenPullRequest opens a pull or merge request and returns it as the
	// host reports it.
	OpenPullRequest(ctx context.Context, client *http.Client, repo *Repository, in NewPullRequest) (PullRequest, error)
}

// NewPullRequest is a pull request to open, from Branch into Base.
type NewPullRequest struct {
	Branch, Base, Title, Body string
	Draft                     bool
}

// HostFor returns the adapter for a kind of host.
func HostFor(kind HostKind) (Host, error) {
	switch kind {
	case GitHub:
		return githubHost{}, nil
	case GitLab:
		return gitlabHost{}, nil
	case Gitea:
		return giteaHost{}, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrBadHost, kind)
}

// hostRequestTimeout bounds a call to the host, so a slow host cannot hold a
// request or a worker indefinitely.
const hostRequestTimeout = 15 * time.Second

// hostRefusal is a non 2xx answer, kept typed so a caller can tell "already
// exists" from "not allowed" while the message still says both.
type hostRefusal struct {
	Status int
	Text   string
	// said is the start of the host's body, for an adapter to tell refusals
	// of one status apart; it never reaches the caller's answer.
	said string
}

func (e *hostRefusal) Error() string { return e.Text }
func (e *hostRefusal) Unwrap() error { return ErrHost }

// callHost sends one JSON request to the host and decodes a JSON reply. A non
// 2xx answer becomes ErrHost with the status and the start of the body, which
// is what a person needs to see to fix a token or a permission.
func callHost(ctx context.Context, client *http.Client, method, url string, headers map[string]string, in any, out any) error {
	var payload io.Reader
	if in != nil {
		body, err := json.Marshal(in)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(ctx, hostRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHost, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// What the host said is logged, not answered: the caller chose the
		// address, so its body is a way to read whatever answers there.
		return &hostRefusal{Status: resp.StatusCode, Text: fmt.Sprintf("%s: the host answered %d to %s", ErrHost, resp.StatusCode, method), said: string(raw)}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%w: unreadable reply from %s", ErrHost, url)
		}
	}
	return nil
}

// refusedAs reports whether the host answered with one of the given statuses.
func refusedAs(err error, statuses ...int) bool {
	var refusal *hostRefusal
	if !errors.As(err, &refusal) {
		return false
	}
	for _, s := range statuses {
		if refusal.Status == s {
			return true
		}
	}
	return false
}

// refusedSaying reports whether the host answered with the status and said
// the phrase, which is how a host tells "already exists" from another 422.
func refusedSaying(err error, status int, phrase string) bool {
	var refusal *hostRefusal
	return errors.As(err, &refusal) && refusal.Status == status &&
		strings.Contains(strings.ToLower(refusal.said), strings.ToLower(phrase))
}

// draftTitle marks a title as a draft the way a host without a draft field
// reads one.
func draftTitle(title, prefix string, draft bool) string {
	if !draft || strings.HasPrefix(title, prefix) {
		return title
	}
	return prefix + " " + title
}

// parseTime reads the timestamps hosts send, which are RFC 3339 with or
// without fractions, and falls back to now for one that is missing.
func parseTime(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return fallback
}

func timePtr(value string) *time.Time {
	if value == "" {
		return nil
	}
	t := parseTime(value, time.Time{})
	if t.IsZero() {
		return nil
	}
	return &t
}

// branchOf strips the refs/heads/ prefix a push carries.
func branchOf(ref string) string {
	const prefix = "refs/heads/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ref
}
