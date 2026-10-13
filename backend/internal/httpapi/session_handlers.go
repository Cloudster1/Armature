package httpapi

import (
	"net/http"

	"github.com/armature/armature/backend/internal/auth"
)

// handleListSessions is where the caller is signed in. requireSession has
// already refused a token, so the principal carries a session.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	sessions, err := s.Auth.Sessions(r.Context(), p.User.ID, *p.SessionID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if sessions == nil {
		sessions = []auth.Session{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) handleEndSession(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "sessionID", "session")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Auth.EndSession(r.Context(), p.User.ID, *p.SessionID, id, clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleEndOtherSessions(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	lsn, err := s.Auth.EndOtherSessions(r.Context(), p.User.ID, *p.SessionID, clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
