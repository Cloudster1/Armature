package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/perm"
)

// Access administration: the groups people belong to, and the roles held by
// people and groups. All of it is organization administration, because a role
// granted over one project is still a decision about the tenant's access.

// permissionView is one permission in the words the matrix shows.
type permissionView struct {
	Permission perm.Permission `json:"permission"`
	Words      string          `json:"words"`
}

type createRoleRequest struct {
	Key         string   `json:"key,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	OrgWideOnly bool     `json:"orgWideOnly,omitempty"`
	Permissions []string `json:"permissions"`
}

type updateRoleRequest struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	OrgWideOnly *bool     `json:"orgWideOnly,omitempty"`
	Permissions *[]string `json:"permissions,omitempty"`
}

func permissionsOf(names []string) []perm.Permission {
	out := make([]perm.Permission, 0, len(names))
	for _, n := range names {
		out = append(out, perm.Permission(n))
	}
	return out
}

// handleListRoles lists the organization's roles and what each grants, so
// a settings page can explain them and the matrix can edit them.
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.Perms.Roles(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"roles": roles})
}

// handleListPermissions names every permission the code checks for, in the
// order the matrix lists them.
func (s *Server) handleListPermissions(w http.ResponseWriter, r *http.Request) {
	out := make([]permissionView, 0, len(perm.AllPermissions))
	for _, p := range perm.AllPermissions {
		out = append(out, permissionView{Permission: p, Words: perm.PermissionWords[p]})
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"permissions": out})
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req createRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Perms.CreateRole(r.Context(), perm.RoleInput{
		Key: perm.Role(req.Key), Name: &req.Name, Description: &req.Description,
		OrgWideOnly: &req.OrgWideOnly, Permissions: permissionsOf(req.Permissions),
	}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"role": made})
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req updateRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := perm.RoleInput{Key: perm.Role(r.PathValue("roleKey")), Name: req.Name, Description: req.Description, OrgWideOnly: req.OrgWideOnly}
	if req.Permissions != nil {
		in.Permissions = permissionsOf(*req.Permissions)
		if in.Permissions == nil {
			in.Permissions = []perm.Permission{}
		}
	}
	saved, lsn, err := s.Perms.UpdateRole(r.Context(), in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"role": saved})
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Perms.DeleteRole(r.Context(), perm.Role(r.PathValue("roleKey")), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
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
		"permissions":      map[string]any{"org": set.OrgPermissions(), "projects": set.ProjectPermissions()},
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
