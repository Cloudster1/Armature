package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
)

// The portal's files: a customer puts them on their own request and reads the
// desk's; the desk service decides whose request it is.

func (s *Server) handlePortalAttachments(w http.ResponseWriter, r *http.Request) {
	found, err := s.Desk.Attachments(r.Context(), r.PathValue("issueKey"), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if found == nil {
		found = []attachment.Attachment{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"attachments": found})
}

func (s *Server) handlePortalAttach(w http.ResponseWriter, r *http.Request) {
	in, err := readUploadedFile(w, r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Desk.Attach(r.Context(), r.PathValue("issueKey"), in, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(uploadError(err)))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"attachment": created})
}

func (s *Server) handlePortalAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("attachmentID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid attachment id."))
		return
	}
	found, body, err := s.Desk.OpenAttachment(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	serveAttachment(w, r, found, body)
}

func (s *Server) handlePortalDetach(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("attachmentID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid attachment id."))
		return
	}
	lsn, err := s.Desk.Detach(r.Context(), id, actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
