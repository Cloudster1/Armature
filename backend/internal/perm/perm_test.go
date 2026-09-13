package perm

import (
	"slices"
	"testing"
)

const (
	here      = "HERE"
	elsewhere = "ELSE"
)

func orgWide(role Role) Grant { return Grant{Role: role} }

func inProject(role Role, key string) Grant { return Grant{Role: role, ProjectKey: key} }

func TestEveryRoleCanRead(t *testing.T) {
	for _, role := range Roles {
		if !role.Allows(Read) {
			t.Errorf("%s cannot read, which makes it a role that grants nothing", role)
		}
	}
}

// A reader is the whole point of having a role that changes nothing.
func TestReaderChangesNothing(t *testing.T) {
	for _, p := range []Permission{
		IssueWrite, IssueTransition, CommentWrite,
		SprintManage, TeamManage, BoardConfigure, ProjectAdminister, OrgAdminister,
	} {
		if Reader.Allows(p) {
			t.Errorf("reader is allowed %s", p)
		}
	}
}

func TestScrumMasterRunsTheWorkWithoutConfiguringTheProject(t *testing.T) {
	for _, p := range []Permission{SprintManage, TeamManage, IssueWrite, IssueTransition} {
		if !ScrumMaster.Allows(p) {
			t.Errorf("a scrum master cannot %s", p)
		}
	}
	// Running sprints is not the same as deciding how the project works.
	for _, p := range []Permission{ProjectAdminister, BoardConfigure, OrgAdminister} {
		if ScrumMaster.Allows(p) {
			t.Errorf("a scrum master is allowed %s", p)
		}
	}
}

func TestProjectAdministratorStopsAtTheProjectBoundary(t *testing.T) {
	if !ProjectAdministrator.Allows(ProjectAdminister) {
		t.Error("a project administrator cannot administer a project")
	}
	for _, p := range []Permission{OrgAdminister, ProjectCreate} {
		if ProjectAdministrator.Allows(p) {
			t.Errorf("a project administrator is allowed %s, which is a tenant level act", p)
		}
	}
}

func TestOnlyGlobalAdministratorAdministersTheOrganization(t *testing.T) {
	for _, role := range Roles {
		if role.Allows(OrgAdminister) != (role == GlobalAdministrator) {
			t.Errorf("%s and org administration disagree", role)
		}
	}
}

func TestGlobalAdministrationIsNotAProjectRole(t *testing.T) {
	if !GlobalAdministrator.OrgWideOnly() {
		t.Error("global administration can be scoped to one project")
	}
	for _, role := range []Role{ProjectAdministrator, ScrumMaster, User, Reader} {
		if role.OrgWideOnly() {
			t.Errorf("%s cannot be granted over one project", role)
		}
	}
}

// The rule that matters most: a role given over one project must not answer for
// another one.
func TestAProjectGrantDoesNotLeak(t *testing.T) {
	set := NewSet([]Grant{inProject(ScrumMaster, here)})

	if !set.Can(SprintManage, here) {
		t.Error("the grant does not apply where it was given")
	}
	if set.Can(SprintManage, elsewhere) {
		t.Error("a grant over one project answered for another")
	}
	if set.Can(Read, elsewhere) {
		t.Error("even reading leaked into a project nobody was given")
	}
}

func TestAnOrgWideGrantAppliesEverywhere(t *testing.T) {
	set := NewSet([]Grant{orgWide(User)})

	if !set.Can(IssueWrite, here) || !set.Can(IssueWrite, elsewhere) {
		t.Error("an organization-wide grant did not reach every project")
	}
	// Including projects that did not exist when it was granted.
	if !set.Can(IssueWrite, "MADELATER") {
		t.Error("an organization-wide grant did not reach a new project")
	}
}

func TestGrantsAddUpRatherThanOverride(t *testing.T) {
	set := NewSet([]Grant{orgWide(Reader), inProject(ScrumMaster, here)})

	if !set.Can(Read, elsewhere) {
		t.Error("the organization-wide reader was lost")
	}
	if !set.Can(SprintManage, here) {
		t.Error("the project grant was lost")
	}
	if set.Can(SprintManage, elsewhere) {
		t.Error("the project grant applied where it was not given")
	}
}

func TestOrganizationQuestionsIgnoreProjectGrants(t *testing.T) {
	set := NewSet([]Grant{inProject(ProjectAdministrator, here)})

	// Administering one project is not a licence to make another.
	if set.CanInOrg(ProjectCreate) {
		t.Error("a project grant answered an organization-wide question")
	}
	if set.CanInOrg(OrgAdminister) {
		t.Error("a project grant reached organization administration")
	}
}

func TestCanSomewhereIsAboutOfferingNotAllowing(t *testing.T) {
	set := NewSet([]Grant{inProject(ScrumMaster, here)})

	if !set.CanSomewhere(SprintManage) {
		t.Error("somebody who runs sprints somewhere is not offered them anywhere")
	}
	if set.CanSomewhere(OrgAdminister) {
		t.Error("a permission nobody holds is offered")
	}
}

func TestSomebodyWithNoRolesHoldsNothing(t *testing.T) {
	set := NewSet(nil)

	if !set.Empty() {
		t.Error("an empty set is not empty")
	}
	if set.Can(Read, here) || set.CanInOrg(Read) {
		t.Error("somebody with no roles can read")
	}
}

// A role nobody recognises grants nothing, rather than everything.
func TestAnUnknownRoleGrantsNothing(t *testing.T) {
	set := NewSet([]Grant{{Role: "superuser"}})

	if !set.Empty() {
		t.Errorf("an unknown role granted %v", set.Grants())
	}
}

func TestSetRemembersWhereItCameFrom(t *testing.T) {
	given := []Grant{orgWide(Reader), inProject(User, here)}
	got := NewSet(given).Grants()

	if len(got) != 2 || got[0].Role != Reader || got[1].Role != User {
		t.Errorf("grants = %+v, want the two it was built from", got)
	}
	// Explaining why somebody can do something needs the scope, not just the role.
	if got[1].ProjectKey != here {
		t.Errorf("the project a role was granted over was lost")
	}
}

func TestProjectsNamesOnlyWhatWasGrantedExplicitly(t *testing.T) {
	set := NewSet([]Grant{orgWide(Reader), inProject(User, here)})

	projects := set.Projects()
	if len(projects) != 1 || projects[0] != here {
		t.Errorf("projects = %v, want just the one named explicitly", projects)
	}
}

func TestAKeyNeverAdministers(t *testing.T) {
	owner := NewSet([]Grant{orgWide(GlobalAdministrator)})
	key := owner.AsKey(nil)

	for _, p := range []Permission{OrgAdminister, ProjectCreate} {
		if !owner.CanInOrg(p) {
			t.Fatalf("the owner should hold %s, or this proves nothing", p)
		}
		if key.CanInOrg(p) {
			t.Errorf("a key holds %s, which its owner should not have been able to lend", p)
		}
		if key.CanSomewhere(p) {
			t.Errorf("a key holds %s somewhere, so a project route would take it", p)
		}
	}
	if !key.Can(IssueWrite, here) {
		t.Error("a key lost the ordinary work along with the administration")
	}
}

func TestAKeyIsConfinedToTheProjectsItNames(t *testing.T) {
	owner := NewSet([]Grant{orgWide(GlobalAdministrator)})
	key := owner.AsKey([]string{here})

	if !key.Can(Read, here) || !key.Can(IssueWrite, here) {
		t.Error("a key cannot work in the project it names")
	}
	if key.Can(Read, elsewhere) {
		t.Error("an organization wide grant carried a key into a project it does not name")
	}
	if key.CanInOrg(Read) {
		t.Error("a confined key kept an organization wide answer, so a listing would show everything")
	}
	all, keys := key.Readable()
	if all || len(keys) != 1 || keys[0] != here {
		t.Errorf("a confined key reads %v (all=%v), want only %s", keys, all, here)
	}
}

func TestAKeyNamingNoProjectsFollowsItsOwner(t *testing.T) {
	owner := NewSet([]Grant{inProject(User, here), inProject(Reader, elsewhere)})
	key := owner.AsKey(nil)

	if !key.Can(IssueWrite, here) || !key.Can(Read, elsewhere) {
		t.Error("a key that names nothing should reach everything its owner does")
	}
}

func TestAKeyCannotNameAProjectItsOwnerCannotReach(t *testing.T) {
	owner := NewSet([]Grant{inProject(User, here)})
	key := owner.AsKey([]string{here, elsewhere})

	if key.Can(Read, elsewhere) {
		t.Error("naming a project granted rights in it, so a key can exceed its owner")
	}
	if !key.Can(IssueWrite, here) {
		t.Error("naming an unreachable project cost the key the one it could reach")
	}
}

func TestNarrowingTheOwnerSetLeavesItAlone(t *testing.T) {
	owner := NewSet([]Grant{orgWide(GlobalAdministrator)})
	_ = owner.AsKey([]string{here})

	if !owner.CanInOrg(OrgAdminister) || !owner.Can(Read, elsewhere) {
		t.Error("narrowing a set for a key changed the set the person holds")
	}
}

// The ratchet: a permission only the global administrator holds is
// administration, and a key may not hold it. This catches the one added next
// year, which would otherwise reach a key the day its route appeared.
func TestEveryAdministratorOnlyPermissionIsRefusedToKeys(t *testing.T) {
	seen := map[Permission]int{}
	for _, role := range Roles {
		for _, p := range role.Permissions() {
			seen[p]++
		}
	}
	if len(seen) == 0 {
		t.Fatal("no permissions were found, so this proves nothing")
	}
	for p, held := range seen {
		onlyAdmin := held == 1 && GlobalAdministrator.Allows(p)
		refused := slices.Contains(keyRefused, p)
		if onlyAdmin && !refused {
			t.Errorf("%s is held by the global administrator alone but a key may still use it; add it to keyRefused", p)
		}
		if refused && !onlyAdmin {
			t.Errorf("%s is refused to keys but an ordinary role grants it, so keys lost work they should do", p)
		}
	}
}
