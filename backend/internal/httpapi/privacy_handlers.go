package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
)

// doorVouchesForNobody refuses an act on the account itself from a session that
// came through an open door, which took the address on trust.
func doorVouchesForNobody(w http.ResponseWriter, r *http.Request, p *auth.Principal) bool {
	if p.SessionID == nil || p.Proof != auth.ProofOpenDoor {
		return false
	}
	respondError(w, r, ErrForbidden("A desk entered without a code cannot export or erase the account behind the address. Ask the desk for a code by mail and sign in with it."))
	return true
}

// handleExportMe hands the caller everything held about them as one file.
func (s *Server) handleExportMe(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if doorVouchesForNobody(w, r, p) {
		return
	}
	export, err := s.Privacy.Export(r.Context(), p.User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	body, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		respondError(w, r, err)
		return
	}
	name := strings.ToLower(strings.Join(strings.Fields(p.User.Name), "-"))
	if name == "" {
		name = "me"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="armature-%s-%s.json"`, name, time.Now().UTC().Format("2006-01-02")))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleEraseMe is the person's own act, from a browser session only: a
// leaked token must not be able to erase its owner.
func (s *Server) handleEraseMe(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.SessionID == nil {
		respondError(w, r, ErrForbidden("Deleting an account is done from a browser session, not with an API token."))
		return
	}
	if doorVouchesForNobody(w, r, p) {
		return
	}
	if err := s.Privacy.Erase(r.Context(), p.User.ID, p.User.ID); err != nil {
		respondError(w, r, err)
		return
	}
	s.clearSessionCookie(w)
	respondNoContent(w)
}

func (s *Server) handleDeleteOrganization(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.SessionID == nil {
		respondError(w, r, ErrForbidden("An organization is deleted from a browser session, not with an API token."))
		return
	}
	lsn, err := s.Privacy.DeleteOrganization(r.Context(), p.Org.ID, p.User.ID, r.URL.Query().Get("confirm"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.Log.Info("organization deleted", "org", p.Org.ID, "slug", p.Org.Slug, "by", p.User.ID)
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleRemoveMember lets somebody go by switching their account off, the
// same as the users page does, so they can be switched on again.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		respondError(w, r, ErrNotFound("That person was not found."))
		return
	}
	off := false
	_, lsn, err := s.Auth.UpdateManagedUser(r.Context(), p.Org.ID, userID, auth.ManagedUserInput{IsActive: &off}, p.User.ID, p.Role, clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
