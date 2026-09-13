// Package git connects a project to the repositories its work lands in.
//
// The connection runs both ways. Inbound, the host tells us about commits,
// branches, pull requests and CI runs, and anything that names an issue by key
// is linked to it; a commit message can also drive the workflow. Outbound, the
// tracker creates a branch for an issue and tells a pull request when the issue
// it is about moves.
package git

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// HostKind is which product the repository lives on. They differ in webhook
// shape, in how they authenticate a delivery, and in their API.
type HostKind string

const (
	GitHub HostKind = "github"
	GitLab HostKind = "gitlab"
	// Gitea is the self-hosted host, which speaks GitHub's dialect closely
	// enough to share most of its reader.
	Gitea HostKind = "gitea"
)

func (h HostKind) Valid() bool { return h == GitHub || h == GitLab || h == Gitea }

// Repository is a repository connected to a project.
type Repository struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`
	Host       HostKind  `json:"host"`
	// Name is "owner/name" on the host.
	Name          string `json:"name"`
	URL           string `json:"url"`
	APIBaseURL    string `json:"apiBaseUrl"`
	DefaultBranch string `json:"defaultBranch"`
	// HasToken says whether the tracker can reach back to the host. The token
	// itself is never sent to a client.
	HasToken          bool   `json:"hasToken"`
	TransitionOnMerge string `json:"transitionOnMerge,omitempty"`
	// WebhookSecret is filled in only on the response that creates or rotates
	// it, which is the one chance to copy it into the host.
	WebhookSecret string    `json:"webhookSecret,omitempty"`
	CommitCount   int       `json:"commitCount"`
	OpenPullCount int       `json:"openPullRequestCount"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`

	// secret and token are read for use, never marshalled.
	secret string
	token  string
}

// Commit is one commit the host told us about.
type Commit struct {
	ID          uuid.UUID `json:"id"`
	Repository  string    `json:"repository"`
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	AuthorName  string    `json:"authorName,omitempty"`
	AuthorEmail string    `json:"authorEmail,omitempty"`
	URL         string    `json:"url,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	CommittedAt time.Time `json:"committedAt"`
}

// Branch is a branch that is, or was made, for an issue. It is followed from
// then on: every push moves its head, and merging the pull request from it
// marks it merged, so the issue shows how far its branch has come.
type Branch struct {
	ID         uuid.UUID `json:"id"`
	Repository string    `json:"repository"`
	Name       string    `json:"name"`
	URL        string    `json:"url,omitempty"`
	HeadSHA    string    `json:"headSha,omitempty"`
	// CommitCount is how many commits pushes have brought to the branch since
	// it was first seen here, not the branch's length on the host.
	CommitCount int        `json:"commitCount"`
	PushedAt    *time.Time `json:"pushedAt,omitempty"`
	MergedAt    *time.Time `json:"mergedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`

	// created is set on a branch a push has just made, which is worth a row
	// of its own even when its name says nothing about an issue.
	created bool
}

// PullState is where a pull request is in its life.
type PullState string

const (
	PullOpen   PullState = "open"
	PullMerged PullState = "merged"
	PullClosed PullState = "closed"
)

// PullRequest is a pull or merge request; the two hosts name it differently and
// this product does not take sides.
type PullRequest struct {
	ID           uuid.UUID  `json:"id"`
	Repository   string     `json:"repository"`
	Number       int64      `json:"number"`
	Title        string     `json:"title"`
	URL          string     `json:"url,omitempty"`
	State        PullState  `json:"state"`
	SourceBranch string     `json:"sourceBranch,omitempty"`
	TargetBranch string     `json:"targetBranch,omitempty"`
	AuthorName   string     `json:"authorName,omitempty"`
	HeadSHA      string     `json:"headSha,omitempty"`
	OpenedAt     time.Time  `json:"openedAt"`
	MergedAt     *time.Time `json:"mergedAt,omitempty"`
	// CI is the latest run against the pull request's head, when there is one.
	CI *CIRun `json:"ci,omitempty"`
}

// CIStatus is how a run came out, or that it has not yet.
type CIStatus string

const (
	CIPending   CIStatus = "pending"
	CISuccess   CIStatus = "success"
	CIFailure   CIStatus = "failure"
	CICancelled CIStatus = "cancelled"
)

// CIRun is one run of a check, workflow or pipeline against a commit.
type CIRun struct {
	ID         uuid.UUID  `json:"id"`
	Repository string     `json:"repository"`
	ExternalID string     `json:"externalId"`
	Name       string     `json:"name"`
	Status     CIStatus   `json:"status"`
	URL        string     `json:"url,omitempty"`
	SHA        string     `json:"sha"`
	Branch     string     `json:"branch,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Development is everything the repositories know about one issue: the panel
// on the issue page.
type Development struct {
	// BranchName is the name a branch made for the issue would get, so the
	// person making one sees it before it exists.
	BranchName   string        `json:"branchName"`
	Branches     []Branch      `json:"branches"`
	PullRequests []PullRequest `json:"pullRequests"`
	Commits      []Commit      `json:"commits"`
	// Runs are the CI runs against this issue's commits and pull requests,
	// newest first.
	Runs []CIRun `json:"runs"`
}

// Delivery is what one webhook told us, in the host's own terms translated into
// ours. A host adapter produces it; the service records it.
type Delivery struct {
	Commits []Commit
	// Branches are the branches the delivery touched, with the head each push
	// left them at. Only a branch that is new, or that names or already
	// belongs to an issue, gets a row.
	Branches     []Branch
	PullRequests []PullRequest
	Runs         []CIRun
	// Deleted names branches the push removed.
	DeletedBranches []string
}

var (
	// ErrNotFound is returned for a repository that is not in the caller's
	// organization; existence itself is privileged.
	ErrNotFound = errors.New("repository not found")
	// ErrNameTaken is returned when the project already has that repository.
	ErrNameTaken = errors.New("that repository is already connected to this project")
	// ErrBadHost is returned for a host this product does not speak to.
	ErrBadHost = errors.New("the host must be github, gitlab or gitea")
	// ErrUnauthenticated is returned for a webhook whose signature or token is
	// wrong, which is the only defence a public endpoint has.
	ErrUnauthenticated = errors.New("the webhook did not authenticate")
	// ErrNoToken is returned when reaching the host needs a token and the
	// repository has none.
	ErrNoToken = errors.New("this repository has no access token, so the tracker cannot write to it")
	// ErrHost is returned when the host's API refused or failed.
	ErrHost = errors.New("the host refused")
	// ErrBranchExists is returned when the host already has a branch of that
	// name, which is usually somebody having made it by hand first.
	ErrBranchExists = errors.New("the host already has a branch of that name")
	// ErrBadBranchName is returned for a name git would not accept as a ref.
	ErrBadBranchName = errors.New("that is not a name git accepts for a branch")
	// ErrUnknownEvent is returned for a webhook event kind this product does
	// not read; the host still gets a 2xx so it does not retry.
	ErrUnknownEvent = errors.New("event not handled")
)
