package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
)

// The organization's own accounts: made with a password by an administrator,
// renamed, given a new password or switched off by one. Invitations and
// single sign-on stay where they are; this is for the people who have neither.

type createUserRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Password string `json:"password"`
}

type updateUserRequest struct {
	Name     *string `json:"name,omitempty"`
	Role     *string `json:"role,omitempty"`
	IsActive *bool   `json:"isActive,omitempty"`
}

type setUserPasswordRequest struct {
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	users, err := s.Auth.ManagedUsers(r.Context(), p.Org.ID, p.Role)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	user, lsn, err := s.Auth.CreateLocalUser(r.Context(), p.Org.ID, auth.CreateLocalUserInput{
		Email: req.Email, Name: req.Name, Role: auth.OrgRole(req.Role), Password: req.Password,
	}, p.User.ID, clientIP(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"user": user})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrNotFound("That person was not found."))
		return
	}
	var req updateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	var role *auth.OrgRole
	if req.Role != nil {
		standing := auth.OrgRole(*req.Role)
		role = &standing
	}
	p := PrincipalFrom(r.Context())
	user, lsn, err := s.Auth.UpdateManagedUser(r.Context(), p.Org.ID, userID,
		auth.ManagedUserInput{Name: req.Name, Role: role, IsActive: req.IsActive}, p.User.ID, p.Role, clientIP(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"user": user})
}

func (s *Server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrNotFound("That person was not found."))
		return
	}
	var req setUserPasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Auth.SetManagedPassword(r.Context(), p.Org.ID, userID, req.Password, p.User.ID, p.Role, clientIP(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleChangePassword is the person's own change. The session that asked
// stays open; every other one of theirs ends.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Auth.ChangeOwnPassword(r.Context(), p.User.ID, p.SessionID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
