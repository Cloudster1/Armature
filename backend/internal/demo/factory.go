// Package demo fills a fresh organization with a made-up but plausible
// operation, so somebody can show the product with something in it rather
// than explain an empty state.
package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/workflow"
)

// FactoryKey is the demo project's key, so a caller can send the reader there.
const FactoryKey = "LINE"

// Factory fills the organization the principal is in with a production line:
// a changeover under way, machines that stop, a scrap rate to bring down, the
// shift that owns the work, its maintenance windows and the milestones the
// plant is aiming at. It runs once per organization; the key is taken after.
func Factory(ctx context.Context, cluster *db.Cluster, p *auth.Principal, log *slog.Logger) (*project.Project, error) {
	orgCtx := db.PinPrimary(auth.ContextForOrg(ctx, p))
	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	projects := project.NewService(cluster, board.Provisioner{}, report.Provisioner{})
	templates := template.NewService(cluster, projects, workflow.NewAdmin(cluster, workflow.NewStore()))
	issues := issue.NewService(cluster, engine, workflow.NewStore())
	actor := issue.Actor{UserID: p.User.ID, OrgRole: p.Role}

	created, _, err := templates.Create(orgCtx, "scrum", project.CreateInput{
		Key:         FactoryKey,
		Name:        "Production line 3",
		Description: "The line that builds the housings: six stations, two robots, one paint cell, two shifts.",
		LeadID:      &actor.UserID,
	}, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("create the demo project: %w", err)
	}
	types, err := issueTypes(orgCtx, cluster)
	if err != nil {
		return nil, err
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(offset int) *time.Time {
		d := today.AddDate(0, 0, offset)
		return &d
	}
	pts := func(v float64) *float64 { return &v }

	work := []struct {
		summary, description, typeName, parent string
		priority                               issue.Priority
		start, due                             *time.Time
		estimate                               *float64
		labels                                 []string
		advance                                []string
	}{
		{"Changeover to housing model B", "Model B starts production in six weeks. Every station needs its fixtures, work instructions and torque values changed, and the paint cell needs the new colour qualified.\nDone means the first hundred model B housings pass final inspection without a rework loop.", bootstrap.TypeInitiative, "", issue.PriorityHigh, nil, nil, nil, nil, nil},
		{"Fixtures and work instructions", "Everything on the stations themselves: fixtures, gauges, the instructions on the screens.", bootstrap.TypeEpic, "Changeover to housing model B", issue.PriorityHigh, nil, nil, nil, nil, []string{"Start progress"}},
		{"Paint cell qualification", "The new colour needs a qualified recipe: film thickness, cure time, adhesion.", bootstrap.TypeEpic, "Changeover to housing model B", issue.PriorityMedium, nil, nil, nil, nil, nil},
		{"Unplanned downtime on line 3", "The line lost eleven hours last month to stops nobody planned. Bring that under four.", bootstrap.TypeEpic, "", issue.PriorityHigh, nil, nil, nil, nil, []string{"Start progress"}},

		{"Replace the worn conveyor belt between stations 4 and 5", "The belt slips under load and station 5 waits for parts. The spare arrived on Tuesday.\nNeeds a two hour stop; plan it into the maintenance window.", bootstrap.TypeTask, "Unplanned downtime on line 3", issue.PriorityHigh, day(-3), day(2), pts(3), []string{"maintenance", "downtime"}, []string{"Start progress"}},
		{"Welding cell 2 stops intermittently", "Three stops this week, each about ten minutes, no fault code that repeats. The PLC log shows the safety circuit opening; the door switch is the suspect.", bootstrap.TypeBug, "Unplanned downtime on line 3", issue.PriorityHighest, day(-5), day(1), pts(5), []string{"downtime", "electrical"}, []string{"Start progress"}},
		{"Scrap rate on the paint line above target", "Scrap ran at 2.4 percent last week against a target of 1 percent. Most of it is orange peel on the left side of the housing, which points at the gun angle on robot R2.", bootstrap.TypeBug, "Paint cell qualification", issue.PriorityHigh, day(-2), day(9), pts(8), []string{"quality"}, []string{"Start progress"}},
		{"Update the work instructions for stations 1 to 6", "As a station operator, I want the screen to show the model B steps so that I build the right housing without asking.", bootstrap.TypeStory, "Fixtures and work instructions", issue.PriorityMedium, day(1), day(14), pts(8), []string{"changeover"}, []string{"Start progress"}},
		{"Verify the model B fixtures on station 3", "", bootstrap.TypeSubtask, "Update the work instructions for stations 1 to 6", issue.PriorityMedium, day(1), day(5), pts(2), nil, []string{"Start progress"}},
		{"Load the new torque values into the tools on station 6", "", bootstrap.TypeSubtask, "Update the work instructions for stations 1 to 6", issue.PriorityMedium, day(6), day(10), pts(2), nil, nil},
		{"Qualify the model B paint recipe", "Film thickness, cure time and adhesion on twenty sample housings, signed off by quality.", bootstrap.TypeStory, "Paint cell qualification", issue.PriorityMedium, day(10), day(28), pts(13), []string{"quality", "changeover"}, nil},
		{"Calibrate the torque tools on station 7", "Annual calibration is due; the certificates expire at the end of the month.", bootstrap.TypeTask, "", issue.PriorityMedium, day(3), day(12), pts(2), []string{"maintenance"}, nil},
		{"Firmware update on the filling machine", "The vendor's update fixes the false low-level alarm. Needs the machine empty and a restart.", bootstrap.TypeTask, "Unplanned downtime on line 3", issue.PriorityLow, day(14), day(21), pts(3), []string{"maintenance", "electrical"}, nil},
		{"Safety audit of the packaging cell", "Quarterly audit with the safety officer: guards, light curtains, emergency stops.", bootstrap.TypeTask, "", issue.PriorityMedium, day(7), day(8), pts(2), []string{"safety"}, nil},
		{"Train shift B on the new HMI screens", "As a shift leader, I want shift B trained on the new screens so that the night shift runs the line the same way the day shift does.", bootstrap.TypeStory, "Fixtures and work instructions", issue.PriorityLow, day(20), day(26), pts(5), []string{"changeover"}, nil},
		{"Order spare gripper fingers for robot R3", "", bootstrap.TypeTask, "", issue.PriorityLowest, nil, nil, pts(1), []string{"maintenance"}, []string{"Close"}},
	}

	labels := label.NewService(cluster)
	keys := map[string]string{}
	for _, w := range work {
		in := issue.CreateInput{
			ProjectKey: created.Key, Summary: w.summary, TypeID: types[w.typeName], ParentKey: keys[w.parent],
			Priority: w.priority, StartDate: w.start, DueDate: w.due, Estimate: w.estimate,
		}
		if w.description != "" {
			in.Description = paragraphs(w.description)
		}
		made, _, err := issues.Create(orgCtx, in, actor)
		if err != nil {
			return nil, fmt.Errorf("create %q: %w", w.summary, err)
		}
		keys[w.summary] = made.Key
		if len(w.labels) > 0 {
			if _, _, err := labels.SetIssueLabels(orgCtx, made.Key, w.labels, actor); err != nil {
				return nil, fmt.Errorf("label %s: %w", made.Key, err)
			}
		}
		for _, name := range w.advance {
			if err := advance(orgCtx, issues, made.Key, name, actor); err != nil {
				return nil, err
			}
		}
	}

	// The shift that runs the line owns the work, and its maintenance
	// windows are the sprints: one just closed, one in flight, one planned.
	teams := team.NewService(cluster)
	shift, _, err := teams.Create(orgCtx, created.Key, team.CreateInput{Name: "Shift A", Description: "Runs line 3 by day and owns its maintenance."}, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("form the shift: %w", err)
	}
	if _, _, err := teams.AddMember(orgCtx, shift.ID, actor.UserID, true, actor.UserID); err != nil {
		return nil, fmt.Errorf("put the owner on the shift: %w", err)
	}
	for _, summary := range []string{
		"Replace the worn conveyor belt between stations 4 and 5", "Welding cell 2 stops intermittently",
		"Calibrate the torque tools on station 7", "Firmware update on the filling machine", "Order spare gripper fingers for robot R3",
	} {
		if _, _, err := issues.SetTeam(orgCtx, keys[summary], &shift.ID, actor); err != nil {
			return nil, fmt.Errorf("hand %q to the shift: %w", summary, err)
		}
	}

	sprints := sprint.NewService(cluster)
	sprints.CountWith(plan.NewService(issues, sprints))
	capacity := 20.0
	closed, _, err := sprints.Create(orgCtx, created.Key, sprint.CreateInput{Name: "Maintenance window 38", Goal: "Spares in, tools checked.", StartsOn: day(-21), EndsOn: day(-8), Capacity: &capacity, TeamID: &shift.ID}, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("create the closed window: %w", err)
	}
	running, _, err := sprints.Create(orgCtx, created.Key, sprint.CreateInput{Name: "Maintenance window 40", Goal: "Stop the stops: belt, welding cell, torque tools.", StartsOn: day(-7), EndsOn: day(6), Capacity: &capacity, TeamID: &shift.ID}, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("create the running window: %w", err)
	}
	next, _, err := sprints.Create(orgCtx, created.Key, sprint.CreateInput{Name: "Maintenance window 42", StartsOn: day(7), EndsOn: day(20), Capacity: &capacity, TeamID: &shift.ID}, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("create the next window: %w", err)
	}
	if _, _, err := sprints.Start(orgCtx, closed.ID, actor.UserID); err != nil {
		return nil, fmt.Errorf("start the closed window: %w", err)
	}
	if _, _, err := issues.SetSprint(orgCtx, keys["Order spare gripper fingers for robot R3"], &closed.ID, actor); err != nil {
		return nil, err
	}
	if _, _, err := sprints.Complete(orgCtx, closed.ID, sprint.CompleteInput{MoveTo: &running.ID}, actor.UserID); err != nil {
		return nil, fmt.Errorf("complete the closed window: %w", err)
	}
	for _, summary := range []string{"Replace the worn conveyor belt between stations 4 and 5", "Welding cell 2 stops intermittently", "Calibrate the torque tools on station 7"} {
		if _, _, err := issues.SetSprint(orgCtx, keys[summary], &running.ID, actor); err != nil {
			return nil, err
		}
	}
	if _, _, err := sprints.Start(orgCtx, running.ID, actor.UserID); err != nil {
		return nil, fmt.Errorf("start the running window: %w", err)
	}

	milestones := milestone.NewService(cluster)
	for _, target := range []struct {
		name, description string
		due               *time.Time
		summaries         []string
	}{
		{"Line 3 back under four hours of unplanned stops", "The month the downtime target holds.", day(21), []string{"Replace the worn conveyor belt between stations 4 and 5", "Welding cell 2 stops intermittently", "Firmware update on the filling machine"}},
		{"Model B start of production", "First hundred housings through final inspection.", day(42), []string{"Update the work instructions for stations 1 to 6", "Qualify the model B paint recipe", "Train shift B on the new HMI screens", "Scrap rate on the paint line above target"}},
	} {
		made, _, err := milestones.Create(orgCtx, created.Key, milestone.Input{Name: target.name, Description: target.description, DueOn: target.due}, actor.UserID)
		if err != nil {
			return nil, fmt.Errorf("create milestone %s: %w", target.name, err)
		}
		for _, summary := range target.summaries {
			if _, _, err := issues.SetMilestone(orgCtx, keys[summary], &made.ID, actor); err != nil {
				return nil, err
			}
		}
	}

	// One dependency the plan can point at: the paint recipe waits on the scrap.
	if _, _, err := issues.AddLink(orgCtx, keys["Scrap rate on the paint line above target"], issue.LinkInput{TypeName: issue.LinkTypeBlocks, TargetKey: keys["Qualify the model B paint recipe"]}, actor); err != nil {
		return nil, fmt.Errorf("link the demo dependency: %w", err)
	}
	if _, _, err := issues.AddComment(orgCtx, keys["Welding cell 2 stops intermittently"], issue.TextDocument("Swapped the door switch on cell 2 at 06:40. No stop since; watching it through the shift."), actor); err != nil {
		return nil, fmt.Errorf("add the demo comment: %w", err)
	}
	if err := plant(orgCtx, cluster, templates, issues, actor, plantInput{
		line: created.Key, types: types, keys: keys, shift: shift.ID, running: running.ID, next: next.ID, day: day, pts: pts,
	}); err != nil {
		return nil, err
	}
	log.Info("filled a demo factory", "project", created.Key, "issues", len(work))
	return created, nil
}

func advance(ctx context.Context, issues *issue.Service, key, name string, actor issue.Actor) error {
	available, err := issues.Transitions(ctx, key, actor)
	if err != nil {
		return fmt.Errorf("list transitions for %s: %w", key, err)
	}
	for _, t := range available {
		if t.Name == name {
			_, _, err := issues.Transition(ctx, key, issue.TransitionInput{TransitionID: t.ID}, actor)
			if err != nil {
				return fmt.Errorf("move %s through %q: %w", key, name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("transition %q is not available on %s", name, key)
}

func issueTypes(ctx context.Context, cluster *db.Cluster) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id, name FROM issue_type`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id   uuid.UUID
				name string
			)
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			out[name] = id
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the issue types: %w", err)
	}
	return out, nil
}

// paragraphs turns text with line breaks into the document a description is.
func paragraphs(text string) json.RawMessage {
	var content []map[string]any
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		content = append(content, map[string]any{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": line}}})
	}
	doc, _ := json.Marshal(map[string]any{"type": "doc", "content": content})
	return doc
}
