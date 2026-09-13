//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/workflow"
)

// TestOrgBootstrap checks that signing up leaves an organization able to do
// something, rather than one that needs configuring before it can hold a single
// issue.
func TestOrgBootstrap(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "bootstrapped")

	t.Run("statuses, types and a workflow all exist", func(t *testing.T) {
		var statuses, types, workflows, schemes, links int
		err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `
				SELECT (SELECT count(*) FROM issue_status),
				       (SELECT count(*) FROM issue_type),
				       (SELECT count(*) FROM workflow),
				       (SELECT count(*) FROM workflow_scheme),
				       (SELECT count(*) FROM issue_link_type)`).
				Scan(&statuses, &types, &workflows, &schemes, &links)
		})
		if err != nil {
			t.Fatal(err)
		}
		if statuses != 4 || types != 6 || workflows != 1 || schemes != 1 || links != 3 {
			t.Errorf("bootstrap produced %d statuses, %d types, %d workflows, %d schemes, %d link types",
				statuses, types, workflows, schemes, links)
		}
	})

	t.Run("the default workflow is coherent", func(t *testing.T) {
		var wf *workflow.Workflow
		err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM workflow LIMIT 1`).Scan(&id); err != nil {
				return err
			}
			var err error
			wf, err = ws.store.Load(ctx, tx, id)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		// The engine's own validation is the standard the workflow editor will
		// hold user-built workflows to, so the shipped default must meet it.
		if err := ws.engine.Validate(wf); err != nil {
			t.Errorf("the default workflow does not pass validation: %v", err)
		}
		if len(wf.Steps) != 4 {
			t.Errorf("got %d steps, want 4", len(wf.Steps))
		}
		initial, err := wf.InitialStep()
		if err != nil {
			t.Fatal(err)
		}
		if initial.Status.Name != bootstrap.StatusToDo {
			t.Errorf("new issues start in %q, want %q", initial.Status.Name, bootstrap.StatusToDo)
		}
	})
}

func TestProjects(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "projects")

	t.Run("a project starts empty and is listed", func(t *testing.T) {
		list, err := ws.projects.List(ws.ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Fatalf("got %d projects, want 1", len(list))
		}
		if list[0].IssueCount != 0 {
			t.Errorf("a new project reports %d issues", list[0].IssueCount)
		}
	})

	t.Run("keys are unique within an organization", func(t *testing.T) {
		_, _, err := ws.projects.Create(ws.ctx, project.CreateInput{
			Name: "Another", Key: ws.project.Key,
		}, ws.actor.UserID)
		if !errors.Is(err, project.ErrKeyTaken) {
			t.Fatalf("error = %v, want ErrKeyTaken", err)
		}
	})

	t.Run("a malformed key is refused before it reaches the database", func(t *testing.T) {
		for _, key := range []string{"A", "TOOLONGAKEY", "WITH SPACE", "WITH-DASH", "1ST"} {
			if _, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "X", Key: key}, ws.actor.UserID); err == nil {
				t.Errorf("key %q was accepted", key)
			}
		}
	})

	// Typing the key in lower case is not a mistake worth rejecting.
	t.Run("a lower case key is accepted and stored upper case", func(t *testing.T) {
		p, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Lowercase", Key: "lwr"}, ws.actor.UserID)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.Key != "LWR" {
			t.Errorf("key = %q, want LWR", p.Key)
		}
	})

	t.Run("archiving hides a project without destroying it", func(t *testing.T) {
		p, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Temporary", Key: "TMPX"}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: p.Key, Summary: "still here"}, ws.actor); err != nil {
			t.Fatal(err)
		}

		if _, err := ws.projects.Archive(ws.ctx, p.Key); err != nil {
			t.Fatalf("archive: %v", err)
		}

		active, err := ws.projects.List(ws.ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range active {
			if item.Key == p.Key {
				t.Error("an archived project is still in the default listing")
			}
		}

		all, err := ws.projects.List(ws.ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		var found *project.Project
		for i := range all {
			if all[i].Key == p.Key {
				found = &all[i]
			}
		}
		if found == nil {
			t.Fatal("the archived project is not listed even when asked for")
		}
		if found.IssueCount != 1 {
			t.Errorf("the archived project reports %d issues, want its 1 to be intact", found.IssueCount)
		}

		// Creating in an archived project must fail: it is closed, not deleted.
		if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: p.Key, Summary: "no"}, ws.actor); !errors.Is(err, project.ErrArchived) {
			t.Errorf("error = %v, want ErrArchived", err)
		}

		if _, err := ws.projects.Restore(ws.ctx, p.Key); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: p.Key, Summary: "back"}, ws.actor); err != nil {
			t.Errorf("creating in a restored project failed: %v", err)
		}
	})

	t.Run("a project in another organization is not found", func(t *testing.T) {
		other := h.newWorkspace(t, "elsewhere")
		if _, err := ws.projects.ByKey(ws.ctx, other.project.Key); !errors.Is(err, project.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestIssueKeys(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "keys")

	t.Run("keys count up from one", func(t *testing.T) {
		for want := 1; want <= 3; want++ {
			created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
				ProjectKey: ws.project.Key,
				Summary:    fmt.Sprintf("issue number %d", want),
			}, ws.actor)
			if err != nil {
				t.Fatal(err)
			}
			if got := created.Key; got != issue.FormatKey(ws.project.Key, int64(want)) {
				t.Errorf("key = %q, want %s-%d", got, ws.project.Key, want)
			}
		}
	})

	// Two people creating at once must never be handed the same key. The
	// counter lives on the project row and is bumped inside the same
	// transaction as the insert, so the row lock serialises them.
	t.Run("concurrent creation never duplicates a key", func(t *testing.T) {
		concurrent := h.newWorkspace(t, "racing")

		const writers = 12
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			keys []string
			errs []error
		)
		for i := range writers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				created, _, err := concurrent.issues.Create(concurrent.ctx, issue.CreateInput{
					ProjectKey: concurrent.project.Key,
					Summary:    fmt.Sprintf("concurrent %d", i),
				}, concurrent.actor)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
					return
				}
				keys = append(keys, created.Key)
			}()
		}
		wg.Wait()

		if len(errs) > 0 {
			t.Fatalf("%d of %d concurrent creations failed, first: %v", len(errs), writers, errs[0])
		}
		seen := map[string]bool{}
		for _, key := range keys {
			if seen[key] {
				t.Fatalf("key %s was handed out twice", key)
			}
			seen[key] = true
		}
		if len(seen) != writers {
			t.Errorf("got %d distinct keys from %d creations", len(seen), writers)
		}
	})

	// A rolled back creation must not burn a number, or the sequence develops
	// holes that look like deleted issues.
	t.Run("a failed creation does not consume a key", func(t *testing.T) {
		ws2 := h.newWorkspace(t, "gapless")
		if _, _, err := ws2.issues.Create(ws2.ctx, issue.CreateInput{ProjectKey: ws2.project.Key, Summary: "first"}, ws2.actor); err != nil {
			t.Fatal(err)
		}

		// An empty summary is refused before the transaction starts, and a bad
		// parent is refused inside it. The second is the interesting case.
		if _, _, err := ws2.issues.Create(ws2.ctx, issue.CreateInput{
			ProjectKey: ws2.project.Key, Summary: "doomed", ParentKey: "NOSUCH-9",
		}, ws2.actor); err == nil {
			t.Fatal("an issue with a nonexistent parent was created")
		}

		next, _, err := ws2.issues.Create(ws2.ctx, issue.CreateInput{ProjectKey: ws2.project.Key, Summary: "second"}, ws2.actor)
		if err != nil {
			t.Fatal(err)
		}
		if next.Key != issue.FormatKey(ws2.project.Key, 2) {
			t.Errorf("key = %q, want %s-2: the rolled back attempt consumed a number", next.Key, ws2.project.Key)
		}
	})

	t.Run("each project has its own sequence", func(t *testing.T) {
		second, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Second", Key: "SECX"}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: second.Key, Summary: "first here"}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if created.Key != "SECX-1" {
			t.Errorf("key = %q, want SECX-1", created.Key)
		}
	})
}

func TestIssueCreation(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "creating")

	t.Run("a new issue starts in the workflow's initial status", func(t *testing.T) {
		created, lsn, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, Summary: "a fresh issue",
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if created.Status.Name != bootstrap.StatusToDo {
			t.Errorf("status = %q, want %q", created.Status.Name, bootstrap.StatusToDo)
		}
		if created.Status.Category != workflow.CategoryTodo {
			t.Errorf("category = %q, want todo", created.Status.Category)
		}
		if created.Reporter == nil || created.Reporter.ID != ws.actor.UserID {
			t.Error("the creator was not recorded as the reporter")
		}
		if created.Priority != issue.PriorityMedium {
			t.Errorf("priority = %q, want the medium default", created.Priority)
		}
		if lsn == 0 {
			t.Error("creation returned no log position")
		}
	})

	t.Run("the creation is the first changelog entry", func(t *testing.T) {
		created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, Summary: "logged from birth",
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		history, err := ws.issues.History(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 1 || len(history[0].Changes) != 1 || history[0].Changes[0].Field != "created" {
			t.Errorf("history = %+v, want a single created entry", history)
		}
	})

	t.Run("a summary is required and bounded", func(t *testing.T) {
		for _, summary := range []string{"", "   ", strings.Repeat("x", 256)} {
			if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
				ProjectKey: ws.project.Key, Summary: summary,
			}, ws.actor); err == nil {
				t.Errorf("a summary of length %d was accepted", len(summary))
			}
		}
	})

	t.Run("a subtask needs a parent, and a parent cannot be a subtask", func(t *testing.T) {
		subtaskType := h.issueTypeID(t, ws, bootstrap.TypeSubtask)

		if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, Summary: "orphan subtask", TypeID: subtaskType,
		}, ws.actor); !errors.Is(err, issue.ErrNeedsParent) {
			t.Errorf("error = %v, want ErrNeedsParent", err)
		}

		parent, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, Summary: "the parent",
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		child, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, Summary: "the child", TypeID: subtaskType, ParentKey: parent.Key,
		}, ws.actor)
		if err != nil {
			t.Fatalf("create subtask: %v", err)
		}
		if child.ParentKey != parent.Key {
			t.Errorf("parent = %q, want %q", child.ParentKey, parent.Key)
		}

		// Nothing sits below a subtask, so it cannot be a parent.
		if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, Summary: "grandchild", TypeID: subtaskType, ParentKey: child.Key,
		}, ws.actor); !errors.Is(err, issue.ErrParentLevel) {
			t.Errorf("error = %v, want ErrParentLevel", err)
		}

		children, err := ws.issues.Children(ws.ctx, parent.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(children) != 1 || children[0].Key != child.Key {
			t.Errorf("children = %v, want just the child", children)
		}
	})

	t.Run("creating in a project that does not exist is refused", func(t *testing.T) {
		if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: "NOPEX", Summary: "nowhere",
		}, ws.actor); !errors.Is(err, project.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})
}

// An issue can be filed where it will sit: on a team, sized, and in a sprint,
// in one request, so the plan can make a ticket in a sprint's own row.
func TestAnIssueCanBeFiledStraightIntoASprint(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "filed-into")
	alpha := ws.newTeam(t, "Alpha")
	sp := ws.planned(t, h, "Sprint F", "2026-09-07", "2026-09-18", nil)
	three := 3.0

	filed, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key, Summary: "straight in", SprintID: &sp.ID, TeamID: &alpha.ID, Estimate: &three,
	}, ws.actor)
	if err != nil {
		t.Fatalf("file into a sprint: %v", err)
	}
	if filed.SprintID == nil || *filed.SprintID != sp.ID || filed.TeamID == nil || *filed.TeamID != alpha.ID || filed.Estimate == nil || *filed.Estimate != 3 {
		t.Errorf("issue = sprint %v team %v estimate %v, want all three set", filed.SprintID, filed.TeamID, filed.Estimate)
	}
	if got := ws.planOf(t); len(got.Sprints) != 1 || got.Sprints[0].Committed != 3 {
		t.Errorf("the plan's sprint = %+v, want the 3 points committed", got.Sprints)
	}

	t.Run("another project's sprint is refused", func(t *testing.T) {
		other := h.newWorkspace(t, "filed-elsewhere")
		theirs := other.planned(t, h, "Theirs", "2026-09-07", "2026-09-18", nil)
		_, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, Summary: "misfiled", SprintID: &theirs.ID}, ws.actor)
		if !errors.Is(err, issue.ErrSprintNotFound) {
			t.Errorf("err = %v, want ErrSprintNotFound", err)
		}
	})

	t.Run("a finished sprint is refused, by the service and by the database", func(t *testing.T) {
		ws.start(t, sp.ID)
		if _, _, err := ws.sprints.Complete(ws.ctx, sp.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		_, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, Summary: "too late", SprintID: &sp.ID}, ws.actor)
		if !errors.Is(err, issue.ErrSprintClosed) {
			t.Errorf("err = %v, want ErrSprintClosed", err)
		}
		_, err = h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE issue SET sprint_id = $2 WHERE id = $1`, filed.ID, sp.ID)
			return err
		})
		if err == nil {
			t.Error("the database let work into a finished sprint")
		}
	})

	t.Run("a negative estimate is refused by the database too", func(t *testing.T) {
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE issue SET estimate = -1 WHERE id = $1`, filed.ID)
			return err
		})
		if err == nil {
			t.Error("the check constraint let a negative estimate through")
		}
	})
}
