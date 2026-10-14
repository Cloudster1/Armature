package desk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// measured is what decides an issue's clocks: where it is, how urgent, and
// whether its status is finished.
type measured struct {
	id, projectID, statusID uuid.UUID
	priority                issue.Priority
	done                    bool
}

func measuredOf(i *issue.Issue) measured {
	return measured{id: i.ID, projectID: i.ProjectID, statusID: i.Status.ID, priority: i.Priority, done: i.Status.Category == workflow.CategoryDone}
}

// IssueUpdated keeps the clocks with the issue: a new priority or request type
// moves their goals, a move trades the old desk's clocks for the new desk's.
func (s *Service) IssueUpdated(ctx context.Context, tx db.DBTX, before, after *issue.Issue, actor issue.Actor) error {
	if before.ProjectID != after.ProjectID {
		return s.moved(ctx, tx, after)
	}
	if before.Priority == after.Priority && sameRequestType(before.RequestTypeID, after.RequestTypeID) {
		return nil
	}
	return s.follow(ctx, tx, measuredOf(after))
}

// moved takes the clocks off an issue and its subtasks, which are the old
// desk's promise, and gives them the new project's, if it is a desk.
func (s *Service) moved(ctx context.Context, tx db.DBTX, after *issue.Issue) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM sla_timer
		WHERE issue_id = $1 OR issue_id IN (SELECT id FROM issue WHERE parent_id = $1)`, after.ID); err != nil {
		return fmt.Errorf("stop the clocks of a moved issue: %w", err)
	}
	subjects := []measured{measuredOf(after)}
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.project_id, c.status_id, c.priority, st.category = 'done'
		FROM issue c JOIN issue_status st ON st.id = c.status_id
		WHERE c.parent_id = $1`, after.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m measured
		if err := rows.Scan(&m.id, &m.projectID, &m.statusID, &m.priority, &m.done); err != nil {
			rows.Close()
			return err
		}
		subjects = append(subjects, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range subjects {
		if err := s.follow(ctx, tx, m); err != nil {
			return err
		}
	}
	return nil
}

// deskPolicy is a policy as the clocks need it.
type deskPolicy struct {
	id     uuid.UUID
	metric Metric
	goals  map[issue.Priority]int
	pause  map[uuid.UUID]bool
}

// policiesOf reads the policies of a project, none unless it is a desk.
func policiesOf(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]deskPolicy, error) {
	rows, err := tx.Query(ctx, `
		SELECT sp.id, sp.metric, sp.goals, sp.pause_status_ids FROM sla_policy sp
		JOIN project p ON p.id = sp.project_id
		WHERE p.id = $1 AND p.kind = 'service'`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deskPolicy
	for rows.Next() {
		var (
			p     deskPolicy
			raw   []byte
			pause []uuid.UUID
		)
		if err := rows.Scan(&p.id, &p.metric, &raw, &pause); err != nil {
			return nil, err
		}
		p.goals = map[issue.Priority]int{}
		if err := json.Unmarshal(raw, &p.goals); err != nil {
			return nil, fmt.Errorf("read policy goals: %w", err)
		}
		p.pause = map[uuid.UUID]bool{}
		for _, id := range pause {
			p.pause[id] = true
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// follow makes an issue's clocks what its desk's policies now say. An open
// clock keeps the time it has run, since the customer's wait was real.
func (s *Service) follow(ctx context.Context, tx db.DBTX, m measured) error {
	policies, err := policiesOf(ctx, tx, m.projectID)
	if err != nil || len(policies) == 0 {
		return err
	}
	rows, err := tx.Query(ctx, selectTimer+` WHERE t.issue_id = $1`, m.id)
	if err != nil {
		return err
	}
	timers, err := s.scanTimers(rows)
	if err != nil {
		return err
	}
	byPolicy := map[uuid.UUID]Timer{}
	for _, t := range timers {
		byPolicy[t.PolicyID] = t
	}

	for _, p := range policies {
		goal := p.goals[m.priority]
		t, has := byPolicy[p.id]
		switch {
		case has && t.CompletedAt != nil:
			// A finished clock keeps the goal it finished against.
			continue
		case has && goal <= 0:
			if _, err := tx.Exec(ctx, `DELETE FROM sla_timer WHERE id = $1`, t.ID); err != nil {
				return fmt.Errorf("take off a clock: %w", err)
			}
		case has && goal != t.GoalMinutes:
			if _, err := tx.Exec(ctx, `UPDATE sla_timer SET goal_minutes = $2 WHERE id = $1`, t.ID, goal); err != nil {
				return fmt.Errorf("move a clock's goal: %w", err)
			}
			// A shorter goal the clock has already used up is a breach now,
			// told as any other; a breach already recorded stays recorded.
			if _, err := s.breach(ctx, tx, t.ID); err != nil {
				return err
			}
		case !has && goal > 0 && !m.done:
			// A done issue owes nobody an answer; a pause status starts paused.
			if _, err := tx.Exec(ctx, `
				INSERT INTO sla_timer (issue_id, policy_id, goal_minutes, started_at, running_since)
				VALUES ($1, $2, $3, now(), CASE WHEN $4 THEN NULL ELSE now() END)
				ON CONFLICT (issue_id, policy_id) DO NOTHING`, m.id, p.id, goal, p.pause[m.statusID]); err != nil {
				return fmt.Errorf("start timer: %w", err)
			}
		}
	}
	return nil
}

func sameRequestType(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
