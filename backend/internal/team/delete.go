package team

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
)

// carrying is what a team still holds when somebody asks to delete it.
type carrying struct {
	Issues       int
	Boards       int
	Sprints      int
	Running      int
	Finished     int
	RequestTypes int
}

// Delete removes a team carrying nothing. Nulling the columns would make its work
// nobody's and its sprints the project's, so somebody decides where each goes.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1 FOR UPDATE OF t`, id))
		if err != nil {
			return err
		}
		holds := carrying{Issues: found.Issues, Boards: found.Boards}
		err = tx.QueryRow(ctx, `
			SELECT count(*),
			       count(*) FILTER (WHERE state = 'active'),
			       count(*) FILTER (WHERE state = 'closed'),
			       (SELECT count(*) FROM request_type WHERE team_id = $1)
			FROM sprint WHERE team_id = $1`, id).
			Scan(&holds.Sprints, &holds.Running, &holds.Finished, &holds.RequestTypes)
		if err != nil {
			return fmt.Errorf("count what the team holds: %w", err)
		}
		if reason := refusal(found.Name, holds); reason != "" {
			return fmt.Errorf("%w: %s", ErrInUse, reason)
		}

		_, err = tx.Exec(ctx, `DELETE FROM team WHERE id = $1`, id)
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: %s still has sprints or request types of its own. Reload the team and try again.", ErrInUse, found.Name)
		}
		if err != nil {
			return fmt.Errorf("delete team: %w", err)
		}
		return nil
	})
}

// refusal says, in sentences, why a team holding something cannot go and what
// to do about each thing; empty when it holds nothing.
func refusal(name string, holds carrying) string {
	// A sprint that has run never leaves its team, so moving the rest would
	// not help and is not asked for.
	if holds.Running+holds.Finished > 0 {
		has := fmt.Sprintf("%s still has %s", name, count(holds.Sprints, "sprint"))
		switch {
		case holds.Running > 0 && holds.Sprints == 1:
			has += ", and it is running"
		case holds.Running > 0:
			has += ", one of them running"
		}
		return has + ". A team keeps the sprints it has run as the record of what it delivered, so it cannot be deleted; rename it instead."
	}

	var reasons []string
	if holds.Issues > 0 {
		reasons = append(reasons, fmt.Sprintf("%s still has %s. Hand %s to another team or back to the project first.",
			name, count(holds.Issues, "issue"), them(holds.Issues)))
	}
	if holds.Boards > 0 {
		reasons = append(reasons, fmt.Sprintf("%s still has %s. Delete %s first.",
			name, count(holds.Boards, "board"), them(holds.Boards)))
	}
	if holds.Sprints > 0 {
		reasons = append(reasons, fmt.Sprintf("%s still has %s that %s not started. Delete %s first.",
			name, count(holds.Sprints, "sprint"), haveOrHas(holds.Sprints), them(holds.Sprints)))
	}
	if holds.RequestTypes > 0 {
		reasons = append(reasons, fmt.Sprintf("%s still has %s routed to it. Route %s to another team or to the project first.",
			name, count(holds.RequestTypes, "request type"), them(holds.RequestTypes)))
	}
	return strings.Join(reasons, " ")
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func them(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func haveOrHas(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
