package availability

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// ForPeople reads each person's days from one day to another, both included:
// their week, their calendar's holidays and their absences, combined in one place.
func (s *Service) ForPeople(ctx context.Context, ids []uuid.UUID, from, to time.Time) (map[uuid.UUID]Person, error) {
	var out map[uuid.UUID]Person
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := readSchedules(ctx, tx, ids)
		if err != nil {
			return err
		}
		calendars, defaultID, err := readCalendarDays(ctx, tx, from, to)
		if err != nil {
			return err
		}
		absences, err := absencesOf(ctx, tx, ids, from, to)
		if err != nil {
			return err
		}
		away := map[uuid.UUID][]Absence{}
		for _, a := range absences {
			away[a.UserID] = append(away[a.UserID], a)
		}
		out = combine(ids, rows, calendars, defaultID, away, from, to)
		return nil
	})
	return out, err
}

func readSchedules(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (map[uuid.UUID]stored, error) {
	rows, err := tx.Query(ctx, `SELECT user_id, calendar_id, minutes FROM member_schedule WHERE user_id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("read working weeks: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]stored{}
	for rows.Next() {
		var (
			id  uuid.UUID
			row stored
			raw []byte
		)
		if err := rows.Scan(&id, &row.CalendarID, &raw); err != nil {
			return nil, err
		}
		var week map[string]int
		if err := json.Unmarshal(raw, &week); err != nil {
			return nil, fmt.Errorf("read working weeks: %w", err)
		}
		if row.Week, err = NormalizeWeek(week); err != nil {
			return nil, err
		}
		out[id] = row
	}
	return out, rows.Err()
}

// readCalendarDays holds every calendar of the organization, empty or not, so
// a person's calendar with no days in the stretch is still theirs.
func readCalendarDays(ctx context.Context, tx db.DBTX, from, to time.Time) (map[uuid.UUID]Days, *uuid.UUID, error) {
	out := map[uuid.UUID]Days{}
	var defaultID *uuid.UUID
	rows, err := tx.Query(ctx, `SELECT id, is_default FROM holiday_calendar`)
	if err != nil {
		return nil, nil, fmt.Errorf("read holiday calendars: %w", err)
	}
	for rows.Next() {
		var (
			id        uuid.UUID
			isDefault bool
		)
		if err := rows.Scan(&id, &isDefault); err != nil {
			rows.Close()
			return nil, nil, err
		}
		out[id] = Days{}
		if isDefault {
			defaultID = &id
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	days, err := readHolidays(ctx, tx, from, to, nil)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range days {
		out[d.Calendar.ID][d.Day] = d.Holiday
	}
	return out, defaultID, nil
}

// readHolidays reads the days off in a stretch, of the calendars named or,
// for nil, of every calendar.
func readHolidays(ctx context.Context, tx db.DBTX, from, to time.Time, calendars []uuid.UUID) ([]CalendarDay, error) {
	rows, err := tx.Query(ctx, `
		SELECT h.id, c.id, c.name, c.is_default, to_char(h.day, 'YYYY-MM-DD'), h.name, h.half_day
		FROM holiday h JOIN holiday_calendar c ON c.id = h.calendar_id
		WHERE h.day BETWEEN $1::date AND $2::date AND ($3::uuid[] IS NULL OR c.id = ANY($3::uuid[]))
		ORDER BY h.day, c.is_default DESC, lower(c.name)`, from, to, calendars)
	if err != nil {
		return nil, fmt.Errorf("read holidays: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CalendarDay, error) {
		var d CalendarDay
		err := row.Scan(&d.ID, &d.Calendar.ID, &d.Calendar.Name, &d.Default, &d.Day, &d.Name, &d.HalfDay)
		return d, err
	})
}

// DefaultHolidays is the default calendar's days off in a stretch, both ends
// included: the days the whole organization has off unless told otherwise.
func (s *Service) DefaultHolidays(ctx context.Context, from, to time.Time) ([]Holiday, error) {
	out := []Holiday{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		days, err := readHolidays(ctx, tx, from, to, nil)
		if err != nil {
			return err
		}
		for _, d := range days {
			if d.Default {
				out = append(out, d.Holiday)
			}
		}
		return nil
	})
	return out, err
}

// CalendarHolidays is the days off in a stretch of the default calendar and
// of every calendar these people keep.
func (s *Service) CalendarHolidays(ctx context.Context, ids []uuid.UUID, from, to time.Time) ([]CalendarDay, error) {
	var out []CalendarDay
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id FROM holiday_calendar WHERE is_default
			UNION
			SELECT calendar_id FROM member_schedule WHERE user_id = ANY($1::uuid[]) AND calendar_id IS NOT NULL`, ids)
		if err != nil {
			return fmt.Errorf("read the calendars people keep: %w", err)
		}
		calendars, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return fmt.Errorf("read the calendars people keep: %w", err)
		}
		if len(calendars) == 0 {
			return nil
		}
		out, err = readHolidays(ctx, tx, from, to, calendars)
		return err
	})
	return out, err
}

// AbsencesOf lists the absences of these people that touch a stretch.
func (s *Service) AbsencesOf(ctx context.Context, ids []uuid.UUID, from, to time.Time) ([]Absence, error) {
	var out []Absence
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = absencesOf(ctx, tx, ids, from, to)
		return err
	})
	return out, err
}

func absencesOf(ctx context.Context, tx db.DBTX, ids []uuid.UUID, from, to time.Time) ([]Absence, error) {
	rows, err := tx.Query(ctx, selectAbsence+`
		WHERE a.user_id = ANY($1::uuid[]) AND a.starts_on <= $3::date AND a.ends_on >= $2::date
		ORDER BY a.starts_on, lower(u.name), a.id`, ids, from, to)
	if err != nil {
		return nil, fmt.Errorf("list absences: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Absence, error) {
		a, err := scanAbsence(row)
		if err != nil {
			return Absence{}, err
		}
		return *a, nil
	})
}

// Members names those of ids who work in the organization now: somebody who has
// left, or a portal customer, keeps what is assigned to them but has no week here.
func (s *Service) Members(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name FROM app_user u
			JOIN org_member o ON o.user_id = u.id AND o.org_id = current_org_id() AND o.org_role <> 'customer'
			WHERE u.id = ANY($1)`, ids)
		if err != nil {
			return fmt.Errorf("read who works here: %w", err)
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
			out[id] = name
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ProjectPeople reads who works on a project in a stretch: the members of its
// teams, and the assignees of its issues scheduled in it.
func (s *Service) ProjectPeople(ctx context.Context, projectKey string, from, to time.Time) (*ProjectPeople, error) {
	out := &ProjectPeople{Names: map[uuid.UUID]string{}, Teams: map[uuid.UUID][]uuid.UUID{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT t.id, m.user_id, u.name
			FROM team t
			JOIN project p ON p.id = t.project_id
			JOIN team_member m ON m.team_id = t.id
			JOIN app_user u ON u.id = m.user_id
			JOIN org_member o ON o.user_id = m.user_id AND o.org_id = current_org_id() AND o.org_role <> 'customer'
			WHERE p.key = $1
			ORDER BY t.position, lower(u.name)`, projectKey)
		if err != nil {
			return fmt.Errorf("read the project's teams: %w", err)
		}
		for rows.Next() {
			var (
				teamID, userID uuid.UUID
				name           string
			)
			if err := rows.Scan(&teamID, &userID, &name); err != nil {
				rows.Close()
				return err
			}
			out.Teams[teamID] = append(out.Teams[teamID], userID)
			out.Names[userID] = name
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `
			SELECT DISTINCT i.assignee_id, u.name
			FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN app_user u ON u.id = i.assignee_id
			JOIN org_member o ON o.user_id = i.assignee_id AND o.org_id = current_org_id() AND o.org_role <> 'customer'
			WHERE p.key = $1
			  AND COALESCE(i.start_date, i.due_date) <= $3::date
			  AND COALESCE(i.due_date, i.start_date) >= $2::date`, projectKey, from, to)
		if err != nil {
			return fmt.Errorf("read the project's assignees: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				userID uuid.UUID
				name   string
			)
			if err := rows.Scan(&userID, &name); err != nil {
				return err
			}
			out.Names[userID] = name
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
