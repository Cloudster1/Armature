// Package template describes the shapes a new project can be given.
//
// A template decides three things at once: what kind of project this is, what
// its first board is, and which workflows its issues follow. Those are the
// choices somebody making a project has to get right before anything else is
// possible, and they are the ones a template answers so the project is usable
// the moment it exists.
//
// Templates are code, not rows. What there is to choose between changes when
// the product does, and a template that could be edited would need every
// project made from it to say which version it meant.
package template

import (
	"errors"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Template is one way to set a project up.
type Template struct {
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Kind        project.Kind `json:"kind"`
	BoardType   board.Type   `json:"boardType"`
	// WorkflowName is the workflow the template brings with it, or empty when
	// projects made from it follow whatever the organization has decided.
	WorkflowName string `json:"workflowName,omitempty"`
	// Desk says the project is a service desk, set up with request types and
	// response goals.
	Desk bool `json:"desk"`
	// Features are the pages a project made from the template starts with.
	Features []project.Feature `json:"features"`

	workflow *workflowSpec
}

// Default is the template a project gets when nobody chose one: the one that
// shows work without anybody having to plan a sprint first.
const Default = "kanban"

// ErrUnknown is returned for a template key that names nothing.
var ErrUnknown = errors.New("no such project template")

// workflowSpec is a workflow described by status name, so a template can be
// written down once and applied in any organization that still has the
// statuses it names.
type workflowSpec struct {
	name, description string
	schemeName        string
	// statuses are made when the organization lacks them, with the category
	// that gives them their meaning on boards and in reports.
	statuses    []statusSpec
	steps       []stepSpec
	transitions []transitionSpec
}

type statusSpec struct {
	name        string
	category    workflow.StatusCategory
	description string
}

type stepSpec struct {
	status  string
	initial bool
}

type transitionSpec struct {
	name string
	// from is empty for a transition available from every status.
	from, to string
	rules    []workflow.RuleInput
}

// simpleTasks is three states and no review step, for work that is done when
// the person doing it says so.
var simpleTasks = &workflowSpec{
	name:        "Simple task workflow",
	description: "To Do, In Progress and Done. Nothing waits on review.",
	schemeName:  "Task tracking workflows",
	steps: []stepSpec{
		{bootstrap.StatusToDo, true},
		{bootstrap.StatusInProgress, false},
		{bootstrap.StatusDone, false},
	},
	transitions: []transitionSpec{
		{name: "Start progress", from: bootstrap.StatusToDo, to: bootstrap.StatusInProgress,
			rules: []workflow.RuleInput{{Kind: workflow.KindPostFunction, Type: workflow.PostAssignToActor}}},
		{name: "Stop progress", from: bootstrap.StatusInProgress, to: bootstrap.StatusToDo},
		{name: "Finish", from: bootstrap.StatusInProgress, to: bootstrap.StatusDone,
			rules: []workflow.RuleInput{{Kind: workflow.KindPostFunction, Type: workflow.PostSetResolved}}},
		{name: "Close", to: bootstrap.StatusDone,
			rules: []workflow.RuleInput{{Kind: workflow.KindPostFunction, Type: workflow.PostSetResolved}}},
		{name: "Reopen", from: bootstrap.StatusDone, to: bootstrap.StatusToDo,
			rules: []workflow.RuleInput{{Kind: workflow.KindPostFunction, Type: workflow.PostClearResolved}}},
	},
}

// The service desk's statuses, named for the customer's side of the wait.
const (
	statusWaitingForSupport  = "Waiting for support"
	statusWaitingForCustomer = "Waiting for customer"
	statusResolved           = "Resolved"
)

// serviceRequests is a workflow with a pause in it: while the desk waits on
// the customer the clocks stop, and the customer's reply brings it back.
var serviceRequests = &workflowSpec{
	name:        "Service request workflow",
	description: "Waiting for support, in progress, waiting for the customer, resolved.",
	schemeName:  "Service desk workflows",
	statuses: []statusSpec{
		{statusWaitingForSupport, workflow.CategoryTodo, "Nobody has picked this up yet."},
		{statusWaitingForCustomer, workflow.CategoryInProgress, "The desk is waiting on the customer."},
		{statusResolved, workflow.CategoryDone, "Answered or fixed."},
	},
	steps: []stepSpec{
		{statusWaitingForSupport, true},
		{bootstrap.StatusInProgress, false},
		{statusWaitingForCustomer, false},
		{statusResolved, false},
	},
	transitions: []transitionSpec{
		{name: "Start work", from: statusWaitingForSupport, to: bootstrap.StatusInProgress,
			rules: []workflow.RuleInput{{Kind: workflow.KindPostFunction, Type: workflow.PostAssignToActor}}},
		{name: "Wait for customer", from: bootstrap.StatusInProgress, to: statusWaitingForCustomer},
		{name: "Customer responded", from: statusWaitingForCustomer, to: bootstrap.StatusInProgress},
		{name: "Resolve", to: statusResolved,
			rules: []workflow.RuleInput{{Kind: workflow.KindPostFunction, Type: workflow.PostSetResolved}}},
		{name: "Reopen", from: statusResolved, to: statusWaitingForSupport,
			rules: []workflow.RuleInput{{Kind: workflow.KindPostFunction, Type: workflow.PostClearResolved}}},
	},
}

// softwareFeatures is everything but the desk's pages. A kanban team may
// still run a sprint, so Kanban and Scrum differ in their board, not here.
var softwareFeatures = project.DefaultFeatures(project.KindSoftware)

// all is every template, in the order the chooser offers them. The first is
// the default.
var all = []Template{
	{
		Key:  Default,
		Name: "Kanban",
		Description: "Work flows continuously. The board shows everything in flight, " +
			"and a swimlane can carry a limit that says when too much is.",
		Kind:      project.KindSoftware,
		BoardType: board.TypeKanban,
		Features:  softwareFeatures,
	},
	{
		Key:  "scrum",
		Name: "Scrum",
		Description: "Work is planned into sprints with a capacity. The board shows the " +
			"sprint that is running; the next one is planned from the backlog.",
		Kind:      project.KindSoftware,
		BoardType: board.TypeScrum,
		Features:  softwareFeatures,
	},
	{
		Key:  "task-tracking",
		Name: "Task tracking",
		Description: "For work that is not software. Three states with no review step, " +
			"and a kanban board over them.",
		Kind:         project.KindBusiness,
		BoardType:    board.TypeKanban,
		WorkflowName: simpleTasks.name,
		workflow:     simpleTasks,
		Features: []project.Feature{
			project.FeatureBoard, project.FeaturePlan, project.FeatureCalendar, project.FeatureMilestones,
			project.FeatureDashboard, project.FeatureHierarchy, project.FeatureTeams, project.FeatureAutomation,
			project.FeatureImport,
		},
	},
	{
		Key:  "service-desk",
		Name: "Service desk",
		Description: "Customers raise requests through a portal; agents answer them against " +
			"response and resolution goals, with notes the customer does not see.",
		Kind:         project.KindService,
		BoardType:    board.TypeKanban,
		WorkflowName: serviceRequests.name,
		workflow:     serviceRequests,
		Desk:         true,
		Features: []project.Feature{
			project.FeatureBoard, project.FeatureCalendar, project.FeatureDashboard, project.FeatureQueues,
			project.FeatureDesk, project.FeatureTeams, project.FeatureAutomation, project.FeatureImport,
		},
	},
}

// All returns the templates on offer, in the order to show them.
func All() []Template {
	out := make([]Template, len(all))
	copy(out, all)
	return out
}

// Find looks a template up by key. An empty key is the default.
func Find(key string) (Template, bool) {
	if key == "" {
		key = Default
	}
	for _, t := range all {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}
