package git

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// gitlabHost speaks GitLab's webhooks and API v4, for gitlab.com and for a
// self-hosted instance at the repository's own API base URL.
type gitlabHost struct{}

func (gitlabHost) Kind() HostKind            { return GitLab }
func (gitlabHost) DefaultAPIBaseURL() string { return "https://gitlab.com/api/v4" }

// Authenticate compares the token GitLab sends in X-Gitlab-Token. GitLab does
// not sign the body; the token is the whole of the proof.
func (gitlabHost) Authenticate(r *http.Request, body []byte, secret string) error {
	given := r.Header.Get("X-Gitlab-Token")
	if given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(secret)) != 1 {
		return ErrUnauthenticated
	}
	return nil
}

const zeroSHA = "0000000000000000000000000000000000000000"

type glCommit struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"`
	Author    struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
}

type glPush struct {
	Ref     string     `json:"ref"`
	Before  string     `json:"before"`
	After   string     `json:"after"`
	Commits []glCommit `json:"commits"`
	Project struct {
		WebURL string `json:"web_url"`
	} `json:"project"`
}

type glMergeRequest struct {
	User struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"user"`
	ObjectAttributes struct {
		IID          int64  `json:"iid"`
		Title        string `json:"title"`
		URL          string `json:"url"`
		State        string `json:"state"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		CreatedAt    string `json:"created_at"`
		MergedAt     string `json:"merged_at"`
		LastCommit   struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

type glPipeline struct {
	ObjectAttributes struct {
		ID         int64  `json:"id"`
		Ref        string `json:"ref"`
		SHA        string `json:"sha"`
		Status     string `json:"status"`
		CreatedAt  string `json:"created_at"`
		FinishedAt string `json:"finished_at"`
	} `json:"object_attributes"`
	Project struct {
		WebURL string `json:"web_url"`
	} `json:"project"`
}

// Parse reads Push, Merge Request and Pipeline hooks.
func (gitlabHost) Parse(r *http.Request, body []byte, repo *Repository) (*Delivery, error) {
	now := time.Now().UTC()
	switch r.Header.Get("X-Gitlab-Event") {
	case "Push Hook":
		var p glPush
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read push: %w", err)
		}
		branch := branchOf(p.Ref)
		d := &Delivery{}
		if p.After == zeroSHA {
			d.DeletedBranches = append(d.DeletedBranches, branch)
			return d, nil
		}
		d.Branches = append(d.Branches, Branch{Name: branch, URL: repo.URL + "/-/tree/" + branch, HeadSHA: p.After, created: p.Before == zeroSHA})
		for _, c := range p.Commits {
			d.Commits = append(d.Commits, Commit{
				SHA: c.ID, Message: c.Message, URL: c.URL, Branch: branch,
				AuthorName: c.Author.Name, AuthorEmail: c.Author.Email,
				CommittedAt: parseTime(c.Timestamp, now),
			})
		}
		return d, nil

	case "Merge Request Hook":
		var p glMergeRequest
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read merge request: %w", err)
		}
		a := p.ObjectAttributes
		state := PullOpen
		switch a.State {
		case "merged":
			state = PullMerged
		case "closed":
			state = PullClosed
		}
		author := p.User.Name
		if author == "" {
			author = p.User.Username
		}
		return &Delivery{PullRequests: []PullRequest{{
			Number: a.IID, Title: a.Title, URL: a.URL, State: state,
			SourceBranch: a.SourceBranch, TargetBranch: a.TargetBranch, HeadSHA: a.LastCommit.ID,
			AuthorName: author, OpenedAt: parseTime(a.CreatedAt, now), MergedAt: timePtr(a.MergedAt),
		}}}, nil

	case "Pipeline Hook":
		var p glPipeline
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read pipeline: %w", err)
		}
		a := p.ObjectAttributes
		return &Delivery{Runs: []CIRun{{
			ExternalID: "pipeline:" + strconv.FormatInt(a.ID, 10), Name: "Pipeline",
			Status: gitlabStatus(a.Status), URL: p.Project.WebURL + "/-/pipelines/" + strconv.FormatInt(a.ID, 10),
			SHA: a.SHA, Branch: a.Ref, StartedAt: parseTime(a.CreatedAt, now), FinishedAt: timePtr(a.FinishedAt),
		}}}, nil
	}
	return nil, ErrUnknownEvent
}

func gitlabStatus(status string) CIStatus {
	switch status {
	case "success", "skipped":
		return CISuccess
	case "failed":
		return CIFailure
	case "canceled", "cancelled":
		return CICancelled
	default:
		// created, pending, running, manual, waiting: not over yet.
		return CIPending
	}
}

func (gitlabHost) headers(repo *Repository) map[string]string {
	return map[string]string{"PRIVATE-TOKEN": repo.token}
}

// project is the repository as GitLab's API addresses it: the path, escaped
// into one segment.
func (gitlabHost) project(repo *Repository) string {
	return strings.TrimRight(repo.APIBaseURL, "/") + "/projects/" + url.PathEscape(repo.Name)
}

func (h gitlabHost) CreateBranch(ctx context.Context, client *http.Client, repo *Repository, name, from string) (string, string, error) {
	var made struct {
		WebURL string `json:"web_url"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	query := "?branch=" + url.QueryEscape(name) + "&ref=" + url.QueryEscape(from)
	err := callHost(ctx, client, http.MethodPost, h.project(repo)+"/repository/branches"+query, h.headers(repo), nil, &made)
	if refusedAs(err, http.StatusBadRequest) && strings.Contains(err.Error(), "already exists") {
		return "", "", ErrBranchExists
	}
	if err != nil {
		return "", "", err
	}
	if made.WebURL == "" {
		made.WebURL = repo.URL + "/-/tree/" + name
	}
	return made.WebURL, made.Commit.ID, nil
}

func (h gitlabHost) CommentOnPullRequest(ctx context.Context, client *http.Client, repo *Repository, number int64, body string) error {
	target := h.project(repo) + "/merge_requests/" + strconv.FormatInt(number, 10) + "/notes"
	return callHost(ctx, client, http.MethodPost, target, h.headers(repo), map[string]string{"body": body}, nil)
}
