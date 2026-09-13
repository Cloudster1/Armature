package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/perm"
)

// Access administration: the groups people belong to, and the roles held by
// people and groups. All of it is organization administration, because a role
// granted over one project is still a decision about the tenant's access.

// roleView is a role and what it grants, as the access page explains it.
type roleView struct {
	Role        perm.Role         `json:"role"`
	OrgWideOnly bool              `json:"orgWideOnly"`
	Permissions []perm.Permission `json:"permissions"`
}

// handleListRoles describes the roles themselves, so a settings page can
// explain what each one grants rather than leaving somebody to guess.
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	out := make([]roleView, 0, len(perm.Roles))
	for _, role := range perm.Roles {
		out = append(out, roleView{
			Role:        role,
			OrgWideOnly: role.OrgWideOnly(),
			Permissions: role.Permissions(),
		})
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"roles": out})
}

// handleMyAccess answers "what may I do", which the client needs to decide
// which buttons to draw. Drawing one it cannot use is worse than not drawing it.
func (s *Server) handleMyAccess(w http.ResponseWriter, r *http.Request) {
	set := PermsFrom(r.Context())
	respondJSON(w, r, http.StatusOK, map[string]any{
		"grants":           set.Grants(),
		"projects":         set.Projects(),
		"canAdministerOrg": set.CanInOrg(perm.OrgAdminister),
		"canCreateProject": set.CanInOrg(perm.ProjectCreate),
	})
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	found, err := s.Perms.Groups(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"groups": found})
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("groupID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid group id."))
		return
	}
	found, err := s.Perms.GroupByID(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"group": found})
}

type createGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ExternalRef string `json:"externalRef"`
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	created, lsn, err := s.Perms.CreateGroup(r.Context(), perm.GroupInput{
		Name: req.Name, Description: req.Description, ExternalRef: req.ExternalRef,
	}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"group": created})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("groupID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid group id."))
		return
	}
	lsn, err := s.Perms.DeleteGroup(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type addGroupMemberRequest struct {
	UserID uuid.UUID `json:"userId"`
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("groupID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid group id."))
		return
	}
	var req addGroupMemberRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.UserID == uuid.Nil {
		respondError(w, r, ErrBadRequest("Who should be added to the group?"))
		return
	}

	updated, lsn, err := s.Perms.AddToGroup(r.Context(), id, req.UserID, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"group": updated})
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("groupID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid group id."))
		return
	}
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid user id."))
		return
	}

	updated, lsn, err := s.Perms.RemoveFromGroup(r.Context(), id, userID, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"group": updated})
}

// handleListAssignments lists who holds what. A project query narrows it to the
// roles that answer in that project, organization-wide ones included, because
// those answer there too.
func (s *Server) handleListAssignments(w http.ResponseWriter, r *http.Request) {
	found, err := s.Perms.Assignments(r.Context(), r.URL.Query().Get("project"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"assignments": found})
}

type grantRoleRequest struct {
	Role       perm.Role  `json:"role"`
	ProjectKey string     `json:"projectKey"`
	UserID     *uuid.UUID `json:"userId"`
	GroupID    *uuid.UUID `json:"groupId"`
}

func (s *Server) handleGrantRole(w http.ResponseWriter, r *http.Request) {
	var req grantRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	granted, lsn, err := s.Perms.Grant(r.Context(), perm.GrantInput{
		Role: req.Role, ProjectKey: req.ProjectKey, UserID: req.UserID, GroupID: req.GroupID,
	}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"assignment": granted})
}

func (s *Server) handleRevokeRole(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("assignmentID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid assignment id."))
		return
	}
	lsn, err := s.Perms.Revoke(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
