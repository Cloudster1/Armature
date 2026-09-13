package httpapi

import (
	"slices"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/workflow"
)

// The workflow package spells the organization roles out because it cannot
// import auth; this is where the two are made to agree.
func TestRuleRoleChoicesAreTheOrganizationsRoles(t *testing.T) {
	want := []string{
		string(auth.RoleOwner), string(auth.RoleAdmin), string(auth.RoleMember), string(auth.RoleCustomer),
	}
	if !slices.Equal(workflow.OrgRoles, want) {
		t.Errorf("workflow.OrgRoles = %v, want auth's %v", workflow.OrgRoles, want)
	}
}
