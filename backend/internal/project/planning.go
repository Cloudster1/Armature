package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
)

// PlanningMethod is how a project schedules its work: in sprints, or flowing.
type PlanningMethod string

const (
	MethodScrum  PlanningMethod = "scrum"
	MethodKanban PlanningMethod = "kanban"
)

// Valid reports whether the method is one the database knows.
func (m PlanningMethod) Valid() bool { return m == MethodScrum || m == MethodKanban }

// ResourceGrouping is what the resource view sets the work against: teams or people.
type ResourceGrouping string

const (
	GroupByTeam   ResourceGrouping = "team"
	GroupByPerson ResourceGrouping = "person"
)

// Valid reports whether the grouping is one the database knows.
func (g ResourceGrouping) Valid() bool { return g == GroupByTeam || g == GroupByPerson }

var (
	// ErrBadPlanning is returned for a planning method or grouping that is not one.
	ErrBadPlanning = errors.New("that is not a way to plan")
	// ErrScrumByTeam is returned for planning a scrum project by person: a sprint is a team's.
	ErrScrumByTeam = errors.New("a scrum project plans by team")
)

// nextPlanning decides the method and grouping a change leaves. Turning to scrum
// takes the grouping back to teams; asking for people on scrum is refused.
func nextPlanning(method PlanningMethod, grouping ResourceGrouping, wantMethod *string, wantGrouping *string) (PlanningMethod, ResourceGrouping, error) {
	if wantMethod != nil {
		method = PlanningMethod(*wantMethod)
		if !method.Valid() {
			return "", "", fmt.Errorf("%w: plan as scrum or kanban", ErrBadPlanning)
		}
	}
	if wantGrouping != nil {
		grouping = ResourceGrouping(*wantGrouping)
		if !grouping.Valid() {
			return "", "", fmt.Errorf("%w: plan by team or by person", ErrBadPlanning)
		}
	}
	if method == MethodScrum && grouping == GroupByPerson {
		if wantGrouping != nil {
			return "", "", ErrScrumByTeam
		}
		grouping = GroupByTeam
	}
	return method, grouping, nil
}

// setPlanning writes a project's method and grouping inside an update.
func setPlanning(ctx context.Context, tx db.DBTX, id uuid.UUID, wantMethod, wantGrouping *string) error {
	var method PlanningMethod
	var grouping ResourceGrouping
	if err := tx.QueryRow(ctx, `SELECT planning_method, resource_grouping FROM project WHERE id = $1`, id).Scan(&method, &grouping); err != nil {
		return err
	}
	method, grouping, err := nextPlanning(method, grouping, wantMethod, wantGrouping)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE project SET planning_method = $2, resource_grouping = $3 WHERE id = $1`, id, string(method), string(grouping))
	return err
}
