package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
)

// remoteLinkRequest names a page by its address. Sending an address the issue
// already carries retitles that page, which is what makes a sync safe to repeat.
type remoteLinkRequest struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	// Source names the application the page lives in.
	Source  string `json:"source"`
	IconURL string `json:"iconUrl,omitempty"`
}

func (s *Server) handleListRemoteLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.Issues.RemoteLinks(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"remoteLinks": links})
}

func (s *Server) handlePutRemoteLink(w http.ResponseWriter, r *http.Request) {
	var req remoteLinkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	link, created, lsn, err := s.Issues.PutRemoteLink(r.Context(), r.PathValue("issueKey"), issue.RemoteLinkInput{
		URL: req.URL, Title: req.Title, Source: req.Source, IconURL: req.IconURL,
	}, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respondJSON(w, r, status, map[string]any{"remoteLink": link})
}

func (s *Server) handleDeleteRemoteLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("remoteLinkID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("That is not a page link id. Take it from the issue's list of page links."))
		return
	}
	lsn, err := s.Issues.RemoveRemoteLink(r.Context(), r.PathValue("issueKey"), id, actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
