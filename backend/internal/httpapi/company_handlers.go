package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/desk"
)

// A desk's companies, as its agents read them and its administrators keep them.

func (s *Server) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	found, err := s.Desk.Companies(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"companies": found})
}

// companyRequest leaves out what it does not change; domains replace the list.
type companyRequest struct {
	Name    *string   `json:"name,omitempty"`
	Domains *[]string `json:"domains,omitempty"`
}

func (s *Server) handleCreateCompany(w http.ResponseWriter, r *http.Request) {
	var req companyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Desk.CreateCompany(r.Context(), r.PathValue("projectKey"), desk.CompanyInput{Name: req.Name, Domains: req.Domains})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"company": created})
}

func companyID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("companyID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid company id."))
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleUpdateCompany(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r)
	if !ok {
		return
	}
	var req companyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Desk.UpdateCompany(r.Context(), id, desk.CompanyInput{Name: req.Name, Domains: req.Domains})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"company": updated})
}

func (s *Server) handleDeleteCompany(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r)
	if !ok {
		return
	}
	lsn, err := s.Desk.DeleteCompany(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type companyMemberRequest struct {
	Email string `json:"email"`
}

// handleAddCompanyMember names a customer by address, making an account for an
// address that has none, as following a request does.
func (s *Server) handleAddCompanyMember(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r)
	if !ok {
		return
	}
	var req companyMemberRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	user, err := s.Auth.UserForAddress(r.Context(), req.Email)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	updated, lsn, err := s.Desk.AddCompanyMember(r.Context(), id, user.ID)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"company": updated})
}

func (s *Server) handleRemoveCompanyMember(w http.ResponseWriter, r *http.Request) {
	id, ok := companyID(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid user id."))
		return
	}
	lsn, err := s.Desk.RemoveCompanyMember(r.Context(), id, userID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
