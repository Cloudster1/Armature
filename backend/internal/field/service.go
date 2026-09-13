package field

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// Service defines fields and sets their values.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// MaxNameLength keeps a field's name short enough for a label.
const MaxNameLength = 60

const selectField = `
SELECT f.id, f.project_id, COALESCE(p.key, ''), f.name, f.kind, f.options, f.position, f.created_at, f.updated_at
FROM custom_field f
LEFT JOIN project p ON p.id = f.project_id`

func scanField(row pgx.Row) (*Field, error) {
	var (
		f       Field
		options []byte
	)
	err := row.Scan(&f.ID, &f.ProjectID, &f.ProjectKey, &f.Name, &f.Kind, &options, &f.Position, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(options, &f.Options); err != nil {
		return nil, fmt.Errorf("decode field options: %w", err)
	}
	if f.Options == nil {
		f.Options = []string{}
	}
	f.Org = f.ProjectID == nil
	return &f, nil
}

// Fields lists the fields an issue of the project has: the organization's
// first, then the project's own, each in the order they are shown.
func (s *Service) Fields(ctx context.Context, projectKey string) ([]Field, error) {
	out := []Field{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := projectID(ctx, tx, projectKey); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectField+` WHERE p.key = $1 OR f.project_id IS NULL ORDER BY f.project_id NULLS FIRST, f.position, f.created_at`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanField(rows)
			if err != nil {
				return err
			}
			out = append(out, *f)
		}
		return rows.Err()
	})
	return out, err
}

// Field reads one field by id.
func (s *Service) Field(ctx context.Context, id uuid.UUID) (*Field, error) {
	var found *Field
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scanField(tx.QueryRow(ctx, selectField+` WHERE f.id = $1`, id))
		return err
	})
	return found, err
}

// OrgFields lists the organization's own fields, the ones every project has.
func (s *Service) OrgFields(ctx context.Context) ([]Field, error) {
	out := []Field{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectField+` WHERE f.project_id IS NULL ORDER BY f.position, f.created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanField(rows)
			if err != nil {
				return err
			}
			out = append(out, *f)
		}
		return rows.Err()
	})
	return out, err
}

// Input describes a field being made or changed.
type Input struct {
	Name    string
	Kind    Kind
	Options []string
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("a field needs a name")
	}
	if len([]rune(name)) > MaxNameLength {
		return "", fmt.Errorf("a field's name must be %d characters or fewer", MaxNameLength)
	}
	return name, nil
}

// Create defines a field for a project. It goes to the end of the list.
func (s *Service) Create(ctx context.Context, projectKey string, in Input) (*Field, db.LSN, error) {
	return s.create(ctx, &projectKey, in)
}

// CreateOrg defines a field every project of the organization has.
func (s *Service) CreateOrg(ctx context.Context, in Input) (*Field, db.LSN, error) {
	return s.create(ctx, nil, in)
}

func (s *Service) create(ctx context.Context, projectKey *string, in Input) (*Field, db.LSN, error) {
	name, err := checkName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	if !in.Kind.Valid() {
		return nil, 0, ErrBadKind
	}
	options, err := cleanOptions(in.Kind, in.Options)
	if err != nil {
		return nil, 0, err
	}
	encoded, _ := json.Marshal(options)

	var created *Field
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var pid *uuid.UUID
		if projectKey != nil {
			found, err := projectID(ctx, tx, *projectKey)
			if err != nil {
				return err
			}
			pid = &found
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO custom_field (org_id, project_id, name, kind, options, position)
			VALUES (current_org_id(), $1, $2, $3, $4,
			        (SELECT COALESCE(max(position), -1) + 1 FROM custom_field WHERE project_id IS NOT DISTINCT FROM $1))
			RETURNING id`, pid, name, string(in.Kind), encoded).Scan(&id)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create field: %w", err)
		}
		created, err = scanField(tx.QueryRow(ctx, selectField+` WHERE f.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// Promote makes a project's field the organization's. Other projects' fields
// of the same name and kind are folded into it, their answers kept, so the
// name means one field afterwards; a same-named field of another kind refuses.
func (s *Service) Promote(ctx context.Context, id uuid.UUID) (*Field, db.LSN, error) {
	var out *Field
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		f, err := scanField(tx.QueryRow(ctx, selectField+` WHERE f.id = $1 FOR UPDATE OF f`, id))
		if err != nil {
			return err
		}
		if f.Org {
			return nil
		}
		rows, err := tx.Query(ctx, `
			SELECT id, kind, options FROM custom_field
			WHERE lower(name) = lower($1) AND id <> $2 AND project_id IS NOT NULL
			ORDER BY created_at FOR UPDATE`, f.Name, id)
		if err != nil {
			return err
		}
		var (
			twins   []uuid.UUID
			options = append([]string{}, f.Options...)
		)
		for rows.Next() {
			var (
				twin uuid.UUID
				kind Kind
				raw  []byte
			)
			if err := rows.Scan(&twin, &kind, &raw); err != nil {
				rows.Close()
				return err
			}
			if kind != f.Kind {
				rows.Close()
				return ErrKindMismatch
			}
			var more []string
			_ = json.Unmarshal(raw, &more)
			for _, o := range more {
				if !slices.Contains(options, o) {
					options = append(options, o)
				}
			}
			twins = append(twins, twin)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// The field becomes the organization's first, so the answers folded
		// into it fit; the name check waits for the commit, when the twins
		// are gone.
		if _, err := tx.Exec(ctx, `SET CONSTRAINTS custom_field_name_unshadowed DEFERRED`); err != nil {
			return err
		}
		encoded, _ := json.Marshal(options)
		if _, err := tx.Exec(ctx, `
			UPDATE custom_field SET project_id = NULL, options = $2,
			       position = (SELECT COALESCE(max(position), -1) + 1 FROM custom_field WHERE project_id IS NULL)
			WHERE id = $1`, id, encoded); err != nil {
			return fmt.Errorf("promote field: %w", err)
		}
		if len(twins) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE issue_field_value SET field_id = $1 WHERE field_id = ANY($2)`, id, twins); err != nil {
				return fmt.Errorf("fold field values: %w", err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM custom_field WHERE id = ANY($1)`, twins); err != nil {
				return err
			}
		}
		out, err = scanField(tx.QueryRow(ctx, selectField+` WHERE f.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "field.promoted", map[string]any{
			"fieldId": id, "name": f.Name, "from": f.ProjectKey, "folded": len(twins),
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateInput carries what an edit may change. The kind is fixed once values
// exist in it; nil leaves a part alone.
type UpdateInput struct {
	Name    *string
	Options *[]string
}

// Update renames a field or changes its options. Values already stored that
// name an option being removed are kept: they are still what somebody said.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Field, db.LSN, error) {
	var updated *Field
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanField(tx.QueryRow(ctx, selectField+` WHERE f.id = $1 FOR UPDATE OF f`, id))
		if err != nil {
			return err
		}
		if in.Name != nil {
			name, err := checkName(*in.Name)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE custom_field SET name = $2 WHERE id = $1`, id, name); isUniqueViolation(err) {
				return ErrNameTaken
			} else if err != nil {
				return err
			}
		}
		if in.Options != nil {
			if before.Kind != Select {
				return fmt.Errorf("%w: only a select field has options", ErrBadValue)
			}
			options, err := cleanOptions(Select, *in.Options)
			if err != nil {
				return err
			}
			encoded, _ := json.Marshal(options)
			if _, err := tx.Exec(ctx, `UPDATE custom_field SET options = $2 WHERE id = $1`, id, encoded); err != nil {
				return err
			}
		}
		updated, err = scanField(tx.QueryRow(ctx, selectField+` WHERE f.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}

// Delete removes a field and every value ever given for it.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM custom_field WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Values lists every field of the issue's project with the issue's answer to
// it, unanswered ones included, so a client can draw the whole form at once.
func (s *Service) Values(ctx context.Context, issueKey string) ([]Value, error) {
	projectKey, num, err := issue.ParseKey(issueKey)
	if err != nil {
		return nil, err
	}
	out := []Value{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var issueID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return issue.ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT f.id, f.project_id, COALESCE(p.key, ''), f.name, f.kind, f.options, f.position, f.created_at, f.updated_at, v.value
			FROM custom_field f
			LEFT JOIN project p ON p.id = f.project_id
			LEFT JOIN issue_field_value v ON v.field_id = f.id AND v.issue_id = $2
			WHERE p.key = $1 OR f.project_id IS NULL
			ORDER BY f.project_id NULLS FIRST, f.position, f.created_at`, projectKey, issueID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				f       Field
				options []byte
				raw     []byte
			)
			if err := rows.Scan(&f.ID, &f.ProjectID, &f.ProjectKey, &f.Name, &f.Kind, &options, &f.Position,
				&f.CreatedAt, &f.UpdatedAt, &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(options, &f.Options); err != nil {
				return fmt.Errorf("decode field options: %w", err)
			}
			if f.Options == nil {
				f.Options = []string{}
			}
			f.Org = f.ProjectID == nil
			v := Value{Field: f}
			if raw != nil {
				v.Value = json.RawMessage(raw)
				v.Display = display(f, v.Value)
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// Set records an issue's answer to a field, or clears it with a null. The
// change goes in the changelog under the field's own name.
func (s *Service) Set(ctx context.Context, issueKey string, fieldID uuid.UUID, raw json.RawMessage, actor issue.Actor) (*Value, db.LSN, error) {
	projectKey, num, err := issue.ParseKey(issueKey)
	if err != nil {
		return nil, 0, err
	}
	var out *Value
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			issueID   uuid.UUID
			projectID uuid.UUID
		)
		err := tx.QueryRow(ctx, `
			SELECT i.id, i.project_id FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num).Scan(&issueID, &projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return issue.ErrNotFound
		}
		if err != nil {
			return err
		}
		f, err := scanField(tx.QueryRow(ctx, selectField+` WHERE f.id = $1`, fieldID))
		if err != nil {
			return err
		}
		if f.ProjectID != nil && *f.ProjectID != projectID {
			return ErrOtherProject
		}

		var before []byte
		err = tx.QueryRow(ctx, `SELECT value FROM issue_field_value WHERE issue_id = $1 AND field_id = $2`,
			issueID, fieldID).Scan(&before)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		value, shown, err := normalize(*f, raw)
		if err != nil {
			return err
		}
		if value == nil {
			if _, err := tx.Exec(ctx, `DELETE FROM issue_field_value WHERE issue_id = $1 AND field_id = $2`, issueID, fieldID); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `
				INSERT INTO issue_field_value (org_id, issue_id, field_id, value)
				VALUES (current_org_id(), $1, $2, $3)
				ON CONFLICT (issue_id, field_id) DO UPDATE SET value = EXCLUDED.value`,
				issueID, fieldID, []byte(value)); err != nil {
				return fmt.Errorf("set field: %w", err)
			}
		}

		out = &Value{Field: *f, Value: value, Display: shown}
		from := ""
		if before != nil {
			from = display(*f, before)
		}
		if from == shown {
			return nil
		}
		change := issue.Change{Field: f.Name, From: from, To: shown}
		if err := issue.RecordChanges(ctx, tx, issueID, actor.UserID, []issue.Change{change}); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": issueID,
			"key":     issue.FormatKey(projectKey, num),
			"changes": []issue.Change{change},
			"actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// projectID resolves a key to a live project's id.
func projectID(ctx context.Context, tx db.DBTX, projectKey string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1 AND archived_at IS NULL`, project.NormalizeKey(projectKey)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, project.ErrNotFound
	}
	return id, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
