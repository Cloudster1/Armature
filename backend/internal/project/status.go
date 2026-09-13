package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// Health is how a project says it is doing, in the three words everybody uses.
type Health string

const (
	OnTrack  Health = "on_track"
	AtRisk   Health = "at_risk"
	OffTrack Health = "off_track"
)

// Valid reports whether the word is one of the three.
func (h Health) Valid() bool { return h == OnTrack || h == AtRisk || h == OffTrack }

// MaxStatusNote keeps an update a paragraph, not a report.
const MaxStatusNote = 2000

// StatusUpdate is one post about how the project is doing.
type StatusUpdate struct {
	ID         uuid.UUID  `json:"id"`
	ProjectKey string     `json:"projectKey"`
	Status     Health     `json:"status"`
	Note       string     `json:"note"`
	TargetOn   *string    `json:"targetOn,omitempty"`
	AuthorID   *uuid.UUID `json:"authorId,omitempty"`
	AuthorName string     `json:"authorName"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// StatusInput is what a post says.
type StatusInput struct {
	Status   Health
	Note     string
	TargetOn *string
}

// ErrBadStatus is a health word that is not one of the three.
var ErrBadStatus = errors.New("a status is on_track, at_risk or off_track")

const selectStatus = `
SELECT s.id, p.key, s.status, s.note, to_char(s.target_on, 'YYYY-MM-DD'), s.author_id, COALESCE(u.name, ''), s.created_at
FROM project_status_update s
JOIN project p ON p.id = s.project_id
LEFT JOIN app_user u ON u.id = s.author_id`

func scanStatus(row pgx.Row) (*StatusUpdate, error) {
	var s StatusUpdate
	err := row.Scan(&s.ID, &s.ProjectKey, &s.Status, &s.Note, &s.TargetOn, &s.AuthorID, &s.AuthorName, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// PostUpdate records how the project is doing. The latest post is the
// project's status until the next one.
func (s *Service) PostUpdate(ctx context.Context, key string, in StatusInput, actor uuid.UUID) (*StatusUpdate, db.LSN, error) {
	if !in.Status.Valid() {
		return nil, 0, ErrBadStatus
	}
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > MaxStatusNote {
		return nil, 0, fmt.Errorf("a status note must be %d characters or fewer", MaxStatusNote)
	}
	var target *time.Time
	if in.TargetOn != nil && strings.TrimSpace(*in.TargetOn) != "" {
		t, err := time.Parse("2006-01-02", strings.TrimSpace(*in.TargetOn))
		if err != nil {
			return nil, 0, errors.New("a target date is written as YYYY-MM-DD")
		}
		target = &t
	}
	var out *StatusUpdate
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var pid uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1 AND archived_at IS NULL`, NormalizeKey(key)).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO project_status_update (org_id, project_id, author_id, status, note, target_on)
			VALUES (current_org_id(), $1, $2, $3, $4, $5) RETURNING id`,
			pid, actor, string(in.Status), note, target).Scan(&id); err != nil {
			return fmt.Errorf("post status update: %w", err)
		}
		out, err = scanStatus(tx.QueryRow(ctx, selectStatus+` WHERE s.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "project.status_posted", map[string]any{
			"projectId": pid, "key": NormalizeKey(key), "status": in.Status, "updateId": id, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// MaxStatusHistory is how many posts the history shows; older ones are still
// in the table.
const MaxStatusHistory = 50

// Updates lists a project's posts, newest first.
func (s *Service) Updates(ctx context.Context, key string) ([]StatusUpdate, error) {
	out := []StatusUpdate{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var pid uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, NormalizeKey(key)).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectStatus+` WHERE s.project_id = $1 ORDER BY s.created_at DESC, s.id DESC LIMIT $2`, pid, MaxStatusHistory)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u, err := scanStatus(rows)
			if err != nil {
				return err
			}
			out = append(out, *u)
		}
		return rows.Err()
	})
	return out, err
}
