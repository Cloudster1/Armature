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
	lsn, err := s.Git.Disconnect(r.Context(), id)
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
	RepositoryID uuid.UUID `json:"repositoryId"`
	// Name is the branch to make; empty takes the name suggested for the
	// issue. From is the branch to start it at; empty takes the repository's
	// default branch.
	Name string `json:"name"`
	From string `json:"from"`
}

func (s *Server) handleCreateBranch(w http.ResponseWriter, r *http.Request) {
	var req createBranchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.RepositoryID == uuid.Nil {
		respondError(w, r, ErrBadRequest("Which repository should the branch be made in?"))
		return
	}
	made, lsn, err := s.Git.CreateBranch(r.Context(), r.PathValue("issueKey"), req.RepositoryID, req.Name, req.From, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"branch": made})
}
