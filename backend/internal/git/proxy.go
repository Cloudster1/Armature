package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// The git proxy lets a partner clone a project's repository and push an
// issue's branch as the person, while the host's token stays here (#333).
const (
	// MaxPushBytes is the most one push may send; the proxy holds a push
	// whole while it reads every command and commit in it.
	MaxPushBytes = 32 << 20
	// TransferTimeout bounds one request of a clone or a push, which streams
	// for longer than any other request is allowed to take.
	TransferTimeout = 10 * time.Minute
)

// Leg is one of the three requests git's smart HTTP is made of.
type Leg string

const (
	LegRefs        Leg = "info/refs"
	LegUploadPack  Leg = "git-upload-pack"
	LegReceivePack Leg = "git-receive-pack"
)

// forwardedHeaders are what a git client says that the host needs to hear;
// its credentials and cookies are never among them.
var forwardedHeaders = []string{"Content-Type", "Content-Encoding", "Accept", "Git-Protocol", "User-Agent"}

// hostUser is the user name each host takes beside a token in basic auth.
func hostUser(kind HostKind) string {
	if kind == GitHub {
		return "x-access-token"
	}
	return "oauth2"
}

// cloneURL is where the host serves the repository over smart HTTP.
func cloneURL(repo *Repository) string {
	return strings.TrimSuffix(strings.TrimRight(repo.URL, "/"), ".git") + ".git"
}

// ProxyRepository finds the repository of a project by the owner/name the host
// knows it by, with its token for the proxy to reach the host with.
func (s *Service) ProxyRepository(ctx context.Context, projectKey, name string) (*Repository, error) {
	var found []*Repository
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRepository+` WHERE p.key = $1 AND lower(r.name) = lower($2) ORDER BY r.host`,
			project.NormalizeKey(projectKey), strings.Trim(name, "/"))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRepository(rows)
			if err != nil {
				return err
			}
			found = append(found, r)
		}
		return rows.Err()
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("find repository %q: %w", name, err)
	case len(found) == 0:
		return nil, ErrNotFound
	case len(found) > 1:
		return nil, ErrAmbiguousName
	}
	return found[0], nil
}

// Forward sends one leg of a clone or a push to the host with the
// repository's token, and hands back the host's answer to relay.
func (s *Service) Forward(ctx context.Context, repo *Repository, leg Leg, query string, header http.Header, body io.Reader) (*http.Response, error) {
	if repo.token == "" {
		return nil, ErrNoToken
	}
	method := http.MethodPost
	if leg == LegRefs {
		method = http.MethodGet
	}
	address := cloneURL(repo) + "/" + string(leg)
	if query != "" {
		address += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHost, err)
	}
	for _, name := range forwardedHeaders {
		if value := header.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	req.SetBasicAuth(hostUser(repo.Host), repo.token)
	resp, err := s.transfer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHost, err)
	}
	return resp, nil
}

// PushedRef is one command of a push as the audit log keeps it, with the
// issue its branch is for and why it was refused, if it was.
type PushedRef struct {
	Ref     string `json:"ref"`
	Old     string `json:"old"`
	New     string `json:"new"`
	Issue   string `json:"issue,omitempty"`
	Refused string `json:"refused,omitempty"`
}

// CheckPush judges every command of a push before any of it reaches the host.
// mayEdit says whether the person may edit the issues of a project.
func (s *Service) CheckPush(ctx context.Context, repo *Repository, push *PushRequest, mayEdit func(projectKey string) bool) ([]PushedRef, bool, error) {
	refs := make([]PushedRef, 0, len(push.Updates))
	var commits map[string][]string
	allowed := true
	for _, u := range push.Updates {
		ref := PushedRef{Ref: u.Ref, Old: u.Old, New: u.New}
		reason, key, err := s.judge(ctx, repo, push, u, mayEdit, &commits)
		if err != nil {
			return nil, false, err
		}
		ref.Issue, ref.Refused = key, reason
		allowed = allowed && reason == ""
		refs = append(refs, ref)
	}
	if !allowed {
		for i := range refs {
			if refs[i].Refused == "" {
				refs[i].Refused = "Another ref in this push was refused, so none of it was pushed."
			}
		}
	}
	return refs, allowed, nil
}

// judge answers why one command may not go to the host, or nothing, and the
// issue its branch is for.
func (s *Service) judge(ctx context.Context, repo *Repository, push *PushRequest, u RefUpdate, mayEdit func(string) bool, commits *map[string][]string) (string, string, error) {
	branch, isBranch := strings.CutPrefix(u.Ref, "refs/heads/")
	switch {
	case strings.HasPrefix(u.Ref, "refs/tags/"):
		return "Tags are not pushed through Armature. Push a branch made for an issue, and tag on the host.", "", nil
	case !isBranch || branch == "":
		return "Only a branch made for an issue is pushed through Armature.", "", nil
	case branch == repo.DefaultBranch:
		return branch + " is the default branch, which takes work only through a pull request. Push to the issue's branch and open one.", "", nil
	case u.Deletes():
		return "Armature does not delete branches. Delete it on the host, or leave it to its pull request.", "", nil
	}
	key, err := s.branchIssue(ctx, repo.ID, branch)
	if err != nil {
		return "", "", err
	}
	if key == "" {
		return branch + " was not made for an issue. Make the issue's branch with create_issue_branch and push to that.", "", nil
	}
	projectKey, _, _ := issue.ParseKey(key)
	if !mayEdit(projectKey) {
		return "You may not edit " + key + ", so you may not push to its branch.", key, nil
	}
	if u.Creates() {
		return "", key, nil
	}
	if *commits == nil {
		found, err := PackCommits(push.Pack, len(u.New))
		if err != nil {
			return "Armature could not read the commits in this push, so it cannot tell it rewrites nothing. Push again with a current git.", key, nil
		}
		*commits = found
	}
	if !FastForward(*commits, u.Old, u.New) {
		return "This would rewrite " + branch + ", and Armature never force-pushes. Fetch the branch, put your work on top of it and push again.", key, nil
	}
	return "", key, nil
}

// branchIssue is the key of the issue a branch still on the host was made
// for, or empty; read on the primary, so a branch made a moment ago counts.
func (s *Service) branchIssue(ctx context.Context, repoID uuid.UUID, branch string) (string, error) {
	var (
		projectKey string
		num        int64
	)
	err := s.db.Read(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT p.key, i.key_num FROM git_branch b
			JOIN issue i ON i.id = b.issue_id
			JOIN project p ON p.id = i.project_id
			WHERE b.repository_id = $1 AND b.name = $2 AND b.deleted_at IS NULL`, repoID, branch).Scan(&projectKey, &num)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read branch %q: %w", branch, err)
	}
	return issue.FormatKey(projectKey, num), nil
}

// PushRecord is one push through the proxy, for the audit log.
type PushRecord struct {
	Actor   uuid.UUID
	AppID   *uuid.UUID
	AppName string
	IP      string
	Refs    []PushedRef
	// Outcome is "refused", or "forwarded" with the host's HTTP status.
	Outcome    string
	HostStatus int
}

// RecordPush writes a push to the audit log: who, through which app, which
// refs, and whether it went on to the host.
func (s *Service) RecordPush(ctx context.Context, repo *Repository, rec PushRecord) (db.LSN, error) {
	data := map[string]any{
		"project": repo.ProjectKey, "repository": repo.Name, "refs": rec.Refs, "outcome": rec.Outcome,
	}
	if rec.HostStatus != 0 {
		data["hostStatus"] = rec.HostStatus
	}
	if rec.AppID != nil {
		data["app"] = map[string]any{"clientId": *rec.AppID, "name": rec.AppName}
	}
	return s.db.Write(context.WithoutCancel(ctx), func(ctx context.Context, tx db.DBTX) error {
		return audit.Write(ctx, tx, audit.Entry{
			Action: "git.push", TargetType: "git_repository", TargetID: &repo.ID, Actor: rec.Actor, IP: rec.IP, Data: data,
		})
	})
}
