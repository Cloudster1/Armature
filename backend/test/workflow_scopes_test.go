//go:build integration

package test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// straightThrough builds a workflow that starts in one status and has a single
// transition into another, which is enough to tell it apart from the default.
func (h *harness) straightThrough(t *testing.T, ws *workspace, name, from, to string) *workflow.Workflow {
	t.Helper()
	fromID := h.statusID(t, ws, from)
	toID := h.statusID(t, ws, to)

	built, _, err := ws.admin.CreateWorkflow(ws.ctx, workflow.GraphInput{
		Name: name,
		Steps: []workflow.StepInput{
			{StatusID: fromID, IsInitial: true},
			{StatusID: toID},
		},
		Transitions: []workflow.TransitionInput{
			{Name: "Finish", FromStatusID: &fromID, ToStatusID: toID},
		},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create workflow %q: %v", name, err)
	}
	return built
}

// overrideWith makes a scheme with the given mappings and hands the project it.
func (h *harness) overrideWith(t *testing.T, ws *workspace, name string, items ...workflow.SchemeItemInput) *workflow.Scheme {
	t.Helper()
	created, _, err := ws.admin.CreateScheme(ws.ctx, workflow.SchemeInput{Name: name, Items: items}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create scheme %q: %v", name, err)
	}
	if _, _, err := ws.projects.SetWorkflowScheme(ws.ctx, ws.project.Key, &created.ID, ws.actor.UserID); err != nil {
		t.Fatalf("give %s the scheme: %v", ws.project.Key, err)
	}
	return created
}

// assignmentFor reads the resolution for one issue type.
func (h *harness) assignmentFor(t *testing.T, ws *workspace, typeName string) workflow.Assignment {
	t.Helper()
	var found []workflow.Assignment
	err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = ws.store.Assignments(ctx, tx, ws.project.ID)
		return err
	})
	if err != nil {
		t.Fatalf("read assignments: %v", err)
	}
	for _, a := range found {
		if a.IssueTypeName == typeName {
			return a
		}
	}
	t.Fatalf("no assignment for %q; got %d rows", typeName, len(found))
	return workflow.Assignment{}
}

// typed creates an issue of a named type.
func (ws *workspace) typed(t *testing.T, h *harness, typeName, summary string) *issue.Issue {
	t.Helper()
	created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key,
		Summary:    summary,
		TypeID:     h.issueTypeID(t, ws, typeName),
	}, ws.actor)
	if err != nil {
		t.Fatalf("create %s: %v", typeName, err)
	}
	return created
}

func TestAProjectInheritsTheOrganizationsWorkflow(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "inherit")

	if ws.project.WorkflowSchemeID != nil {
		t.Error("a new project named a scheme of its own; it should have no opinion")
	}

	for _, name := range []string{bootstrap.TypeTask, bootstrap.TypeBug, bootstrap.TypeEpic} {
		got := h.assignmentFor(t, ws, name)
		if got.Origin.Scope != workflow.ScopeTenant {
			t.Errorf("%s is decided at %s scope, want the organization", name, got.Origin.Scope)
		}
		if got.WorkflowName != bootstrap.DefaultWorkflowName {
			t.Errorf("%s uses %q, want the organization's workflow", name, got.WorkflowName)
		}
		if got.Origin.Named {
			t.Errorf("%s is mapped by name, but the organization's scheme only has a fallback", name)
		}
	}
}

func TestAProjectOverridesOneIssueType(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "override")

	triage := h.straightThrough(t, ws, "Bug triage", bootstrap.StatusInReview, bootstrap.StatusDone)
	bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
	h.overrideWith(t, ws, "Override scheme", workflow.SchemeItemInput{
		IssueTypeID: &bugType, WorkflowID: triage.ID,
	})

	t.Run("the type the project named uses the project's workflow", func(t *testing.T) {
		got := h.assignmentFor(t, ws, bootstrap.TypeBug)
		if got.Origin.Scope != workflow.ScopeProject || !got.Origin.Named {
			t.Errorf("bug origin = %+v, want a named project mapping", got.Origin)
		}
		if got.WorkflowName != "Bug triage" {
			t.Errorf("bug uses %q, want Bug triage", got.WorkflowName)
		}
	})

	t.Run("a new bug starts where the project's workflow starts", func(t *testing.T) {
		bug := ws.typed(t, h, bootstrap.TypeBug, "the override decides where this lands")
		if bug.Status.Name != bootstrap.StatusInReview {
			t.Errorf("bug opened in %q, want %q", bug.Status.Name, bootstrap.StatusInReview)
		}
	})

	// The point of an override is that it is a difference, not a replacement.
	t.Run("everything the override leaves out still falls through", func(t *testing.T) {
		got := h.assignmentFor(t, ws, bootstrap.TypeTask)
		if got.Origin.Scope != workflow.ScopeTenant {
			t.Errorf("task origin = %+v, want the organization", got.Origin)
		}
		task := ws.typed(t, h, bootstrap.TypeTask, "this one is ordinary")
		if task.Status.Name != bootstrap.StatusToDo {
			t.Errorf("task opened in %q, want %q", task.Status.Name, bootstrap.StatusToDo)
		}
	})
}

// A project that says "everything here works differently" outranks an
// organization that has an opinion about one type: the narrower scope is asked
// first, and only what it leaves out falls through.
func TestAProjectsFallbackOutranksATenantMappingForTheType(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "narrow")

	tenantBugs := h.straightThrough(t, ws, "Tenant bugs", bootstrap.StatusInProgress, bootstrap.StatusDone)
	projectAll := h.straightThrough(t, ws, "Project everything", bootstrap.StatusInReview, bootstrap.StatusDone)

	bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
	defaultWorkflow := h.assignmentFor(t, ws, bootstrap.TypeTask).WorkflowID
	tenantScheme := h.defaultSchemeID(t, ws)
	if _, _, err := ws.admin.SaveScheme(ws.ctx, tenantScheme, workflow.SchemeInput{
		Name: bootstrap.DefaultSchemeName,
		Items: []workflow.SchemeItemInput{
			{WorkflowID: defaultWorkflow},
			{IssueTypeID: &bugType, WorkflowID: tenantBugs.ID},
		},
	}, ws.actor.UserID); err != nil {
		t.Fatalf("map bugs at the organization: %v", err)
	}

	h.overrideWith(t, ws, "Project scheme", workflow.SchemeItemInput{WorkflowID: projectAll.ID})

	got := h.assignmentFor(t, ws, bootstrap.TypeBug)
	if got.WorkflowName != "Project everything" {
		t.Errorf("bug uses %q, want the project's blanket workflow", got.WorkflowName)
	}
	if got.Origin.Scope != workflow.ScopeProject || got.Origin.Named {
		t.Errorf("bug origin = %+v, want the project's fallback", got.Origin)
	}
}

func TestClearingTheOverrideHandsTheDecisionBack(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "handback")

	triage := h.straightThrough(t, ws, "Bug triage", bootstrap.StatusInReview, bootstrap.StatusDone)
	bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
	h.overrideWith(t, ws, "Override scheme", workflow.SchemeItemInput{
		IssueTypeID: &bugType, WorkflowID: triage.ID,
	})

	updated, _, err := ws.projects.SetWorkflowScheme(ws.ctx, ws.project.Key, nil, ws.actor.UserID)
	if err != nil {
		t.Fatalf("clear the override: %v", err)
	}
	if updated.WorkflowSchemeID != nil {
		t.Errorf("scheme = %v, want none", updated.WorkflowSchemeID)
	}

	got := h.assignmentFor(t, ws, bootstrap.TypeBug)
	if got.Origin.Scope != workflow.ScopeTenant || got.WorkflowName != bootstrap.DefaultWorkflowName {
		t.Errorf("bug = %+v, want the organization's workflow again", got)
	}
}

// Issues that were already open keep their status when the workflow changes
// underneath them; nothing rewrites history to match the new configuration.
func TestChangingTheSchemeLeavesOpenIssuesWhereTheyAre(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "midflight")

	before := ws.typed(t, h, bootstrap.TypeBug, "opened under the old rules")
	if before.Status.Name != bootstrap.StatusToDo {
		t.Fatalf("bug opened in %q", before.Status.Name)
	}

	triage := h.straightThrough(t, ws, "Bug triage", bootstrap.StatusInReview, bootstrap.StatusDone)
	bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
	h.overrideWith(t, ws, "Override scheme", workflow.SchemeItemInput{
		IssueTypeID: &bugType, WorkflowID: triage.ID,
	})

	after, err := ws.issues.ByKey(ws.ctx, before.Key)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status.Name != bootstrap.StatusToDo {
		t.Errorf("status = %q, want it left alone at %q", after.Status.Name, bootstrap.StatusToDo)
	}

	// Its status is not in the new workflow, so only the global transitions are
	// offered rather than none at all.
	available, err := ws.issues.Transitions(ws.ctx, before.Key, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 0 {
		t.Errorf("transitions = %v, want none from a status the new workflow does not contain", transitionNames(available))
	}
}

func TestPromotingASchemeMovesEveryInheritingProject(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "promote")

	replacement := h.straightThrough(t, ws, "Lean", bootstrap.StatusInProgress, bootstrap.StatusDone)
	created, _, err := ws.admin.CreateScheme(ws.ctx, workflow.SchemeInput{
		Name:  "Lean scheme",
		Items: []workflow.SchemeItemInput{{WorkflowID: replacement.ID}},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create scheme: %v", err)
	}

	if _, err := ws.admin.SetDefaultScheme(ws.ctx, created.ID, ws.actor.UserID); err != nil {
		t.Fatalf("promote scheme: %v", err)
	}

	got := h.assignmentFor(t, ws, bootstrap.TypeTask)
	if got.WorkflowName != "Lean" || got.Origin.Scope != workflow.ScopeTenant {
		t.Errorf("task = %+v, want the newly promoted workflow", got)
	}
	task := ws.typed(t, h, bootstrap.TypeTask, "under the new organization scheme")
	if task.Status.Name != bootstrap.StatusInProgress {
		t.Errorf("task opened in %q, want %q", task.Status.Name, bootstrap.StatusInProgress)
	}
}

func TestTheOrganizationAlwaysKeepsAnAnswer(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "backstop")
	schemeID := h.defaultSchemeID(t, ws)

	t.Run("the default scheme cannot be deleted", func(t *testing.T) {
		_, err := ws.admin.DeleteScheme(ws.ctx, schemeID)
		if !errors.Is(err, workflow.ErrInUse) {
			t.Errorf("error = %v, want it refused as in use", err)
		}
	})

	t.Run("it cannot lose its fallback", func(t *testing.T) {
		lean := h.straightThrough(t, ws, "Lean", bootstrap.StatusInProgress, bootstrap.StatusDone)
		bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
		_, _, err := ws.admin.SaveScheme(ws.ctx, schemeID, workflow.SchemeInput{
			Name:  bootstrap.DefaultSchemeName,
			Items: []workflow.SchemeItemInput{{IssueTypeID: &bugType, WorkflowID: lean.ID}},
		}, ws.actor.UserID)
		if !errors.Is(err, workflow.ErrInvalid) {
			t.Errorf("error = %v, want it refused as invalid", err)
		}
	})

	// The service refuses first, so the last line of defence is the database.
	t.Run("nor by going round the service", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE workflow_scheme SET is_default = false WHERE id = $1`, schemeID)
		if err == nil {
			t.Fatal("SQL stood the organization down from its own scheme")
		}
		if !strings.Contains(err.Error(), "one default workflow scheme") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	t.Run("and the fallback mapping cannot be deleted either", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`DELETE FROM workflow_scheme_item WHERE scheme_id = $1 AND issue_type_id IS NULL`, schemeID)
		if err == nil {
			t.Fatal("SQL removed the fallback the whole organization relies on")
		}
		if !strings.Contains(err.Error(), "issue types it does not name") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})
}

// An archived project can be restored, so the scheme it names is still spoken
// for; deleting it would leave the project pointing at nothing.
func TestASchemeAnArchivedProjectNamesIsStillInUse(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "archived")

	lean := h.straightThrough(t, ws, "Lean", bootstrap.StatusInProgress, bootstrap.StatusDone)
	scheme := h.overrideWith(t, ws, "Archived scheme", workflow.SchemeItemInput{WorkflowID: lean.ID})

	if _, err := ws.projects.Archive(ws.ctx, ws.project.Key); err != nil {
		t.Fatalf("archive the project: %v", err)
	}

	_, err := ws.admin.DeleteScheme(ws.ctx, scheme.ID)
	if !errors.Is(err, workflow.ErrInUse) {
		t.Errorf("error = %v, want it refused as in use", err)
	}
	if err != nil && !strings.Contains(err.Error(), ws.project.Key) {
		t.Errorf("error = %q, want it to name %s", err, ws.project.Key)
	}

	// And the listing agrees with the refusal, or the reason is invisible.
	var schemes []workflow.Scheme
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		schemes, err = ws.store.ListSchemes(ctx, tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, sc := range schemes {
		if sc.ID == scheme.ID && !slices.Contains(sc.Projects, ws.project.Key) {
			t.Errorf("the listing says %s is used by %v, but deleting it names %s",
				sc.Name, sc.Projects, ws.project.Key)
		}
	}
}

func TestASchemeCannotBeBorrowedFromAnotherOrganization(t *testing.T) {
	h := newHarness(t)
	mine := h.newWorkspace(t, "mine")
	theirs := h.newWorkspace(t, "theirs")

	lean := h.straightThrough(t, theirs, "Lean", bootstrap.StatusInProgress, bootstrap.StatusDone)
	other, _, err := theirs.admin.CreateScheme(theirs.ctx, workflow.SchemeInput{
		Name:  "Their scheme",
		Items: []workflow.SchemeItemInput{{WorkflowID: lean.ID}},
	}, theirs.actor.UserID)
	if err != nil {
		t.Fatalf("create the other tenant's scheme: %v", err)
	}

	t.Run("the service says it does not exist", func(t *testing.T) {
		_, _, err := mine.projects.SetWorkflowScheme(mine.ctx, mine.project.Key, &other.ID, mine.actor.UserID)
		if err == nil {
			t.Fatal("a project took another organization's scheme")
		}
		if !strings.Contains(err.Error(), "no such workflow scheme") {
			t.Errorf("error = %v, want it reported as missing rather than forbidden", err)
		}
	})

	t.Run("and SQL refuses it too", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE project SET workflow_scheme_id = $2 WHERE id = $1`, mine.project.ID, other.ID)
		if err == nil {
			t.Fatal("SQL let a project borrow another organization's scheme")
		}
		if !strings.Contains(err.Error(), "another organization") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	// A mapping is the other way in: pointing one organization's scheme at
	// another's workflow would resolve issues into a graph nobody there owns.
	t.Run("nor may a mapping point across", func(t *testing.T) {
		mineScheme := h.defaultSchemeID(t, mine)
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO workflow_scheme_item (org_id, scheme_id, issue_type_id, workflow_id)
			VALUES ($1, $2, NULL, $3)`, mine.orgID, mineScheme, lean.ID)
		if err == nil {
			t.Fatal("SQL mapped another organization's workflow")
		}
		if !strings.Contains(err.Error(), "cross organizations") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})
}

func TestDeletingAWorkflowSomethingUsesIsRefused(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "inuse")

	inUse := h.assignmentFor(t, ws, bootstrap.TypeTask).WorkflowID
	if _, err := ws.admin.DeleteWorkflow(ws.ctx, inUse); !errors.Is(err, workflow.ErrInUse) {
		t.Errorf("error = %v, want it refused as in use", err)
	}

	spare := h.straightThrough(t, ws, "Spare", bootstrap.StatusToDo, bootstrap.StatusDone)
	if _, err := ws.admin.DeleteWorkflow(ws.ctx, spare.ID); err != nil {
		t.Errorf("deleting an unused workflow: %v", err)
	}
}

// Rules are attached to transitions, so an edit that kept a transition has to
// keep its rules; a delete and re-insert would throw them away silently.
func TestSavingAWorkflowKeepsTheRulesOnTransitionsItKept(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "rules")

	todo := h.statusID(t, ws, bootstrap.StatusToDo)
	progress := h.statusID(t, ws, bootstrap.StatusInProgress)
	done := h.statusID(t, ws, bootstrap.StatusDone)

	built := h.straightThrough(t, ws, "Guarded", bootstrap.StatusToDo, bootstrap.StatusInProgress)
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO transition_rule (org_id, transition_id, kind, rule_type, config)
		VALUES ($1, $2, 'condition', 'role_is', '{"role":"admin"}')`,
		ws.orgID, built.Transitions[0].ID); err != nil {
		t.Fatalf("attach a rule: %v", err)
	}

	saved, _, err := ws.admin.SaveWorkflow(ws.ctx, built.ID, workflow.GraphInput{
		Name: "Guarded",
		Steps: []workflow.StepInput{
			{StatusID: todo, IsInitial: true},
			{StatusID: progress},
			{StatusID: done},
		},
		Transitions: []workflow.TransitionInput{
			{ID: built.Transitions[0].ID, Name: "Finish", FromStatusID: &todo, ToStatusID: progress},
			{Name: "Close", ToStatusID: done},
		},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("save the workflow: %v", err)
	}

	if len(saved.Transitions) != 2 {
		t.Fatalf("transitions = %d, want 2", len(saved.Transitions))
	}
	for _, tr := range saved.Transitions {
		if tr.ID == built.Transitions[0].ID && len(tr.Rules) != 1 {
			t.Errorf("the kept transition has %d rules, want the one it had", len(tr.Rules))
		}
	}
}

func TestCopyingAWorkflowTakesItsRulesAlong(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "copying")

	source := h.assignmentFor(t, ws, bootstrap.TypeTask).WorkflowID
	copied, _, err := ws.admin.CopyWorkflow(ws.ctx, source, "Copy of the default", ws.actor.UserID)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}

	var original *workflow.Workflow
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		original, err = ws.store.Load(ctx, tx, source)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if len(copied.Steps) != len(original.Steps) || len(copied.Transitions) != len(original.Transitions) {
		t.Fatalf("copy has %d steps and %d transitions, want %d and %d",
			len(copied.Steps), len(copied.Transitions), len(original.Steps), len(original.Transitions))
	}
	if _, err := copied.InitialStep(); err != nil {
		t.Errorf("the copy has no initial step: %v", err)
	}
	// The copy is a separate workflow, not a second name for the same rows.
	for _, step := range copied.Steps {
		for _, was := range original.Steps {
			if step.ID == was.ID {
				t.Fatal("the copy shares a step row with the original")
			}
		}
	}
}

func TestAWorkflowThatCouldNotWorkIsRefused(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "refused")
	todo := h.statusID(t, ws, bootstrap.StatusToDo)
	done := h.statusID(t, ws, bootstrap.StatusDone)

	cases := []struct {
		name  string
		in    workflow.GraphInput
		wants string
	}{
		{
			name:  "with nowhere for new issues to start",
			in:    workflow.GraphInput{Name: "Nowhere", Steps: []workflow.StepInput{{StatusID: todo}}},
			wants: "where new issues start",
		},
		{
			name: "with two starting points",
			in: workflow.GraphInput{Name: "Both", Steps: []workflow.StepInput{
				{StatusID: todo, IsInitial: true}, {StatusID: done, IsInitial: true},
			}},
			wants: "only start in one status",
		},
		{
			name: "with a transition leading out of the workflow",
			in: workflow.GraphInput{Name: "Dangling",
				Steps:       []workflow.StepInput{{StatusID: todo, IsInitial: true}},
				Transitions: []workflow.TransitionInput{{Name: "Finish", ToStatusID: done}},
			},
			wants: "does not contain",
		},
		{
			name:  "with no statuses at all",
			in:    workflow.GraphInput{Name: "Empty"},
			wants: "at least one status",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ws.admin.CreateWorkflow(ws.ctx, tc.in, ws.actor.UserID)
			if !errors.Is(err, workflow.ErrInvalid) {
				t.Fatalf("error = %v, want it refused as invalid", err)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

// defaultSchemeID reads the organization's own scheme.
func (h *harness) defaultSchemeID(t *testing.T, ws *workspace) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM workflow_scheme WHERE is_default`).Scan(&id)
	})
	if err != nil {
		t.Fatalf("read the organization's scheme: %v", err)
	}
	return id
}

// projectSchemeID reads which scheme a project names right now, nil for none.
func (h *harness) projectSchemeID(t *testing.T, ws *workspace, key string) *uuid.UUID {
	t.Helper()
	found, err := ws.projects.ByKey(ws.ctx, key)
	if err != nil {
		t.Fatalf("read project %s: %v", key, err)
	}
	return found.WorkflowSchemeID
}

func TestAProjectAdministratorMapsOneTypeWithoutASchemeOfItsOwn(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "rowwise")

	triage := h.straightThrough(t, ws, "Bug triage", bootstrap.StatusInReview, bootstrap.StatusDone)
	bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)

	updated, _, err := ws.projects.SetWorkflowAssignment(ws.ctx, ws.project.Key, bugType, &triage.ID, ws.actor.UserID)
	if err != nil {
		t.Fatalf("map bugs: %v", err)
	}
	if updated.WorkflowSchemeID == nil {
		t.Fatal("the project has no scheme after mapping a type; the row needs somewhere to live")
	}

	bug := h.assignmentFor(t, ws, bootstrap.TypeBug)
	if bug.WorkflowName != "Bug triage" || bug.Origin.Scope != workflow.ScopeProject || !bug.Origin.Named {
		t.Errorf("bug = %+v, want Bug triage named by the project", bug)
	}
	task := h.assignmentFor(t, ws, bootstrap.TypeTask)
	if task.Origin.Scope != workflow.ScopeTenant {
		t.Errorf("task origin = %+v, want the organization still", task.Origin)
	}

	// The scheme is bookkeeping, but it is named after the project so the
	// organization's list says whose it is.
	var name string
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT name FROM workflow_scheme WHERE id = $1`, *updated.WorkflowSchemeID).Scan(&name)
	}); err != nil {
		t.Fatal(err)
	}
	if name != ws.project.Name+" scheme" {
		t.Errorf("scheme named %q, want %q", name, ws.project.Name+" scheme")
	}

	// A second row edits the same scheme rather than making another.
	epicType := h.issueTypeID(t, ws, bootstrap.TypeEpic)
	again, _, err := ws.projects.SetWorkflowAssignment(ws.ctx, ws.project.Key, epicType, &triage.ID, ws.actor.UserID)
	if err != nil {
		t.Fatalf("map epics: %v", err)
	}
	if *again.WorkflowSchemeID != *updated.WorkflowSchemeID {
		t.Error("mapping a second type made a second scheme")
	}
}

func TestMappingOnASharedSchemeCopiesItFirst(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "shared")

	triage := h.straightThrough(t, ws, "Bug triage", bootstrap.StatusInReview, bootstrap.StatusDone)
	lean := h.straightThrough(t, ws, "Lean", bootstrap.StatusInProgress, bootstrap.StatusDone)
	bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
	taskType := h.issueTypeID(t, ws, bootstrap.TypeTask)
	shared := h.overrideWith(t, ws, "Shared scheme", workflow.SchemeItemInput{IssueTypeID: &bugType, WorkflowID: triage.ID})

	other, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Other", Key: "OTH", SchemeID: shared.ID}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create the second project: %v", err)
	}

	updated, _, err := ws.projects.SetWorkflowAssignment(ws.ctx, ws.project.Key, taskType, &lean.ID, ws.actor.UserID)
	if err != nil {
		t.Fatalf("map tasks: %v", err)
	}
	if updated.WorkflowSchemeID == nil || *updated.WorkflowSchemeID == shared.ID {
		t.Fatalf("the project still points at the shared scheme; another project's rows were edited")
	}

	// The copy kept the bug row, so the project behaves as before for bugs.
	if bug := h.assignmentFor(t, ws, bootstrap.TypeBug); bug.WorkflowName != "Bug triage" {
		t.Errorf("bug = %q after the copy, want Bug triage kept", bug.WorkflowName)
	}
	if task := h.assignmentFor(t, ws, bootstrap.TypeTask); task.WorkflowName != "Lean" {
		t.Errorf("task = %q, want Lean", task.WorkflowName)
	}

	// The other project never noticed.
	if got := h.projectSchemeID(t, ws, other.Key); got == nil || *got != shared.ID {
		t.Errorf("the other project's scheme = %v, want the shared one untouched", got)
	}
	var sharedRows int
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM workflow_scheme_item WHERE scheme_id = $1`, shared.ID).Scan(&sharedRows)
	}); err != nil {
		t.Fatal(err)
	}
	if sharedRows != 1 {
		t.Errorf("the shared scheme has %d rows, want the 1 it started with", sharedRows)
	}
}

func TestUnmappingTheLastTypeHandsTheDecisionBack(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "lastrow")

	triage := h.straightThrough(t, ws, "Bug triage", bootstrap.StatusInReview, bootstrap.StatusDone)
	bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
	mapped, _, err := ws.projects.SetWorkflowAssignment(ws.ctx, ws.project.Key, bugType, &triage.ID, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	own := *mapped.WorkflowSchemeID

	back, _, err := ws.projects.SetWorkflowAssignment(ws.ctx, ws.project.Key, bugType, nil, ws.actor.UserID)
	if err != nil {
		t.Fatalf("unmap bugs: %v", err)
	}
	if back.WorkflowSchemeID != nil {
		t.Errorf("the project still names scheme %v with nothing in it", back.WorkflowSchemeID)
	}
	var stillThere bool
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflow_scheme WHERE id = $1)`, own).Scan(&stillThere)
	}); err != nil {
		t.Fatal(err)
	}
	if stillThere {
		t.Error("the empty scheme was left behind")
	}
	if bug := h.assignmentFor(t, ws, bootstrap.TypeBug); bug.Origin.Scope != workflow.ScopeTenant {
		t.Errorf("bug origin = %+v, want the organization again", bug.Origin)
	}
}

func TestAnotherOrganizationsWorkflowCannotBeMappedRowwise(t *testing.T) {
	h := newHarness(t)
	mine := h.newWorkspace(t, "minerow")
	theirs := h.newWorkspace(t, "theirrow")

	lean := h.straightThrough(t, theirs, "Lean", bootstrap.StatusInProgress, bootstrap.StatusDone)
	bugType := h.issueTypeID(t, mine, bootstrap.TypeBug)

	_, _, err := mine.projects.SetWorkflowAssignment(mine.ctx, mine.project.Key, bugType, &lean.ID, mine.actor.UserID)
	if err == nil {
		t.Fatal("a project mapped another organization's workflow")
	}
	if !errors.Is(err, workflow.ErrInvalid) || !strings.Contains(err.Error(), "this organization's workflows") {
		t.Errorf("error = %v, want a sentence naming what to choose instead", err)
	}
	// Nothing half done: no scheme was left behind by the refused write.
	if got := h.projectSchemeID(t, mine, mine.project.Key); got != nil {
		t.Errorf("the project names scheme %v after a refused mapping", got)
	}
}
