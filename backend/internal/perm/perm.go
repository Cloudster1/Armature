// Package perm answers one question: may this person do this thing here.
//
// Roles are granted to people and to groups, over the whole organization or
// over one project. What each role means is a table in this file rather than
// conditionals spread through the handlers, so the answer to "what can a scrum
// master do" is somewhere a person can read it.
package perm

import "slices"

// Role is a named bundle of permissions.
type Role string

const (
	// GlobalAdministrator administers the tenant: its members, its roles, its
	// workflows and every project in it.
	GlobalAdministrator Role = "global_administrator"
	// ProjectAdministrator configures one project: its boards, its workflow
	// scheme, its teams, and whether it is archived.
	ProjectAdministrator Role = "project_administrator"
	// ScrumMaster runs the work in one project: sprints, teams and the
	// planning that goes with them.
	ScrumMaster Role = "scrum_master"
	// User does the ordinary work: files issues, moves them, comments.
	User Role = "user"
	// Reader sees a project and changes nothing in it.
	Reader Role = "reader"
)

// Roles are listed most powerful first, which is the order they should be
// offered in and the order a listing reads best in.
var Roles = []Role{GlobalAdministrator, ProjectAdministrator, ScrumMaster, User, Reader}

func (r Role) Valid() bool { return slices.Contains(Roles, r) }

// OrgWideOnly reports whether a role only makes sense over the whole tenant.
// Administering the organization from inside one project is not a smaller
// version of the same thing; it is a different thing.
func (r Role) OrgWideOnly() bool { return r == GlobalAdministrator }

// Permission is one thing somebody may do.
type Permission string

const (
	// Read is seeing a project and everything in it.
	Read Permission = "read"

	// IssueWrite is filing and editing issues, including scheduling and sizing.
	IssueWrite Permission = "issue.write"
	// IssueTransition is moving an issue through its workflow.
	IssueTransition Permission = "issue.transition"
	// CommentWrite is adding and editing comments.
	CommentWrite Permission = "comment.write"

	// SprintManage is planning, starting and completing sprints, and deciding
	// what is committed to them.
	SprintManage Permission = "sprint.manage"
	// TeamManage is forming teams and deciding who is on them.
	TeamManage Permission = "team.manage"
	// BoardConfigure is changing what a board is: its swimlanes and its scope.
	BoardConfigure Permission = "board.configure"

	// ProjectAdminister is a project's own settings, including which workflow
	// scheme it overrides the organization with.
	ProjectAdminister Permission = "project.administer"
	// ProjectCreate is making a new project, which is a tenant level act.
	ProjectCreate Permission = "project.create"

	// OrgAdminister is the tenant's own settings: members, invitations, roles,
	// groups, the identity provider and the organization's workflows.
	OrgAdminister Permission = "org.administer"
)

// grants is what each role can do. A role's permissions are exactly this list;
// there is no inheritance between roles, because "a scrum master is a user plus
// sprints" stops being true the moment somebody wants a scrum master who cannot
// edit issues.
var grants = map[Role][]Permission{
	GlobalAdministrator: {
		Read, IssueWrite, IssueTransition, CommentWrite,
		SprintManage, TeamManage, BoardConfigure,
		ProjectAdminister, ProjectCreate, OrgAdminister,
	},
	ProjectAdministrator: {
		Read, IssueWrite, IssueTransition, CommentWrite,
		SprintManage, TeamManage, BoardConfigure, ProjectAdminister,
	},
	ScrumMaster: {
		Read, IssueWrite, IssueTransition, CommentWrite,
		SprintManage, TeamManage,
	},
	User: {
		Read, IssueWrite, IssueTransition, CommentWrite,
	},
	Reader: {
		Read,
	},
}

// Allows reports whether a role grants a permission.
func (r Role) Allows(p Permission) bool { return slices.Contains(grants[r], p) }

// Permissions lists what a role grants, for a settings page that has to explain
// itself rather than leave somebody guessing.
func (r Role) Permissions() []Permission { return slices.Clone(grants[r]) }

// Grant is one role held over one scope.
//
// The scope is a project key rather than an id because every question the HTTP
// layer asks arrives as a key in the path; keying by id would mean a database
// lookup on every guarded request to translate one into the other. An empty key
// is the whole organization: the role applies in every project, including ones
// made after it was granted.
type Grant struct {
	Role       Role   `json:"role"`
	ProjectKey string `json:"projectKey,omitempty"`
}

// Set is everything one person may do in one organization.
//
// It is resolved once per request and carried on the principal, so a handler
// asks a question of memory rather than of the database.
type Set struct {
	// orgWide is what is granted everywhere, project keyed is what is granted
	// in one place. Keeping them apart is what stops a project grant leaking.
	orgWide   map[Permission]bool
	byProject map[string]map[Permission]bool
	roles     []Grant
}

// NewSet turns a list of grants into the answers a request needs.
func NewSet(grants []Grant) Set {
	s := Set{
		orgWide:   map[Permission]bool{},
		byProject: map[string]map[Permission]bool{},
		roles:     slices.Clone(grants),
	}
	for _, g := range grants {
		if !g.Role.Valid() {
			continue
		}
		if g.ProjectKey == "" {
			for _, p := range g.Role.Permissions() {
				s.orgWide[p] = true
			}
			continue
		}
		here, ok := s.byProject[g.ProjectKey]
		if !ok {
			here = map[Permission]bool{}
			s.byProject[g.ProjectKey] = here
		}
		for _, p := range g.Role.Permissions() {
			here[p] = true
		}
	}
	return s
}

// Grants returns the roles this set was built from, for showing somebody why
// they can do what they can do.
func (s Set) Grants() []Grant { return slices.Clone(s.roles) }

// CanInOrg reports whether the permission is held over the whole organization.
// It is the right question for anything not about one project: creating a
// project, or administering the tenant.
func (s Set) CanInOrg(p Permission) bool { return s.orgWide[p] }

// Can reports whether the permission is held in one project, whether that comes
// from an organization-wide grant or from one made over this project alone.
func (s Set) Can(p Permission, projectKey string) bool {
	if s.orgWide[p] {
		return true
	}
	return s.byProject[projectKey][p]
}

// CanSomewhere reports whether the permission is held anywhere at all. It
// answers "should this person be offered this at all", never "may they do it
// here", which is a question only Can can answer.
func (s Set) CanSomewhere(p Permission) bool {
	if s.orgWide[p] {
		return true
	}
	for _, here := range s.byProject {
		if here[p] {
			return true
		}
	}
	return false
}

// Empty reports whether the set grants nothing at all, which is what somebody
// with no roles holds.
func (s Set) Empty() bool { return len(s.orgWide) == 0 && len(s.byProject) == 0 }

// Projects lists the projects this set names explicitly, so a listing can be
// narrowed to what somebody has been given rather than showing everything and
// refusing on click.
func (s Set) Projects() []string {
	out := make([]string, 0, len(s.byProject))
	for key := range s.byProject {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

// keyRefused is what no API key holds, whatever its owner may do. A key does
// the work; deciding who may do it is left to somebody at a keyboard.
var keyRefused = []Permission{OrgAdminister, ProjectCreate}

// AsKey narrows a set to what an API key may do with it. It is applied where
// the set is built, so every later guard and listing reads the narrowed answer.
func (s Set) AsKey(projects []string) Set {
	out := Set{
		orgWide:   map[Permission]bool{},
		byProject: map[string]map[Permission]bool{},
		roles:     s.keyGrants(projects),
	}
	for p := range s.orgWide {
		if slices.Contains(keyRefused, p) {
			continue
		}
		if len(projects) == 0 {
			out.orgWide[p] = true
			continue
		}
		// An organization-wide grant reaches the named projects and no
		// further, or naming projects would narrow nothing.
		for _, key := range projects {
			out.allow(key, p)
		}
	}
	for key, here := range s.byProject {
		if len(projects) > 0 && !slices.Contains(projects, key) {
			continue
		}
		for p := range here {
			if slices.Contains(keyRefused, p) {
				continue
			}
			out.allow(key, p)
		}
	}
	return out
}

// allow records one permission in one project, making the inner map on demand.
func (s Set) allow(projectKey string, p Permission) {
	here, ok := s.byProject[projectKey]
	if !ok {
		here = map[Permission]bool{}
		s.byProject[projectKey] = here
	}
	here[p] = true
}

// keyGrants is what the access page should say a key holds: the owner's grants
// with the ones a key cannot use at all left out.
func (s Set) keyGrants(projects []string) []Grant {
	out := make([]Grant, 0, len(s.roles))
	for _, g := range s.roles {
		if g.Role.OrgWideOnly() {
			continue
		}
		if g.ProjectKey != "" && len(projects) > 0 && !slices.Contains(projects, g.ProjectKey) {
			continue
		}
		out = append(out, g)
	}
	return out
}

// Readable says which projects the set may see: all of them, or the ones listed.
func (s Set) Readable() (all bool, keys []string) {
	if s.orgWide[Read] {
		return true, nil
	}
	keys = []string{}
	for key, here := range s.byProject {
		if here[Read] {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return false, keys
}
