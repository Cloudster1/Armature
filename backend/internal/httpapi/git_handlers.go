package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/git"
)

// webhookBodyLimit bounds what a host may send in one delivery. A push with
// hundreds of commits is well under it; anything larger is not a webhook.
const webhookBodyLimit = 5 << 20

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	found, err := s.Git.List(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"repositories": found})
}

type repositoryRequest struct {
	Host              string  `json:"host"`
	Name              string  `json:"name"`
	URL               *string `json:"url"`
	APIBaseURL        string  `json:"apiBaseUrl"`
	DefaultBranch     *string `json:"defaultBranch"`
	AccessToken       *string `json:"accessToken"`
	TransitionOnMerge *string `json:"transitionOnMerge"`
}

func (s *Server) handleConnectRepository(w http.ResponseWriter, r *http.Request) {
	var req repositoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := git.ConnectInput{Host: git.HostKind(req.Host), Name: req.Name, APIBaseURL: req.APIBaseURL}
	if req.URL != nil {
		in.URL = *req.URL
	}
	if req.DefaultBranch != nil {
		in.DefaultBranch = *req.DefaultBranch
	}
	if req.AccessToken != nil {
		in.AccessToken = *req.AccessToken
	}
	if req.TransitionOnMerge != nil {
		in.TransitionOnMerge = *req.TransitionOnMerge
	}

	connected, lsn, err := s.Git.Connect(r.Context(), r.PathValue("projectKey"), in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{
		"repository": connected,
		"webhookUrl": s.webhookURL(connected.ID),
	})
}

// webhookURL is the address to give the host. The application's own base is
// what the browser reaches, and the API is proxied under it.
func (s *Server) webhookURL(repoID uuid.UUID) string {
	return s.AppBaseURL + "/api/v1/git/webhooks/" + repoID.String()
}

func (s *Server) handleUpdateRepository(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("repositoryID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid repository id."))
		return
	}
	var req repositoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Git.Update(r.Context(), id, git.UpdateInput{
		URL: req.URL, DefaultBranch: req.DefaultBranch, AccessToken: req.AccessToken, TransitionOnMerge: req.TransitionOnMerge,
	}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"repository": updated})
}

func (s *Server) handleRotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("repositoryID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid repository id."))
		return
	}
	rotated, lsn, err := s.Git.RotateSecret(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"repository": rotated, "webhookUrl": s.webhookURL(id)})
}

func (s *Server) handleDisconnectRepository(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("repositoryID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid repository id."))
		return
	}
	lsn, err := s.Git.Disconnect(r.Context(), id, userFrom(r), clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleWebhook is the one endpoint a host calls. It has no session: the
// delivery is authenticated against the repository's own secret, and a bad one
// is refused before anything is read.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("repositoryID"))
	if err != nil {
		respondError(w, r, ErrNotFound("No such repository."))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, webhookBodyLimit))
	if err != nil {
		respondError(w, r, ErrBadRequest("The delivery could not be read."))
		return
	}

	receipt, err := s.Git.Receive(r.Context(), id, r, body)
	switch {
	case errors.Is(err, git.ErrUnauthenticated):
		respondError(w, r, &APIError{Status: http.StatusUnauthorized, Code: "webhook_unauthenticated", Message: "The delivery did not authenticate."})
		return
	case errors.Is(err, git.ErrNotFound):
		respondError(w, r, ErrNotFound("No such repository."))
		return
	case err != nil:
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, receipt)
}

func (s *Server) handleIssueDevelopment(w http.ResponseWriter, r *http.Request) {
	found, err := s.Git.ForIssue(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, found)
}

type createBranchRequest struct {
	// RepositoryID or Repository, its owner/name, names a repository of the
	// issue's project; a tool knows the name, the page the id.
	RepositoryID uuid.UUID `json:"repositoryId,omitzero"`
	Repository   string    `json:"repository,omitempty"`
	// Name is the branch to make; empty takes the name suggested for the
	// issue. From is the branch to start it at; empty takes the default branch.
	Name string `json:"name,omitempty"`
	From string `json:"from,omitempty"`
}

func (s *Server) handleCreateBranch(w http.ResponseWriter, r *http.Request) {
	var req createBranchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	repoID, err := s.issueRepository(r, req.RepositoryID, req.Repository)
	if err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Git.CreateBranch(r.Context(), r.PathValue("issueKey"), repoID, req.Name, req.From, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"branch": made})
}

type openPullRequestRequest struct {
	RepositoryID uuid.UUID `json:"repositoryId,omitzero"`
	Repository   string    `json:"repository,omitempty"`
	Branch       string    `json:"branch"`
	Title        string    `json:"title"`
	Body         string    `json:"body,omitempty"`
	Draft        bool      `json:"draft,omitempty"`
}

func (s *Server) handleOpenPullRequest(w http.ResponseWriter, r *http.Request) {
	var req openPullRequestRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	repoID, err := s.issueRepository(r, req.RepositoryID, req.Repository)
	if err != nil {
		respondError(w, r, err)
		return
	}
	opened, lsn, err := s.Git.OpenPullRequest(r.Context(), r.PathValue("issueKey"), repoID, git.PullRequestInput{
		Branch: req.Branch, Title: req.Title, Body: req.Body, Draft: req.Draft,
	}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"pullRequest": opened})
}

// issueRepository is the repository a write on an issue names, by id or by
// owner/name, and never both.
func (s *Server) issueRepository(r *http.Request, id uuid.UUID, name string) (uuid.UUID, error) {
	switch {
	case id != uuid.Nil && name != "":
		return uuid.Nil, ErrBadRequest("Name the repository once, by repository or by repositoryId, not both.")
	case id != uuid.Nil:
		return id, nil
	case name == "":
		return uuid.Nil, ErrBadRequest("Which repository? Give repository as owner/name, the way list_repositories names it.")
	}
	return s.Git.RepositoryOfIssue(r.Context(), r.PathValue("issueKey"), name)
}
