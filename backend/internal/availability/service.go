package availability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// Service owns the holiday calendars and the working weeks.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectCalendar = `
SELECT c.id, c.name, c.is_default,
       (SELECT count(*) FROM holiday h WHERE h.calendar_id = c.id),
       (SELECT count(*) FROM member_schedule s WHERE s.calendar_id = c.id),
       c.created_at, c.updated_at
FROM holiday_calendar c`

func scanCalendar(row pgx.Row) (*HolidayCalendar, error) {
	var c HolidayCalendar
	err := row.Scan(&c.ID, &c.Name, &c.Default, &c.DayCount, &c.PeopleCount, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListCalendars returns the organization's calendars, the default first.
func (s *Service) ListCalendars(ctx context.Context) ([]HolidayCalendar, error) {
	out := []HolidayCalendar{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectCalendar+` ORDER BY c.is_default DESC, lower(c.name)`)
		if err != nil {
			return fmt.Errorf("list holiday calendars: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCalendar(rows)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return rows.Err()
	})
	return out, err
}

// Calendar reads one calendar with its days.
func (s *Service) Calendar(ctx context.Context, id uuid.UUID) (*HolidayCalendar, error) {
	var out *HolidayCalendar
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = reload(ctx, tx, id)
		return err
	})
	return out, err
}

func reload(ctx context.Context, tx db.DBTX, id uuid.UUID) (*HolidayCalendar, error) {
	c, err := scanCalendar(tx.QueryRow(ctx, selectCalendar+` WHERE c.id = $1`, id))
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), name, half_day FROM holiday
		WHERE calendar_id = $1 ORDER BY day`, id)
	if err != nil {
		return nil, fmt.Errorf("read holidays: %w", err)
	}
	defer rows.Close()
	c.Days = []Holiday{}
	for rows.Next() {
		var h Holiday
		if err := rows.Scan(&h.Day, &h.Name, &h.HalfDay); err != nil {
			return nil, err
		}
		c.Days = append(c.Days, h)
	}
	return c, rows.Err()
}

// CalendarInput is a new calendar, or what an edit changes. A nil name is
// left alone; Default true makes the calendar everybody's fallback.
type CalendarInput struct {
	Name    *string
	Default *bool
}

// CreateCalendar adds a calendar. An organization with no default yet gets
// this one as its default, so somebody always has a calendar.
func (s *Service) CreateCalendar(ctx context.Context, in CalendarInput, actor uuid.UUID) (*HolidayCalendar, db.LSN, error) {
	if in.Name == nil {
		return nil, 0, invalid("a calendar needs a name")
	}
	name, err := NormalizeName(*in.Name, "calendar")
	if err != nil {
		return nil, 0, err
	}
	var out *HolidayCalendar
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		makeDefault := in.Default != nil && *in.Default
		if !makeDefault {
			if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM holiday_calendar WHERE is_default)`).Scan(&makeDefault); err != nil {
				return err
			}
		}
		if makeDefault {
			if _, err := tx.Exec(ctx, `UPDATE holiday_calendar SET is_default = false WHERE is_default`); err != nil {
				return fmt.Errorf("hand over the default: %w", err)
			}
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO holiday_calendar (org_id, name, is_default)
			VALUES (current_org_id(), $1, $2) RETURNING id`, name, makeDefault).Scan(&id)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create holiday calendar: %w", err)
		}
		if out, err = reload(ctx, tx, id); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "holiday_calendar.created", map[string]any{
			"id": id, "name": name, "default": makeDefault, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateCalendar renames a calendar or makes it the default. Unmaking the
// default is refused: another calendar is made the default instead.
func (s *Service) UpdateCalendar(ctx context.Context, id uuid.UUID, in CalendarInput, actor uuid.UUID) (*HolidayCalendar, db.LSN, error) {
	var name string
	if in.Name != nil {
		var err error
		if name, err = NormalizeName(*in.Name, "calendar"); err != nil {
			return nil, 0, err
		}
	}
	var out *HolidayCalendar
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanCalendar(tx.QueryRow(ctx, selectCalendar+` WHERE c.id = $1 FOR UPDATE OF c`, id))
		if err != nil {
			return err
		}
		if in.Name != nil && name != before.Name {
			_, err := tx.Exec(ctx, `UPDATE holiday_calendar SET name = $2 WHERE id = $1`, id, name)
			if isUniqueViolation(err) {
				return ErrNameTaken
			}
			if err != nil {
				return fmt.Errorf("rename holiday calendar: %w", err)
			}
		}
		if in.Default != nil && *in.Default != before.Default {
			if !*in.Default {
				return ErrDefaultCalendar
			}
			if _, err := tx.Exec(ctx, `UPDATE holiday_calendar SET is_default = false WHERE is_default`); err != nil {
				return fmt.Errorf("hand over the default: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE holiday_calendar SET is_default = true WHERE id = $1`, id); err != nil {
				return fmt.Errorf("make the default: %w", err)
			}
		}
		if out, err = reload(ctx, tx, id); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "holiday_calendar.updated", map[string]any{
			"id": id, "name": out.Name, "default": out.Default, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteCalendar removes a calendar that is not the default. The people
// given it fall back to the default, which is what null means for them.
func (s *Service) DeleteCalendar(ctx context.Context, id uuid.UUID, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanCalendar(tx.QueryRow(ctx, selectCalendar+` WHERE c.id = $1 FOR UPDATE OF c`, id))
		if err != nil {
			return err
		}
		if found.Default {
			return ErrDefaultCalendar
		}
		if _, err := tx.Exec(ctx, `DELETE FROM holiday_calendar WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete holiday calendar: %w", err)
		}
		return events.EmitInTenant(ctx, tx, "holiday_calendar.deleted", map[string]any{
			"id": id, "name": found.Name, "actorId": actor,
		})
	})
}

// ReplaceDays makes the calendar hold exactly these days.
func (s *Service) ReplaceDays(ctx context.Context, id uuid.UUID, days []Holiday, actor uuid.UUID) (*HolidayCalendar, db.LSN, error) {
	days, err := NormalizeDays(days)
	if err != nil {
		return nil, 0, err
	}
	var out *HolidayCalendar
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := scanCalendar(tx.QueryRow(ctx, selectCalendar+` WHERE c.id = $1 FOR UPDATE OF c`, id)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM holiday WHERE calendar_id = $1`, id); err != nil {
			return fmt.Errorf("clear holidays: %w", err)
		}
		if err := putDays(ctx, tx, id, days); err != nil {
			return err
		}
		if out, err = reload(ctx, tx, id); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "holiday_calendar.days_changed", map[string]any{
			"id": id, "days": len(days), "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// Import adds the days of an iCalendar file to a calendar. A day the
// calendar already has takes the file's name for it: the file is newer.
func (s *Service) Import(ctx context.Context, id uuid.UUID, file io.Reader, actor uuid.UUID) (*HolidayCalendar, int, db.LSN, error) {
	days, err := ImportICS(file)
	if err != nil {
		return nil, 0, 0, err
	}
	var out *HolidayCalendar
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := scanCalendar(tx.QueryRow(ctx, selectCalendar+` WHERE c.id = $1 FOR UPDATE OF c`, id)); err != nil {
			return err
		}
		if err := putDays(ctx, tx, id, days); err != nil {
			return err
		}
		if out, err = reload(ctx, tx, id); err != nil {
			return err
		}
		if len(out.Days) > MaxDays {
			return fmt.Errorf("%w: the calendar would hold more than %d days; remove past years first, or import into a new calendar", ErrBadFile, MaxDays)
		}
		return events.EmitInTenant(ctx, tx, "holiday_calendar.days_changed", map[string]any{
			"id": id, "days": len(out.Days), "imported": len(days), "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, 0, err
	}
	return out, len(days), lsn, nil
}

// putDays writes days onto a calendar. A day already there takes only the new
// name: a file has no half days, so it must not undo the one somebody set.
func putDays(ctx context.Context, tx db.DBTX, calendarID uuid.UUID, days []Holiday) error {
	if len(days) == 0 {
		return nil
	}
	dates := make([]string, len(days))
	names := make([]string, len(days))
	halves := make([]bool, len(days))
	for i, d := range days {
		dates[i], names[i], halves[i] = d.Day, d.Name, d.HalfDay
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO holiday (org_id, calendar_id, day, name, half_day)
		SELECT current_org_id(), $1, d.day::date, d.name, d.half_day
		FROM unnest($2::text[], $3::text[], $4::boolean[]) AS d(day, name, half_day)
		ON CONFLICT (calendar_id, day) DO UPDATE SET name = EXCLUDED.name`,
		calendarID, dates, names, halves)
	if err != nil {
		return fmt.Errorf("write holidays: %w", err)
	}
	return nil
}

// WorkingWeek is a person's week here: theirs when somebody set it, the
// standard week on the default calendar when nobody has.
func (s *Service) WorkingWeek(ctx context.Context, userID uuid.UUID) (*WorkingWeek, error) {
	var out *WorkingWeek
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = readWeek(ctx, tx, userID)
		return err
	})
	return out, err
}

func readWeek(ctx context.Context, tx db.DBTX, userID uuid.UUID) (*WorkingWeek, error) {
	var role string
	var calendarID *uuid.UUID
	var raw []byte
	err := tx.QueryRow(ctx, `
		SELECT m.org_role, s.calendar_id, s.minutes
		FROM org_member m
		LEFT JOIN member_schedule s ON s.org_id = m.org_id AND s.user_id = m.user_id
		WHERE m.org_id = current_org_id() AND m.user_id = $1`, userID).Scan(&role, &calendarID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotAMember
	}
	if err != nil {
		return nil, fmt.Errorf("read working week: %w", err)
	}
	if role == customerRole {
		return nil, ErrCustomer
	}

	out := &WorkingWeek{UserID: userID, CalendarID: calendarID, Saved: raw != nil, Minutes: StandardWeek()}
	if raw != nil {
		var stored map[string]int
		if err := json.Unmarshal(raw, &stored); err != nil {
			return nil, fmt.Errorf("read working week: %w", err)
		}
		if out.Minutes, err = NormalizeWeek(stored); err != nil {
			return nil, err
		}
	}

	var ref CalendarRef
	query := `SELECT id, name FROM holiday_calendar WHERE is_default`
	args := []any{}
	if calendarID != nil {
		query, args = `SELECT id, name FROM holiday_calendar WHERE id = $1`, []any{*calendarID}
	}
	err = tx.QueryRow(ctx, query, args...).Scan(&ref.ID, &ref.Name)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// An organization made before calendars existed, or by hand, may have none.
	case err != nil:
		return nil, fmt.Errorf("read the calendar of a working week: %w", err)
	default:
		out.Calendar = &ref
	}
	return out, nil
}

// customerRole is the standing of a portal customer, who has no working week.
const customerRole = "customer"

// WeekInput is a person's whole week. A nil calendar follows the default; a
// nil Minutes is the standard week.
type WeekInput struct {
	CalendarID *uuid.UUID
	Minutes    map[string]int
}

// SaveWorkingWeek sets a person's week and calendar.
func (s *Service) SaveWorkingWeek(ctx context.Context, userID uuid.UUID, in WeekInput, actor uuid.UUID) (*WorkingWeek, db.LSN, error) {
	minutes, err := NormalizeWeek(in.Minutes)
	if err != nil {
		return nil, 0, err
	}
	body, err := json.Marshal(minutes)
	if err != nil {
		return nil, 0, err
	}
	var out *WorkingWeek
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Read first so a stranger and a customer are told apart, before the
		// guard in the database refuses both alike.
		if _, err := readWeek(ctx, tx, userID); err != nil {
			return err
		}
		if in.CalendarID != nil {
			if _, err := scanCalendar(tx.QueryRow(ctx, selectCalendar+` WHERE c.id = $1`, *in.CalendarID)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO member_schedule (org_id, user_id, calendar_id, minutes)
			VALUES (current_org_id(), $1, $2, $3::jsonb)
			ON CONFLICT (org_id, user_id) DO UPDATE SET calendar_id = EXCLUDED.calendar_id, minutes = EXCLUDED.minutes`,
			userID, in.CalendarID, string(body))
		if err != nil {
			return fmt.Errorf("save working week: %w", err)
		}
		if out, err = readWeek(ctx, tx, userID); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "working_week.saved", map[string]any{
			"userId": userID, "calendarId": in.CalendarID, "minutes": minutes, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
