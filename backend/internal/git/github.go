package git

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// githubHost speaks GitHub's webhooks and REST API. GitHub Enterprise is the
// same product at another address, which the repository's API base URL covers.
type githubHost struct{}

func (githubHost) Kind() HostKind            { return GitHub }
func (githubHost) DefaultAPIBaseURL() string { return "https://api.github.com" }

// Authenticate checks the HMAC GitHub puts in X-Hub-Signature-256.
func (githubHost) Authenticate(r *http.Request, body []byte, secret string) error {
	header := r.Header.Get("X-Hub-Signature-256")
	if !strings.HasPrefix(header, "sha256=") {
		return ErrUnauthenticated
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(strings.TrimPrefix(header, "sha256="))) {
		return ErrUnauthenticated
	}
	return nil
}

// SignGitHub produces the header value GitHub would send for a body, for tests
// and for anybody driving the endpoint by hand.
func SignGitHub(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type ghUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type ghCommit struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"`
	Author    ghUser `json:"author"`
}

type ghPush struct {
	Ref     string `json:"ref"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Created bool   `json:"created"`
	Deleted bool   `json:"deleted"`
	// Gitea's push carries no created or deleted flag; its before and after
	// say the same thing the way GitLab's do.
	Commits    []ghCommit `json:"commits"`
	Repository struct {
		HTMLURL string `json:"html_url"`
	} `json:"repository"`
}

type ghPullEvent struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number    int64  `json:"number"`
		Title     string `json:"title"`
		HTMLURL   string `json:"html_url"`
		State     string `json:"state"`
		Merged    bool   `json:"merged"`
		MergedAt  string `json:"merged_at"`
		CreatedAt string `json:"created_at"`
		User      ghUser `json:"user"`
		Head      struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
}

type ghCheckRun struct {
	CheckRun struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		Conclusion  string `json:"conclusion"`
		HTMLURL     string `json:"html_url"`
		HeadSHA     string `json:"head_sha"`
		StartedAt   string `json:"started_at"`
		CompletedAt string `json:"completed_at"`
		CheckSuite  struct {
			HeadBranch string `json:"head_branch"`
		} `json:"check_suite"`
	} `json:"check_run"`
}

type ghWorkflowRun struct {
	WorkflowRun struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		Conclusion   string `json:"conclusion"`
		HTMLURL      string `json:"html_url"`
		HeadSHA      string `json:"head_sha"`
		HeadBranch   string `json:"head_branch"`
		RunStartedAt string `json:"run_started_at"`
		UpdatedAt    string `json:"updated_at"`
	} `json:"workflow_run"`
}

// ghRef is a create or delete event: a branch or tag came or went.
type ghRef struct {
	Ref     string `json:"ref"`
	RefType string `json:"ref_type"`
	SHA     string `json:"sha"`
}

// Parse reads push, pull_request, check_run and workflow_run deliveries. A ping
// is an empty delivery: GitHub sends one when the hook is saved, and a 2xx is
// what tells the person saving it that the address and secret are right.
func (githubHost) Parse(r *http.Request, body []byte, repo *Repository) (*Delivery, error) {
	return parseGitHubDialect(r.Header.Get("X-GitHub-Event"), body, func(branch string) string {
		return repo.URL + "/tree/" + branch
	})
}

// parseGitHubDialect reads the events GitHub and Gitea both send, given the
// event's name and how the host addresses a branch.
func parseGitHubDialect(event string, body []byte, branchURL func(string) string) (*Delivery, error) {
	now := time.Now().UTC()
	switch event {
	case "ping":
		return &Delivery{}, nil

	case "create", "delete":
		var p ghRef
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read %s: %w", event, err)
		}
		if p.RefType != "branch" {
			return &Delivery{}, nil
		}
		if event == "delete" {
			return &Delivery{DeletedBranches: []string{p.Ref}}, nil
		}
		return &Delivery{Branches: []Branch{{Name: p.Ref, URL: branchURL(p.Ref), HeadSHA: p.SHA, created: true}}}, nil

	case "push":
		var p ghPush
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read push: %w", err)
		}
		branch := branchOf(p.Ref)
		d := &Delivery{}
		if p.Deleted || p.After == zeroSHA {
			d.DeletedBranches = append(d.DeletedBranches, branch)
			return d, nil
		}
		d.Branches = append(d.Branches, Branch{
			Name: branch, URL: branchURL(branch), HeadSHA: p.After,
			created: p.Created || p.Before == zeroSHA,
		})
		for _, c := range p.Commits {
			d.Commits = append(d.Commits, Commit{
				SHA: c.ID, Message: c.Message, URL: c.URL, Branch: branch,
				AuthorName: c.Author.Name, AuthorEmail: c.Author.Email,
				CommittedAt: parseTime(c.Timestamp, now),
			})
		}
		return d, nil

	case "pull_request":
		var p ghPullEvent
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read pull request: %w", err)
		}
		pr := p.PullRequest
		state := PullOpen
		switch {
		case pr.Merged:
			state = PullMerged
		case pr.State == "closed":
			state = PullClosed
		}
		return &Delivery{PullRequests: []PullRequest{{
			Number: pr.Number, Title: pr.Title, URL: pr.HTMLURL, State: state,
			SourceBranch: pr.Head.Ref, TargetBranch: pr.Base.Ref, HeadSHA: pr.Head.SHA,
			AuthorName: pr.User.Login, OpenedAt: parseTime(pr.CreatedAt, now), MergedAt: timePtr(pr.MergedAt),
		}}}, nil

	case "check_run":
		var p ghCheckRun
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read check run: %w", err)
		}
		c := p.CheckRun
		return &Delivery{Runs: []CIRun{{
			ExternalID: "check_run:" + strconv.FormatInt(c.ID, 10), Name: c.Name,
			Status: githubStatus(c.Status, c.Conclusion), URL: c.HTMLURL, SHA: c.HeadSHA,
			Branch: c.CheckSuite.HeadBranch, StartedAt: parseTime(c.StartedAt, now), FinishedAt: timePtr(c.CompletedAt),
		}}}, nil

	case "workflow_run":
		var p ghWorkflowRun
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("read workflow run: %w", err)
		}
		w := p.WorkflowRun
		run := CIRun{
			ExternalID: "workflow_run:" + strconv.FormatInt(w.ID, 10), Name: w.Name,
			Status: githubStatus(w.Status, w.Conclusion), URL: w.HTMLURL, SHA: w.HeadSHA,
			Branch: w.HeadBranch, StartedAt: parseTime(w.RunStartedAt, now),
		}
		if run.Status != CIPending {
			run.FinishedAt = timePtr(w.UpdatedAt)
		}
		return &Delivery{Runs: []CIRun{run}}, nil
	}
	return nil, ErrUnknownEvent
}

// githubStatus folds GitHub's status and conclusion pair into one answer.
func githubStatus(status, conclusion string) CIStatus {
	if status != "completed" {
		return CIPending
	}
	switch conclusion {
	case "success", "neutral", "skipped":
		return CISuccess
	case "cancelled":
		return CICancelled
	default:
		// failure, timed_out, action_required, stale: none of them is a pass.
		return CIFailure
	}
}

func (githubHost) headers(repo *Repository) map[string]string {
	return map[string]string{
		"Authorization":        "Bearer " + repo.token,
		"X-GitHub-Api-Version": "2022-11-28",
	}
}

// CreateBranch reads the sha the base branch points at and makes a ref to it.
func (h githubHost) CreateBranch(ctx context.Context, client *http.Client, repo *Repository, name, from string) (string, string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	base := strings.TrimRight(repo.APIBaseURL, "/") + "/repos/" + repo.Name
	if err := callHost(ctx, client, http.MethodGet, base+"/git/ref/heads/"+from, h.headers(repo), nil, &ref); err != nil {
		return "", "", err
	}
	err := callHost(ctx, client, http.MethodPost, base+"/git/refs", h.headers(repo),
		map[string]string{"ref": "refs/heads/" + name, "sha": ref.Object.SHA}, nil)
	if refusedAs(err, http.StatusUnprocessableEntity) {
		return "", "", ErrBranchExists
	}
	if err != nil {
		return "", "", err
	}
	return repo.URL + "/tree/" + name, ref.Object.SHA, nil
}

// CommentOnPullRequest posts to the issue comments endpoint, which on GitHub is
// where a pull request's conversation lives.
func (h githubHost) CommentOnPullRequest(ctx context.Context, client *http.Client, repo *Repository, number int64, body string) error {
	url := strings.TrimRight(repo.APIBaseURL, "/") + "/repos/" + repo.Name + "/issues/" + strconv.FormatInt(number, 10) + "/comments"
	return callHost(ctx, client, http.MethodPost, url, h.headers(repo), map[string]string{"body": body}, nil)
}
