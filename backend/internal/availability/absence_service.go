package availability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/perm"
)

// Recorder is whoever writes an absence down, with what they may do here.
type Recorder struct {
	ID    uuid.UUID
	Perms perm.Set
}

const selectAbsence = `
SELECT a.id, a.user_id, u.name, to_char(a.starts_on, 'YYYY-MM-DD'), to_char(a.ends_on, 'YYYY-MM-DD'), a.half_day
FROM absence a JOIN app_user u ON u.id = a.user_id`

func scanAbsence(row pgx.Row) (*Absence, error) {
	var a Absence
	err := row.Scan(&a.ID, &a.UserID, &a.UserName, &a.StartsOn, &a.EndsOn, &a.HalfDay)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAbsenceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Absences lists who is away on any day from one day to another, both
// included, for everybody or for one person.
func (s *Service) Absences(ctx context.Context, from, to time.Time, userID *uuid.UUID) ([]Absence, error) {
	out := []Absence{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectAbsence+`
			WHERE a.starts_on <= $2 AND a.ends_on >= $1 AND ($3::uuid IS NULL OR a.user_id = $3)
			ORDER BY a.starts_on, lower(u.name), a.id`, from, to, userID)
		if err != nil {
			return fmt.Errorf("list absences: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAbsence(rows)
			if err != nil {
				return err
			}
			out = append(out, *a)
		}
		return rows.Err()
	})
	return out, err
}

// memberStanding tells a stranger from a portal customer, before the guard in
// the database refuses both alike.
func memberStanding(ctx context.Context, tx db.DBTX, userID uuid.UUID) error {
	var role string
	err := tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = current_org_id() AND user_id = $1`, userID).Scan(&role)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotAMember
	case err != nil:
		return fmt.Errorf("read the member: %w", err)
	case role == customerRole:
		return ErrCustomer
	}
	return nil
}

// mayRecord lets the person, an administrator, or somebody who manages teams
// in a project where the person is on a team. Leading a team is not enough.
func mayRecord(ctx context.Context, tx db.DBTX, by Recorder, userID uuid.UUID) error {
	if by.ID == userID || by.Perms.CanInOrg(perm.OrgAdminister) {
		return nil
	}
	if !by.Perms.CanSomewhere(perm.TeamManage) {
		return ErrMayNotRecord
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT p.key FROM team_member m
		JOIN team t ON t.id = m.team_id JOIN project p ON p.id = t.project_id
		WHERE m.user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("read the person's teams: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("read the person's teams: %w", err)
	}
	for _, key := range keys {
		if by.Perms.Can(perm.TeamManage, key) {
			return nil
		}
	}
	return ErrMayNotRecord
}

// RecordAbsence writes down that somebody is away.
func (s *Service) RecordAbsence(ctx context.Context, by Recorder, userID uuid.UUID, in AbsenceInput) (*Absence, db.LSN, error) {
	if in.StartsOn == nil {
		return nil, 0, invalid("an absence needs a first day")
	}
	endsOn, halfDay := *in.StartsOn, false
	if in.EndsOn != nil {
		endsOn = *in.EndsOn
	}
	if in.HalfDay != nil {
		halfDay = *in.HalfDay
	}
	checked, err := CheckAbsence(*in.StartsOn, endsOn, halfDay)
	if err != nil {
		return nil, 0, err
	}
	var out *Absence
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := memberStanding(ctx, tx, userID); err != nil {
			return err
		}
		if err := mayRecord(ctx, tx, by, userID); err != nil {
			return err
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO absence (org_id, user_id, starts_on, ends_on, half_day, created_by)
			VALUES (current_org_id(), $1, $2::date, $3::date, $4, $5) RETURNING id`,
			userID, checked.StartsOn, checked.EndsOn, checked.HalfDay, by.ID).Scan(&id)
		if err != nil {
			return absenceWriteError("record an absence", err)
		}
		if out, err = scanAbsence(tx.QueryRow(ctx, selectAbsence+` WHERE a.id = $1`, id)); err != nil {
			return err
		}
		return emitAbsence(ctx, tx, "absence.recorded", out, by)
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateAbsence moves an absence's days or makes it a half day, or not.
func (s *Service) UpdateAbsence(ctx context.Context, by Recorder, id uuid.UUID, in AbsenceInput) (*Absence, db.LSN, error) {
	var out *Absence
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanAbsence(tx.QueryRow(ctx, selectAbsence+` WHERE a.id = $1 FOR UPDATE OF a`, id))
		if err != nil {
			return err
		}
		if err := mayRecord(ctx, tx, by, before.UserID); err != nil {
			return err
		}
		startsOn, endsOn, halfDay := before.StartsOn, before.EndsOn, before.HalfDay
		if in.StartsOn != nil {
			startsOn = *in.StartsOn
		}
		if in.EndsOn != nil {
			endsOn = *in.EndsOn
		}
		if in.HalfDay != nil {
			halfDay = *in.HalfDay
		}
		checked, err := CheckAbsence(startsOn, endsOn, halfDay)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE absence SET starts_on = $2::date, ends_on = $3::date, half_day = $4 WHERE id = $1`,
			id, checked.StartsOn, checked.EndsOn, checked.HalfDay)
		if err != nil {
			return absenceWriteError("change an absence", err)
		}
		if out, err = scanAbsence(tx.QueryRow(ctx, selectAbsence+` WHERE a.id = $1`, id)); err != nil {
			return err
		}
		return emitAbsence(ctx, tx, "absence.changed", out, by)
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// RemoveAbsence takes an absence back: the person is not away after all.
func (s *Service) RemoveAbsence(ctx context.Context, by Recorder, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanAbsence(tx.QueryRow(ctx, selectAbsence+` WHERE a.id = $1 FOR UPDATE OF a`, id))
		if err != nil {
			return err
		}
		if err := mayRecord(ctx, tx, by, found.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM absence WHERE id = $1`, id); err != nil {
			return fmt.Errorf("remove an absence: %w", err)
		}
		return emitAbsence(ctx, tx, "absence.removed", found, by)
	})
}

// emitAbsence says who and by whom, never which days: the stream reaches
// webhooks and the audit log, and the days are for colleagues to read here.
func emitAbsence(ctx context.Context, tx db.DBTX, topic string, a *Absence, by Recorder) error {
	return events.EmitInTenant(ctx, tx, topic, map[string]any{"id": a.ID, "userId": a.UserID, "actorId": by.ID})
}

// absenceWriteError turns the database's refusal of an overlap into the one
// the person can act on.
func absenceWriteError(doing string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == exclusionViolation {
		return ErrOverlap
	}
	return fmt.Errorf("%s: %w", doing, err)
}

// exclusionViolation is SQLSTATE 23P01, which absence_no_overlap raises.
const exclusionViolation = "23P01"
