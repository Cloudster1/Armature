//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
)

// fromTemplate makes a second project in the workspace's organization, the way
// the template says.
func (ws *workspace) fromTemplate(t *testing.T, h *harness, key, name string) *project.Project {
	t.Helper()
	templates := template.NewService(h.cluster, ws.projects, ws.admin)
	created, _, err := templates.Create(ws.ctx, key, project.CreateInput{
		Name: name,
		Key:  strings.ToUpper(name[:min(len(name), 3)] + uuid.New().String()[:3]),
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create a project from the %q template: %v", key, err)
	}
	return created
}

// issueIn files an issue in a project other than the workspace's own.
func (ws *workspace) issueIn(t *testing.T, projectKey, summary string) string {
	t.Helper()
	created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: projectKey, Summary: summary}, ws.actor)
	if err != nil {
		t.Fatalf("create %q: %v", summary, err)
	}
	return created.Key
}

// boardOf reads the board a project opens on.
func (ws *workspace) boardOf(t *testing.T, projectKey string) *board.Board {
	t.Helper()
	b, err := ws.boards.ForProject(ws.ctx, projectKey)
	if err != nil {
		t.Fatalf("load the board of %s: %v", projectKey, err)
	}
	return b
}

// runnable makes a sprint with dates, so that it can be started.
func (ws *workspace) runnable(t *testing.T, projectKey, name string, teamID *uuid.UUID) *sprint.Sprint {
	t.Helper()
	from := time.Now().UTC().AddDate(0, 0, -1)
	to := from.AddDate(0, 0, 14)
	created, _, err := ws.sprints.Create(ws.ctx, projectKey, sprint.CreateInput{
		Name: name, StartsOn: &from, EndsOn: &to, TeamID: teamID,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create sprint %q: %v", name, err)
	}
	return created
}

func (ws *workspace) start(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, _, err := ws.sprints.Start(ws.ctx, id, ws.actor.UserID); err != nil {
		t.Fatalf("start sprint: %v", err)
	}
}

func laneNames(b *board.Board) string {
	var names []string
	for _, lane := range b.Swimlanes {
		names = append(names, lane.Name)
	}
	return strings.Join(names, ",")
}

func TestEachTemplateShapesTheProject(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "shaping")

	for _, tpl := range template.All() {
		t.Run(tpl.Name, func(t *testing.T) {
			p := ws.fromTemplate(t, h, tpl.Key, tpl.Name+" project")

			if p.Kind != tpl.Kind {
				t.Errorf("kind = %q, want the template's %q", p.Kind, tpl.Kind)
			}
			if p.Template != tpl.Key {
				t.Errorf("project remembers template %q, want %q", p.Template, tpl.Key)
			}

			b := ws.boardOf(t, p.Key)
			if b.Type != tpl.BoardType {
				t.Errorf("first board is %s, want the template's %s", b.Type, tpl.BoardType)
			}

			if tpl.WorkflowName == "" {
				if p.WorkflowSchemeID != nil {
					t.Error("a template with no workflow of its own gave the project a scheme")
				}
				return
			}
			if p.WorkflowSchemeID == nil {
				t.Fatal("the template brings a workflow but the project follows the organization")
			}
			// The board is built over the template's workflow, not the
			// organization's.
			want := map[string]string{
				"task-tracking": strings.Join([]string{bootstrap.StatusToDo, bootstrap.StatusInProgress, bootstrap.StatusDone}, ","),
				"service-desk":  "Waiting for support,In Progress,Waiting for customer,Resolved",
			}[tpl.Key]
			if got := laneNames(b); got != want {
				t.Errorf("swimlanes = %v, want %v", got, want)
			}

			var resolved string
			err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
				wf, err := ws.store.ForIssueType(ctx, tx, p.ID, h.issueTypeID(t, ws, bootstrap.TypeTask))
				if err != nil {
					return err
				}
				resolved = wf.Name
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if resolved != tpl.WorkflowName {
				t.Errorf("a task in the project follows %q, want %q", resolved, tpl.WorkflowName)
			}
		})
	}
}

// Two projects from the same template share the workflow the first one brought,
// rather than the second failing on the name or quietly making a copy.
func TestATemplateBringsItsWorkflowOnce(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "reusing")

	first := ws.fromTemplate(t, h, "task-tracking", "Chores")
	second := ws.fromTemplate(t, h, "task-tracking", "Errands")

	if *first.WorkflowSchemeID != *second.WorkflowSchemeID {
		t.Errorf("the two projects have different schemes, want the same one")
	}

	var workflows, schemes int
	err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM workflow WHERE name = 'Simple task workflow'`).Scan(&workflows); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM workflow_scheme WHERE name = 'Task tracking workflows'`).Scan(&schemes)
	})
	if err != nil {
		t.Fatal(err)
	}
	if workflows != 1 || schemes != 1 {
		t.Errorf("%d workflows and %d schemes by the template's names, want one of each", workflows, schemes)
	}
}

func TestAnUnknownTemplateIsRefused(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "unknown")

	templates := template.NewService(h.cluster, ws.projects, ws.admin)
	_, _, err := templates.Create(ws.ctx, "waterfall", project.CreateInput{Name: "Never"}, ws.actor.UserID)
	if !errors.Is(err, template.ErrUnknown) {
		t.Errorf("err = %v, want ErrUnknown", err)
	}

	// And over the API it is the caller's mistake, not the server's.
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "templated")
	r := c.post("/api/v1/projects", map[string]any{"name": "Never", "template": "waterfall"})
	if r.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", r.Status, r.Raw)
	}

	r = c.get("/api/v1/project-templates")
	if r.Status != http.StatusOK {
		t.Fatalf("list templates: %d", r.Status)
	}
	if got := len(r.Body["templates"].([]any)); got != len(template.All()) {
		t.Errorf("the API offers %d templates, the package has %d", got, len(template.All()))
	}
}

// TestAScrumBoardShowsTheRunningSprint is what makes the type behaviour rather
// than a label: the same issues, two boards, two different answers.
func TestAScrumBoardShowsTheRunningSprint(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "sprinting")
	p := ws.fromTemplate(t, h, "scrum", "Sprinting")

	a := ws.issueIn(t, p.Key, "committed a")
	b := ws.issueIn(t, p.Key, "committed b")
	ws.issueIn(t, p.Key, "left in the backlog")

	flow, _, err := ws.boards.CreateBoard(ws.ctx, p.Key, board.CreateInput{Name: "Flow", Type: board.TypeKanban}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	kanban := func() []string {
		loaded, err := ws.boards.ByID(ws.ctx, flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		return cardKeys(loaded)
	}

	t.Run("with no sprint the scrum board is empty and the kanban board is not", func(t *testing.T) {
		scrum := ws.boardOf(t, p.Key)
		if scrum.Type != board.TypeScrum {
			t.Fatalf("the project's board is %s", scrum.Type)
		}
		if got := cardKeys(scrum); len(got) != 0 {
			t.Errorf("scrum board shows %v before any sprint runs", got)
		}
		if scrum.Sprint != nil {
			t.Errorf("scrum board names a sprint %q when none is running", scrum.Sprint.Name)
		}
		if got := len(kanban()); got != 3 {
			t.Errorf("kanban board shows %d cards, want all 3", got)
		}
	})

	planned := ws.runnable(t, p.Key, "Sprint 1", nil)
	ws.commit(t, a, &planned.ID, nil)
	ws.commit(t, b, &planned.ID, nil)

	t.Run("planning is not running", func(t *testing.T) {
		if got := cardKeys(ws.boardOf(t, p.Key)); len(got) != 0 {
			t.Errorf("a planned sprint's work is already on the board: %v", got)
		}
	})

	ws.start(t, planned.ID)

	t.Run("a running sprint's work is the board", func(t *testing.T) {
		scrum := ws.boardOf(t, p.Key)
		got := strings.Join(cardKeys(scrum), ",")
		if got != a+","+b {
			t.Errorf("scrum board shows %q, want %s and %s", got, a, b)
		}
		if scrum.Sprint == nil || scrum.Sprint.Name != "Sprint 1" {
			t.Errorf("scrum board names %+v, want Sprint 1", scrum.Sprint)
		}
		if got := len(kanban()); got != 3 {
			t.Errorf("kanban board shows %d cards, want all 3 regardless", got)
		}
	})

	if _, _, err := ws.sprints.Complete(ws.ctx, planned.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	t.Run("a completed sprint leaves the board empty again", func(t *testing.T) {
		if got := cardKeys(ws.boardOf(t, p.Key)); len(got) != 0 {
			t.Errorf("scrum board still shows %v", got)
		}
	})

	t.Run("switching the type switches what is shown", func(t *testing.T) {
		kind := board.TypeKanban
		updated, _, err := ws.boards.UpdateBoard(ws.ctx, ws.boardOf(t, p.Key).ID, board.UpdateBoardInput{Type: &kind}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(cardKeys(updated)); got != 3 {
			t.Errorf("as kanban the same board shows %d cards, want 3", got)
		}
	})
}

// A project with teams has a sprint stream per team and one of its own. A
// team's scrum board follows its team's sprint, not whichever is running.
func TestATeamsScrumBoardFollowsTheTeamsSprint(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "streams")
	p := ws.fromTemplate(t, h, "scrum", "Streams")

	platform, _, err := ws.teams.Create(ws.ctx, p.Key, team.CreateInput{Name: "Platform"}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	mine := ws.issueIn(t, p.Key, "platform work")
	other := ws.issueIn(t, p.Key, "nobody's work")
	if _, _, err := ws.issues.SetTeam(ws.ctx, mine, &platform.ID, ws.actor); err != nil {
		t.Fatal(err)
	}

	// The team's board says no type, and takes after the project's.
	teamBoard, _, err := ws.boards.CreateBoard(ws.ctx, p.Key, board.CreateInput{Name: "Platform board", TeamID: &platform.ID}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if teamBoard.Type != board.TypeScrum {
		t.Errorf("a board added to a scrum project without a type is %s", teamBoard.Type)
	}

	theirs := ws.runnable(t, p.Key, "Platform sprint", &platform.ID)
	ours := ws.runnable(t, p.Key, "Project sprint", nil)
	ws.commit(t, mine, &theirs.ID, nil)
	ws.commit(t, other, &ours.ID, nil)
	ws.start(t, theirs.ID)
	ws.start(t, ours.ID)

	loaded, err := ws.boards.ByID(ws.ctx, teamBoard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cardKeys(loaded), ","); got != mine {
		t.Errorf("team board shows %q, want only %s", got, mine)
	}
	if loaded.Sprint == nil || loaded.Sprint.Name != "Platform sprint" {
		t.Errorf("team board follows %+v, want the team's sprint", loaded.Sprint)
	}

	whole := ws.boardOf(t, p.Key)
	if got := strings.Join(cardKeys(whole), ","); got != other {
		t.Errorf("the project's own board shows %q, want only %s", got, other)
	}
	if whole.Sprint == nil || whole.Sprint.Name != "Project sprint" {
		t.Errorf("the project's board follows %+v, want its own sprint", whole.Sprint)
	}
}

func TestABoardIsEitherScrumOrKanban(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "typed")

	t.Run("a project made without a template opens on a kanban board", func(t *testing.T) {
		if got := ws.boardOf(t, ws.project.Key).Type; got != board.TypeKanban {
			t.Errorf("type = %s, want kanban", got)
		}
	})

	t.Run("the service refuses a third kind", func(t *testing.T) {
		_, _, err := ws.boards.CreateBoard(ws.ctx, ws.project.Key, board.CreateInput{Name: "Odd", Type: "waterfall"}, ws.actor.UserID)
		if !errors.Is(err, board.ErrBadType) {
			t.Errorf("err = %v, want ErrBadType", err)
		}
		kind := board.Type("waterfall")
		_, _, err = ws.boards.UpdateBoard(ws.ctx, ws.boardOf(t, ws.project.Key).ID, board.UpdateBoardInput{Type: &kind}, ws.actor.UserID)
		if !errors.Is(err, board.ErrBadType) {
			t.Errorf("update err = %v, want ErrBadType", err)
		}
	})

	t.Run("and so does the database", func(t *testing.T) {
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE board SET type = 'waterfall' WHERE id = $1`, ws.boardOf(t, ws.project.Key).ID)
			return err
		})
		if err == nil {
			t.Error("the database accepted a board type the enum does not have")
		}
	})
}
