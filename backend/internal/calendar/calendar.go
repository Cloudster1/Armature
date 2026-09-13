// Package calendar lays a project's dated things over a month: issues by their
// start and due dates, sprints, milestones and versions. It stores nothing; a
// day is read from the same columns the plan and the releases page read.
package calendar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
)

// MaxItems bounds a month; a project with more dated issues than this in one
// month is asked to narrow, not shown a wall.
const MaxItems = 500

// Kind says what an item is, which decides how it is drawn.
type Kind string

const (
	KindIssue     Kind = "issue"
	KindSprint    Kind = "sprint"
	KindMilestone Kind = "milestone"
	KindVersion   Kind = "version"
)

// Item is one thing with a date or a range of dates.
type Item struct {
	Kind  Kind      `json:"kind"`
	ID    uuid.UUID `json:"id"`
	Key   string    `json:"key,omitempty"`
	Title string    `json:"title"`
	// From and To are dates as YYYY-MM-DD; a thing with one date has both the same.
	From string `json:"from"`
	To   string `json:"to"`
	// Category is the status category of an issue, so a done one draws quieter.
	Category string `json:"category,omitempty"`
	// Done says a sprint completed, a milestone closed or a version released.
	Done bool `json:"done,omitempty"`
}

// Month is what the calendar page draws.
type Month struct {
	Year  int    `json:"year"`
	Month int    `json:"month"`
	Items []Item `json:"items"`
	// Truncated says MaxItems was reached and some issues are not shown.
	Truncated bool `json:"truncated"`
}

// ErrBadMonth is a month outside 1..12 or a year outside sense.
var ErrBadMonth = errors.New("a month is written as YYYY-MM, such as 2026-09")

// Service reads months.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// ParseMonth reads YYYY-MM.
func ParseMonth(s string) (int, int, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return 0, 0, ErrBadMonth
	}
	return t.Year(), int(t.Month()), nil
}

const dateLayout = "2006-01-02"

// Month reads everything dated inside the month, including ranges that cross
// into it from either side, so a bar that started last month is still drawn.
func (s *Service) Month(ctx context.Context, projectKey string, year, month int) (*Month, error) {
	if month < 1 || month > 12 || year < 1970 || year > 9999 {
		return nil, ErrBadMonth
	}
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)
	out := &Month{Year: year, Month: month, Items: []Item{}}
	key := project.NormalizeKey(projectKey)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, key).Scan(&pid); err != nil {
			return project.ErrNotFound
		}
		rows, err := tx.Query(ctx, `
			SELECT i.id, p.key || '-' || i.key_num, i.summary, st.category,
			       COALESCE(i.start_date, i.due_date), COALESCE(i.due_date, i.start_date)
			FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN issue_status st ON st.id = i.status_id
			WHERE i.project_id = $1
			  AND COALESCE(i.start_date, i.due_date) < $3
			  AND COALESCE(i.due_date, i.start_date) >= $2
			ORDER BY COALESCE(i.start_date, i.due_date), i.key_num
			LIMIT $4`, pid, first, next, MaxItems+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				it       Item
				from, to time.Time
			)
			if err := rows.Scan(&it.ID, &it.Key, &it.Title, &it.Category, &from, &to); err != nil {
				return err
			}
			if len(out.Items) >= MaxItems {
				out.Truncated = true
				break
			}
			it.Kind, it.From, it.To = KindIssue, from.Format(dateLayout), to.Format(dateLayout)
			out.Items = append(out.Items, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		more, err := tx.Query(ctx, `
			SELECT 'sprint', id, name, starts_on, ends_on, state = 'closed' FROM sprint
			WHERE project_id = $1 AND starts_on IS NOT NULL AND ends_on IS NOT NULL AND starts_on < $3 AND ends_on >= $2
			UNION ALL
			SELECT 'milestone', id, name, due_on, due_on, closed_at IS NOT NULL FROM milestone
			WHERE project_id = $1 AND due_on IS NOT NULL AND due_on >= $2 AND due_on < $3
			UNION ALL
			SELECT 'version', id, name, COALESCE(start_on, release_on), COALESCE(release_on, start_on), released_at IS NOT NULL FROM version
			WHERE project_id = $1 AND archived_at IS NULL AND COALESCE(start_on, release_on) IS NOT NULL
			  AND COALESCE(start_on, release_on) < $3 AND COALESCE(release_on, start_on) >= $2
			ORDER BY 4, 3`, pid, first, next)
		if err != nil {
			return err
		}
		defer more.Close()
		for more.Next() {
			var (
				kind     string
				it       Item
				from, to time.Time
			)
			if err := more.Scan(&kind, &it.ID, &it.Title, &from, &to, &it.Done); err != nil {
				return err
			}
			it.Kind, it.From, it.To = Kind(kind), from.Format(dateLayout), to.Format(dateLayout)
			out.Items = append(out.Items, it)
		}
		return more.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read calendar month: %w", err)
	}
	return out, nil
}
