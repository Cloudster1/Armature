package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/workflow"
)

// MaintenanceKey is the second demo project: the crew that keeps the whole
// plant running, on a workflow of its own.
const MaintenanceKey = "MAINT"

// plantInput is what the rest of the plant builds on: the line, its issues by
// summary, the day shift and its windows.
type plantInput struct {
	line          string
	types         map[string]uuid.UUID
	keys          map[string]string
	shift         uuid.UUID
	running, next uuid.UUID
	day           func(int) *time.Time
	pts           func(float64) *float64
}

// plant adds what a single line leaves out of a demo: a second shift with a
// board of its own, a maintenance project whose work waits for parts, and
// issues spread across both so every page has something to show.
func plant(ctx context.Context, cluster *db.Cluster, templates *template.Service, issues *issue.Service, actor issue.Actor, in plantInput) error {
	labels := label.NewService(cluster)
	teams := team.NewService(cluster)
	boards := board.NewService(cluster, issues)

	// The night shift, with the line's boards beside it: one per shift and
	// one over everything, kanban, so both board types are on show.
	shiftB, _, err := teams.Create(ctx, in.line, team.CreateInput{Name: "Shift B", Description: "Runs line 3 by night and hands over at six."}, actor.UserID)
	if err != nil {
		return fmt.Errorf("form shift B: %w", err)
	}
	if _, _, err := teams.AddMember(ctx, shiftB.ID, actor.UserID, false, actor.UserID); err != nil {
		return fmt.Errorf("put the owner on shift B: %w", err)
	}
	kanban := board.TypeKanban
	for _, b := range []board.CreateInput{
		{Name: "Shift A board", Description: "The day shift's window, and nothing else.", TeamID: &in.shift},
		{Name: "Shift B board", Description: "The night shift's window, and nothing else.", TeamID: &shiftB.ID},
		{Name: "Whole line", Description: "Every card on line 3, whatever window it is in.", Type: kanban},
	} {
		if _, _, err := boards.CreateBoard(ctx, in.line, b, actor.UserID); err != nil {
			return fmt.Errorf("add the %s: %w", b.Name, err)
		}
	}

	more := []plantIssue{
		{"Night shift handover checklist for line 3", "As a shift leader, I want a checklist at handover so that nothing found at night waits until the morning meeting.", bootstrap.TypeStory, issue.PriorityMedium, in.day(-4), in.day(3), in.pts(3), []string{"changeover"}, &shiftB.ID, &in.running, []string{"Start progress"}},
		{"Replace the vision camera lens on station 2", "The lens is scratched and the camera rejects good housings. The spare is in the cabinet.", bootstrap.TypeTask, issue.PriorityHigh, in.day(-2), in.day(1), in.pts(2), []string{"maintenance", "quality"}, &shiftB.ID, &in.running, []string{"Start progress", "Ready for review"}},
		{"Label printer at station 6 jams on every tenth label", "Only on the night shift, only with the new label stock.", bootstrap.TypeBug, issue.PriorityMedium, in.day(8), in.day(12), in.pts(3), []string{"downtime"}, &shiftB.ID, &in.next, nil},
		{"Stock the line side racks for model B", "", bootstrap.TypeTask, issue.PriorityLow, in.day(9), in.day(16), in.pts(2), []string{"changeover"}, &shiftB.ID, &in.next, nil},
		{"Update the andon board thresholds", "Five minutes to yellow, ten to red, as agreed with the shift leaders.", bootstrap.TypeTask, issue.PriorityLow, in.day(-10), in.day(-9), in.pts(1), []string{"downtime"}, nil, nil, []string{"Close"}},
		{"Torque audit on the fastening station", "Twenty joints a shift for a week, logged against the certificate.", bootstrap.TypeTask, issue.PriorityMedium, in.day(-1), in.day(6), in.pts(3), []string{"quality"}, &shiftB.ID, &in.running, nil},
	}
	if err := file(ctx, issues, labels, in.line, in.types, in.keys, actor, more); err != nil {
		return err
	}

	// Maintenance work waits for parts, which the software workflow has no
	// word for, so the project gets a workflow and a scheme of its own.
	admin := workflow.NewAdmin(cluster, workflow.NewStore())
	statuses := map[string]uuid.UUID{}
	if err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		all, err := workflow.NewStore().ListStatuses(ctx, tx)
		for _, s := range all {
			statuses[s.Name] = s.ID
		}
		return err
	}); err != nil {
		return fmt.Errorf("read the statuses: %w", err)
	}
	waiting, _, err := admin.CreateStatus(ctx, workflow.StatusInput{Name: "Waiting for parts", Category: workflow.CategoryInProgress, Description: "Started, and stopped until a part arrives."}, actor.UserID)
	if err != nil {
		return fmt.Errorf("add the waiting status: %w", err)
	}
	statuses[waiting.Name] = waiting.ID
	todo, doing, done := statuses[bootstrap.StatusToDo], statuses[bootstrap.StatusInProgress], statuses[bootstrap.StatusDone]
	flow, _, err := admin.CreateWorkflow(ctx, workflow.GraphInput{
		Name:        "Maintenance workflow",
		Description: "Work on the plant: started, waiting for a part, or finished.",
		Steps: []workflow.StepInput{
			{StatusID: todo, IsInitial: true, Layout: &workflow.Point{X: 80, Y: 160}},
			{StatusID: doing, Layout: &workflow.Point{X: 340, Y: 160}},
			{StatusID: waiting.ID, Layout: &workflow.Point{X: 340, Y: 340}},
			{StatusID: done, Layout: &workflow.Point{X: 600, Y: 160}},
		},
		Transitions: []workflow.TransitionInput{
			{Name: "Start work", FromStatusID: &todo, ToStatusID: doing},
			{Name: "Wait for parts", Description: "The part is on order; nothing moves until it lands.", FromStatusID: &doing, ToStatusID: waiting.ID},
			{Name: "Parts arrived", FromStatusID: &waiting.ID, ToStatusID: doing},
			{Name: "Close", Description: "Fixed, tested, signed off.", ToStatusID: done},
			{Name: "Reopen", FromStatusID: &done, ToStatusID: todo},
		},
	}, actor.UserID)
	if err != nil {
		return fmt.Errorf("draw the maintenance workflow: %w", err)
	}
	scheme, _, err := admin.CreateScheme(ctx, workflow.SchemeInput{Name: "Maintenance scheme", Items: []workflow.SchemeItemInput{{WorkflowID: flow.ID}}}, actor.UserID)
	if err != nil {
		return fmt.Errorf("make the maintenance scheme: %w", err)
	}
	maint, _, err := templates.Create(ctx, "task-tracking", project.CreateInput{
		Key:         MaintenanceKey,
		Name:        "Plant maintenance",
		Description: "Everything that keeps the plant running: presses, compressors, cranes, the paint supply, and the parts they wait for.",
		LeadID:      &actor.UserID,
		SchemeID:    scheme.ID,
	}, actor.UserID)
	if err != nil {
		return fmt.Errorf("create the maintenance project: %w", err)
	}
	crew, _, err := teams.Create(ctx, maint.Key, team.CreateInput{Name: "Maintenance crew", Description: "Four fitters and two electricians, on call around the clock."}, actor.UserID)
	if err != nil {
		return fmt.Errorf("form the maintenance crew: %w", err)
	}
	if _, _, err := teams.AddMember(ctx, crew.ID, actor.UserID, true, actor.UserID); err != nil {
		return fmt.Errorf("put the owner on the crew: %w", err)
	}
	if _, _, err := boards.CreateBoard(ctx, maint.Key, board.CreateInput{Name: "Crew board", Description: "What the crew is on, with the parts it waits for in a lane of their own.", TeamID: &crew.ID}, actor.UserID); err != nil {
		return fmt.Errorf("give the crew a board: %w", err)
	}

	plantWork := []plantIssue{
		{"Rebuild the hydraulic unit on press 2", "Pressure drops under load; the pump is worn. Seal kit and pump ordered.", bootstrap.TypeTask, issue.PriorityHigh, in.day(-6), in.day(5), in.pts(8), []string{"maintenance"}, &crew.ID, nil, []string{"Start work", "Wait for parts"}},
		{"Compressor 1 trips on high temperature", "Trips twice a shift when the hall is warm. The aftercooler is probably blocked.", bootstrap.TypeBug, issue.PriorityHighest, in.day(-2), in.day(1), in.pts(3), []string{"electrical", "downtime"}, &crew.ID, nil, []string{"Start work"}},
		{"Replace the light curtain on the packaging cell", "The old one no longer passes the muting test.", bootstrap.TypeTask, issue.PriorityMedium, in.day(1), in.day(4), in.pts(2), []string{"safety"}, &crew.ID, nil, []string{"Start work"}},
		{"Annual inspection of the overhead crane", "With the inspector, before the certificate runs out at the end of the month.", bootstrap.TypeTask, issue.PriorityMedium, in.day(12), in.day(12), in.pts(2), []string{"safety"}, nil, nil, nil},
		{"Leaking coolant line at machining centre 4", "", bootstrap.TypeBug, issue.PriorityHigh, in.day(-9), in.day(-8), in.pts(1), []string{"maintenance"}, &crew.ID, nil, []string{"Close"}},
		{"Order and fit new drive belts for conveyor 7", "Belts are cracking; ordered a set on Monday.", bootstrap.TypeTask, issue.PriorityLow, in.day(-3), in.day(10), in.pts(2), []string{"maintenance"}, &crew.ID, nil, []string{"Start work", "Wait for parts"}},
		{"Vibration on pump P-12 above alarm level", "Bearing on the drive end, by the sound of it.", bootstrap.TypeBug, issue.PriorityHigh, in.day(-1), in.day(3), in.pts(3), []string{"maintenance"}, &crew.ID, nil, []string{"Start work"}},
		{"Calibrate the flow meters in the paint supply", "Quality wants the calibration certificates before the model B recipe is signed off.", bootstrap.TypeTask, issue.PriorityMedium, in.day(5), in.day(9), in.pts(2), []string{"quality"}, nil, nil, nil},
		{"Fit guards on the new deburring station", "", bootstrap.TypeTask, issue.PriorityMedium, in.day(6), in.day(8), in.pts(3), []string{"safety"}, &crew.ID, nil, nil},
		{"Replace the worn gripper on robot R3", "The fingers the line ordered arrived; fit them in the next window.", bootstrap.TypeTask, issue.PriorityLow, in.day(-14), in.day(-12), in.pts(1), []string{"maintenance"}, &crew.ID, nil, []string{"Close"}},
		{"Roof leak over the raw material store", "Drips onto the pallets when it rains hard. Tarpaulin on for now.", bootstrap.TypeBug, issue.PriorityMedium, in.day(2), in.day(20), in.pts(5), []string{"maintenance"}, nil, nil, nil},
	}
	if err := file(ctx, issues, labels, maint.Key, in.types, in.keys, actor, plantWork); err != nil {
		return err
	}

	// The two projects meet: the fitted gripper is the line's spare.
	if _, _, err := issues.AddLink(ctx, in.keys["Replace the worn gripper on robot R3"], issue.LinkInput{TypeName: bootstrap.LinkRelates, TargetKey: in.keys["Order spare gripper fingers for robot R3"]}, actor); err != nil {
		return fmt.Errorf("link the gripper across projects: %w", err)
	}
	if _, _, err := issues.AddComment(ctx, in.keys["Rebuild the hydraulic unit on press 2"], issue.TextDocument("Pump is on back order until Thursday. Press 2 runs at reduced pressure until then; quality has been told."), actor); err != nil {
		return fmt.Errorf("add the maintenance comment: %w", err)
	}
	return nil
}

// plantIssue is one issue of the plant, and where it goes.
type plantIssue struct {
	summary, description, typeName string
	priority                       issue.Priority
	start, due                     *time.Time
	estimate                       *float64
	labels                         []string
	team, sprint                   *uuid.UUID
	advance                        []string
}

// file creates the issues in one project and moves each as far as it says.
func file(ctx context.Context, issues *issue.Service, labels *label.Service, projectKey string, types map[string]uuid.UUID, keys map[string]string, actor issue.Actor, work []plantIssue) error {
	for _, w := range work {
		in := issue.CreateInput{
			ProjectKey: projectKey, Summary: w.summary, TypeID: types[w.typeName],
			Priority: w.priority, StartDate: w.start, DueDate: w.due, Estimate: w.estimate, TeamID: w.team,
		}
		if w.description != "" {
			in.Description = paragraphs(w.description)
		}
		made, _, err := issues.Create(ctx, in, actor)
		if err != nil {
			return fmt.Errorf("create %q: %w", w.summary, err)
		}
		keys[w.summary] = made.Key
		if len(w.labels) > 0 {
			if _, _, err := labels.SetIssueLabels(ctx, made.Key, w.labels, actor); err != nil {
				return fmt.Errorf("label %s: %w", made.Key, err)
			}
		}
		if w.sprint != nil {
			if _, _, err := issues.SetSprint(ctx, made.Key, w.sprint, actor); err != nil {
				return fmt.Errorf("plan %s: %w", made.Key, err)
			}
		}
		for _, name := range w.advance {
			if err := advance(ctx, issues, made.Key, name, actor); err != nil {
				return err
			}
		}
	}
	return nil
}
