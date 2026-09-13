// Package version owns what a project ships: named versions, each with the
// issues it fixes and the ones it affects, released when the project says so.
package version

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/project"
)

var (
	ErrNotFound      = errors.New("version not found")
	ErrDuplicateName = errors.New("a version with that name is already here")
	ErrArchived      = errors.New("that version is archived")
	ErrNotReleased   = errors.New("only a released version can be archived")
)

// Version is one thing a project ships.
type Version struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"projectId"`
	ProjectKey  string     `json:"projectKey"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	StartOn     *time.Time `json:"startOn,omitempty"`
	ReleaseOn   *time.Time `json:"releaseOn,omitempty"`
	ReleasedAt  *time.Time `json:"releasedAt,omitempty"`
	ArchivedAt  *time.Time `json:"archivedAt,omitempty"`
	Position    int        `json:"position"`
	// Progress counts the issues that fix this version, by status category.
	Progress  milestone.Progress `json:"progress"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// Ref is a version as it hangs off an issue.
type Ref struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Released bool      `json:"released"`
}

// Input is a version as an editor sends it. Nil leaves a date alone; a set
// pointer holding nil clears it.
type Input struct {
	Name        *string
	Description *string
	StartOn     *(*time.Time)
	ReleaseOn   *(*time.Time)
}

// Notes are the finished issues of a version, grouped by type, in the order
// a release note lists them.
type Notes struct {
	Version Version     `json:"version"`
	Groups  []NoteGroup `json:"groups"`
	// Open counts what names this version and is not done yet.
	Open int `json:"open"`
}

type NoteGroup struct {
	Type   string     `json:"type"`
	Issues []NoteItem `json:"issues"`
}

type NoteItem struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
}

type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectVersion = `
SELECT v.id, v.project_id, p.key, v.name, v.description, v.start_on, v.release_on, v.released_at, v.archived_at, v.position,
       v.created_at, v.updated_at,
       (SELECT count(*) FROM issue_version iv JOIN issue i ON i.id = iv.issue_id JOIN issue_status s ON s.id = i.status_id
         WHERE iv.version_id = v.id AND iv.role = 'fix' AND s.category = 'done'),
       (SELECT count(*) FROM issue_version iv JOIN issue i ON i.id = iv.issue_id JOIN issue_status s ON s.id = i.status_id
         WHERE iv.version_id = v.id AND iv.role = 'fix' AND s.category = 'in_progress'),
       (SELECT count(*) FROM issue_version iv JOIN issue i ON i.id = iv.issue_id JOIN issue_status s ON s.id = i.status_id
         WHERE iv.version_id = v.id AND iv.role = 'fix' AND s.category = 'todo')
FROM version v
JOIN project p ON p.id = v.project_id`

// order puts unreleased versions first by release date, then released ones
// newest first, archived last.
const order = ` ORDER BY (v.archived_at IS NOT NULL), (v.released_at IS NOT NULL), v.release_on NULLS LAST, v.released_at DESC, v.position, v.created_at`

func scan(row pgx.Row) (*Version, error) {
	var (
		v                      Version
		done, inProgress, todo int
	)
	err := row.Scan(&v.ID, &v.ProjectID, &v.ProjectKey, &v.Name, &v.Description, &v.StartOn, &v.ReleaseOn, &v.ReleasedAt, &v.ArchivedAt, &v.Position,
		&v.CreatedAt, &v.UpdatedAt, &done, &inProgress, &todo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Progress = milestone.Measure(done, inProgress, todo)
	return &v, nil
}

// List is a project's versions; archived ones only when asked.
func (s *Service) List(ctx context.Context, projectKey string, includeArchived bool) ([]Version, error) {
	query := selectVersion + ` WHERE p.key = $1`
	if !includeArchived {
		query += ` AND v.archived_at IS NULL`
	}
	out := []Version{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, query+order, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, *v)
		}
		return rows.Err()
	})
	return out, err
}

// ByID reads one version.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Version, error) {
	var out *Version
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1`, id))
		return err
	})
	return out, err
}

// Create adds a version to a project.
func (s *Service) Create(ctx context.Context, projectKey string, in Input, actor uuid.UUID) (*Version, db.LSN, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return nil, 0, errors.New("a version needs a name")
	}
	var start, release *time.Time
	if in.StartOn != nil {
		start = *in.StartOn
	}
	if in.ReleaseOn != nil {
		release = *in.ReleaseOn
	}
	if start != nil && release != nil && start.After(*release) {
		return nil, 0, errors.New("a version cannot start after it is released")
	}
	description := ""
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	var out *Version
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1 AND archived_at IS NULL`, project.NormalizeKey(projectKey)).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO version (org_id, project_id, name, description, start_on, release_on, position)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, COALESCE((SELECT max(position) + 1 FROM version WHERE project_id = $1), 0))
			RETURNING id`, projectID, name, description, start, release).Scan(&id)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if err != nil {
			return fmt.Errorf("create version: %w", err)
		}
		out, err = scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "version.created", map[string]any{"versionId": id, "projectId": projectID, "name": name, "actorId": actor})
	})
	return out, lsn, err
}

// Update changes a version's details.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input, actor uuid.UUID) (*Version, db.LSN, error) {
	var out *Version
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1 FOR UPDATE OF v`, id))
		if err != nil {
			return err
		}
		if current.ArchivedAt != nil {
			return ErrArchived
		}
		name, description, start, release := current.Name, current.Description, current.StartOn, current.ReleaseOn
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
			if name == "" {
				return errors.New("a version needs a name")
			}
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.StartOn != nil {
			start = *in.StartOn
		}
		if in.ReleaseOn != nil {
			release = *in.ReleaseOn
		}
		if start != nil && release != nil && start.After(*release) {
			return errors.New("a version cannot start after it is released")
		}
		_, err = tx.Exec(ctx, `UPDATE version SET name = $2, description = $3, start_on = $4, release_on = $5 WHERE id = $1`, id, name, description, start, release)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// Release marks the version shipped, now; Unrelease takes that back.
func (s *Service) Release(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Version, db.LSN, error) {
	return s.mark(ctx, id, `UPDATE version SET released_at = now() WHERE id = $1 AND archived_at IS NULL`, "version.released", actor)
}

func (s *Service) Unrelease(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Version, db.LSN, error) {
	return s.mark(ctx, id, `UPDATE version SET released_at = NULL WHERE id = $1 AND archived_at IS NULL`, "version.unreleased", actor)
}

// Archive puts a released version away; it can no longer be named on an issue.
func (s *Service) Archive(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Version, db.LSN, error) {
	var out *Version
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1 FOR UPDATE OF v`, id))
		if err != nil {
			return err
		}
		if current.ReleasedAt == nil {
			return ErrNotReleased
		}
		if _, err := tx.Exec(ctx, `UPDATE version SET archived_at = COALESCE(archived_at, now()) WHERE id = $1`, id); err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "version.archived", map[string]any{"versionId": id, "actorId": actor})
	})
	return out, lsn, err
}

func (s *Service) mark(ctx context.Context, id uuid.UUID, sql, topic string, actor uuid.UUID) (*Version, db.LSN, error) {
	var out *Version
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, sql, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			if _, err := scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1`, id)); err != nil {
				return err
			}
			return ErrArchived
		}
		out, err = scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, topic, map[string]any{"versionId": id, "name": out.Name, "actorId": actor})
	})
	return out, lsn, err
}

// Delete removes a version; the issues that named it simply stop naming it.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var name string
		err := tx.QueryRow(ctx, `DELETE FROM version WHERE id = $1 RETURNING name`, id).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "version.deleted", map[string]any{"versionId": id, "name": name, "actorId": actor})
	})
}

// ReleaseNotes lists what the version fixed, done issues by type, and how
// much named it that is not done yet.
func (s *Service) ReleaseNotes(ctx context.Context, id uuid.UUID) (*Notes, error) {
	var out *Notes
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		v, err := scan(tx.QueryRow(ctx, selectVersion+` WHERE v.id = $1`, id))
		if err != nil {
			return err
		}
		out = &Notes{Version: *v, Groups: []NoteGroup{}}
		rows, err := tx.Query(ctx, `
			SELECT it.name, p.key || '-' || i.key_num, i.summary, s.category = 'done'
			FROM issue_version iv
			JOIN issue i ON i.id = iv.issue_id
			JOIN project p ON p.id = i.project_id
			JOIN issue_type it ON it.id = i.issue_type_id
			JOIN issue_status s ON s.id = i.status_id
			WHERE iv.version_id = $1 AND iv.role = 'fix'
			ORDER BY it.hierarchy_level DESC, it.position, i.key_num`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		index := map[string]int{}
		for rows.Next() {
			var (
				typeName, key, summary string
				done                   bool
			)
			if err := rows.Scan(&typeName, &key, &summary, &done); err != nil {
				return err
			}
			if !done {
				out.Open++
				continue
			}
			at, ok := index[typeName]
			if !ok {
				at = len(out.Groups)
				index[typeName] = at
				out.Groups = append(out.Groups, NoteGroup{Type: typeName, Issues: []NoteItem{}})
			}
			out.Groups[at].Issues = append(out.Groups[at].Issues, NoteItem{Key: key, Summary: summary})
		}
		return rows.Err()
	})
	return out, err
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
