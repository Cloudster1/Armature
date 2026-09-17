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

// orBound answers a failed lookup with its error and a refusal with the
// bound-session sentence.
func orBound(err error) error {
	if err != nil {
		return err
	}
	return sessionBound(http.StatusForbidden)
}

// sessionBound is the answer to a session reaching past the organization its
// proof vouches for.
func sessionBound(status int) *APIError {
	return &APIError{Status: status, Code: "session_bound",
		Message: "This sign-in only reaches the organization it was made for. Sign in with your password to reach your other organizations."}
}

// actsForTheWholePerson says whether the caller may act on the account itself,
// which reaches every organization: a bound session only when it has no other.
func (s *Server) actsForTheWholePerson(r *http.Request, p *auth.Principal) (bool, error) {
	if p.TokenID != nil || p.ReachesEverywhere() {
		return true, nil
	}
	memberships, err := s.Auth.Memberships(r.Context(), p.User.ID)
	if err != nil {
		return false, err
	}
	for _, m := range memberships {
		if p.Org == nil || m.OrgID != p.Org.ID {
			return false, nil
		}
	}
	return true, nil
}

// handleExportMe hands the caller everything held about them as one file.
func (s *Server) handleExportMe(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if ok, err := s.actsForTheWholePerson(r, p); err != nil || !ok {
		respondError(w, r, orBound(err))
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
	if ok, err := s.actsForTheWholePerson(r, p); err != nil || !ok {
		respondError(w, r, orBound(err))
		return
	}
	if err := s.Privacy.Erase(r.Context(), p.User.ID, p.User.ID); err != nil {
		respondError(w, r, err)
		return
	}
	s.clearSessionCookie(w)
	respondNoContent(w)
}

// handleRemoveMember lets an organization go of a member.
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

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		respondError(w, r, ErrNotFound("That person was not found."))
		return
	}
	lsn, err := s.Privacy.RemoveMember(r.Context(), p.Org.ID, userID, p.User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
