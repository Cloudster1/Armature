// Package issuetype keeps the installation's library of issue types: the ones
// it started with, which code finds by name, and those its administrators add.
package issuetype

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

// Icon names the glyph a type is drawn with.
type Icon string

// Icons are the glyphs the web knows. The database's issue_type_icon_known
// check holds the same list, and a test compares the two.
var Icons = []Icon{"task", "bug", "story", "epic", "subtask", "initiative", "change", "improvement", "incident", "question", "research", "release"}

// IconNames is Icons as plain strings, for the API's description.
func IconNames() []string {
	out := make([]string, len(Icons))
	for i, icon := range Icons {
		out[i] = string(icon)
	}
	return out
}

// The levels a type may sit at, as the database's range check allows.
const (
	MinLevel = issue.LevelSubtask
	MaxLevel = issue.MaxLevel
)

// Limits on what an administrator writes, so a picker stays readable.
const (
	MaxNameLength        = 60
	MaxDescriptionLength = 300
)

// IssueType is one type as the client sees it.
type IssueType struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Icon        Icon      `json:"icon"`
	Level       int       `json:"level"`
	IsSubtask   bool      `json:"isSubtask"`
	Position    int       `json:"position"`
	// Builtin types are the ones code finds by name; they keep their name and level.
	Builtin bool `json:"builtin"`
	// Retired types stay on their issues and are offered for nothing new.
	Retired   bool       `json:"retired"`
	RetiredAt *time.Time `json:"retiredAt,omitempty"`
}

var (
	// ErrNotFound is a type that is not in the library.
	ErrNotFound = errors.New("that issue type was not found")
	// ErrNameTaken is a name another type has already, case aside.
	ErrNameTaken = errors.New("there is already an issue type with that name")
	// ErrBadName is a blank or overlong name.
	ErrBadName = fmt.Errorf("give the type a name of up to %d characters", MaxNameLength)
	// ErrBadDescription is an overlong description.
	ErrBadDescription = fmt.Errorf("keep the description to %d characters", MaxDescriptionLength)
	// ErrBadIcon is a glyph the web does not draw.
	ErrBadIcon = errors.New("that is not one of the issue type icons; pick one from the list")
	// ErrBadLevel is a level outside the hierarchy.
	ErrBadLevel = fmt.Errorf("a type sits at a level from %d to %d", MinLevel, MaxLevel)
)

// BuiltinError is a change a built-in type refuses, naming the type and what to do instead.
type BuiltinError struct {
	Name string
	// What is refused: "rename", "level", "retire" or "delete".
	What string
}

func (e *BuiltinError) Error() string {
	switch e.What {
	case "retire":
		return fmt.Sprintf("%s is built in and stays offered. Retire a type of your own instead", e.Name)
	case "delete":
		return fmt.Sprintf("%s is built in and cannot be deleted. Change its description or icon instead, or add a type of your own", e.Name)
	default:
		return fmt.Sprintf("%s is built in, so its name and level stay. Change its description or icon, or add a type of your own", e.Name)
	}
}

// InUseError is a type something still uses, so it cannot be deleted or change level.
type InUseError struct {
	Name string
	// By says what uses it: "issues" or "request types".
	By string
	// Level is set when the refusal is of a new level rather than a deletion.
	Level bool
}

func (e *InUseError) Error() string {
	if e.Level {
		return fmt.Sprintf("Issues already use %s, so its level cannot change. Add a new type at the level you want instead", e.Name)
	}
	if e.By == "request types" {
		return fmt.Sprintf("A service desk request type files issues as %s, so it cannot be deleted. Point the request type at another type, or retire %s instead", e.Name, e.Name)
	}
	return fmt.Sprintf("Issues use %s, so it cannot be deleted. Retire it instead: its issues keep it and nothing new is offered it", e.Name)
}

// Service keeps the library.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectType = `
SELECT id, name, description, icon, hierarchy_level, is_subtask, position, builtin, retired_at
FROM issue_type`

func scan(row pgx.Row) (*IssueType, error) {
	var t IssueType
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Icon, &t.Level, &t.IsSubtask, &t.Position, &t.Builtin, &t.RetiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Retired = t.RetiredAt != nil
	return &t, nil
}

// List returns the types, highest level first so a picker reads down the tree;
// retired ones only when asked for.
func (s *Service) List(ctx context.Context, withRetired bool) ([]IssueType, error) {
	types := []IssueType{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectType+`
			WHERE $1 OR retired_at IS NULL
			ORDER BY hierarchy_level DESC, position, name`, withRetired)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scan(rows)
			if err != nil {
				return err
			}
			types = append(types, *t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list issue types: %w", err)
	}
	return types, nil
}

// Input is a type being added.
type Input struct {
	Name        string
	Description string
	Icon        Icon
	Level       int
}

// UpdateInput carries what an edit changes; nil leaves a part alone.
type UpdateInput struct {
	Name        *string
	Description *string
	Icon        *Icon
	Level       *int
}

// Actor is who acts and from where, for the audit log.
type Actor struct {
	UserID uuid.UUID
	IP     string
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLength {
		return "", ErrBadName
	}
	return name, nil
}

func cleanDescription(text string) (string, error) {
	text = strings.TrimSpace(text)
	if len([]rune(text)) > MaxDescriptionLength {
		return "", ErrBadDescription
	}
	return text, nil
}

func checkIcon(icon Icon) error {
	if !slices.Contains(Icons, icon) {
		return ErrBadIcon
	}
	return nil
}

func checkLevel(level int) error {
	if level < MinLevel || level > MaxLevel {
		return ErrBadLevel
	}
	return nil
}

// Create adds a type of the installation's own, after the others at its level.
func (s *Service) Create(ctx context.Context, in Input, actor Actor) (*IssueType, db.LSN, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	description, err := cleanDescription(in.Description)
	if err != nil {
		return nil, 0, err
	}
	if in.Icon == "" {
		in.Icon = "task"
	}
	if err := checkIcon(in.Icon); err != nil {
		return nil, 0, err
	}
	if err := checkLevel(in.Level); err != nil {
		return nil, 0, err
	}
	var made *IssueType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		// Last in the list, so the first type at a level, which an issue made
		// without a type becomes, stays the one it was.
		err := tx.QueryRow(ctx, `
			INSERT INTO issue_type (name, description, icon, hierarchy_level, position)
			VALUES ($1, $2, $3, $4, (SELECT COALESCE(max(position), 0) + 1 FROM issue_type))
			RETURNING id`, name, description, in.Icon, in.Level).Scan(&id)
		if isCode(err, "23505") {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create issue type: %w", err)
		}
		if made, err = scan(tx.QueryRow(ctx, selectType+` WHERE id = $1`, id)); err != nil {
			return err
		}
		return record(ctx, tx, "issue_type.created", made, actor, nil)
	})
	if err != nil {
		return nil, 0, err
	}
	return made, lsn, nil
}

// Update changes a type. A built-in one keeps its name and level, and a type
// issues use keeps its level, since the tree under them would no longer hold.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput, actor Actor) (*IssueType, db.LSN, error) {
	var updated *IssueType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scan(tx.QueryRow(ctx, selectType+` WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		after := *before
		if in.Name != nil {
			if after.Name, err = cleanName(*in.Name); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if after.Description, err = cleanDescription(*in.Description); err != nil {
				return err
			}
		}
		if in.Icon != nil {
			if err := checkIcon(*in.Icon); err != nil {
				return err
			}
			after.Icon = *in.Icon
		}
		if in.Level != nil {
			if err := checkLevel(*in.Level); err != nil {
				return err
			}
			after.Level = *in.Level
		}
		if before.Builtin && (after.Name != before.Name || after.Level != before.Level) {
			return &BuiltinError{Name: before.Name, What: "rename"}
		}
		if after.Level != before.Level {
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue WHERE issue_type_id = $1)`, id).Scan(&used); err != nil {
				return err
			}
			if used {
				return &InUseError{Name: before.Name, By: "issues", Level: true}
			}
		}
		_, err = tx.Exec(ctx, `
			UPDATE issue_type SET name = $2, description = $3, icon = $4, hierarchy_level = $5
			WHERE id = $1`, id, after.Name, after.Description, after.Icon, after.Level)
		if isCode(err, "23505") {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("update issue type: %w", err)
		}
		if updated, err = scan(tx.QueryRow(ctx, selectType+` WHERE id = $1`, id)); err != nil {
			return err
		}
		return record(ctx, tx, "issue_type.updated", updated, actor, changes(before, updated))
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}

// Retire stops offering a type for anything new; its issues keep it.
func (s *Service) Retire(ctx context.Context, id uuid.UUID, actor Actor) (*IssueType, db.LSN, error) {
	return s.setRetired(ctx, id, true, actor)
}

// Reinstate offers a retired type again.
func (s *Service) Reinstate(ctx context.Context, id uuid.UUID, actor Actor) (*IssueType, db.LSN, error) {
	return s.setRetired(ctx, id, false, actor)
}

func (s *Service) setRetired(ctx context.Context, id uuid.UUID, retire bool, actor Actor) (*IssueType, db.LSN, error) {
	var out *IssueType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scan(tx.QueryRow(ctx, selectType+` WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if retire && before.Builtin {
			return &BuiltinError{Name: before.Name, What: "retire"}
		}
		// Asking twice is answered with how it already is, and recorded once.
		if before.Retired == retire {
			out = before
			return nil
		}
		action, query := "issue_type.reinstated", `UPDATE issue_type SET retired_at = NULL WHERE id = $1`
		if retire {
			action, query = "issue_type.retired", `UPDATE issue_type SET retired_at = now() WHERE id = $1`
		}
		if _, err := tx.Exec(ctx, query, id); err != nil {
			return err
		}
		if out, err = scan(tx.QueryRow(ctx, selectType+` WHERE id = $1`, id)); err != nil {
			return err
		}
		return record(ctx, tx, action, out, actor, nil)
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// Delete removes a type nothing uses. One that issues use is retired instead,
// which is what the refusal says.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, actor Actor) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		gone, err := scan(tx.QueryRow(ctx, selectType+` WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if gone.Builtin {
			return &BuiltinError{Name: gone.Name, What: "delete"}
		}
		var issues, requests bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM issue WHERE issue_type_id = $1),
			       EXISTS (SELECT 1 FROM request_type WHERE issue_type_id = $1)`, id).Scan(&issues, &requests); err != nil {
			return err
		}
		switch {
		case issues:
			return &InUseError{Name: gone.Name, By: "issues"}
		case requests:
			return &InUseError{Name: gone.Name, By: "request types"}
		}
		// The foreign keys are the backstop for an issue filed in between.
		if _, err := tx.Exec(ctx, `DELETE FROM issue_type WHERE id = $1`, id); isCode(err, "23503") {
			return &InUseError{Name: gone.Name, By: "issues"}
		} else if err != nil {
			return fmt.Errorf("delete issue type: %w", err)
		}
		return record(ctx, tx, "issue_type.deleted", gone, actor, nil)
	})
}

// changes names what an edit changed, as from and to.
func changes(before, after *IssueType) map[string]any {
	out := map[string]any{}
	add := func(field string, from, to any) {
		if from != to {
			out[field] = map[string]any{"from": from, "to": to}
		}
	}
	add("name", before.Name, after.Name)
	add("description", before.Description, after.Description)
	add("icon", string(before.Icon), string(after.Icon))
	add("level", before.Level, after.Level)
	return out
}

func record(ctx context.Context, tx db.DBTX, action string, t *IssueType, actor Actor, changed map[string]any) error {
	data := map[string]any{"name": t.Name, "level": t.Level, "icon": string(t.Icon)}
	if len(changed) > 0 {
		data["changes"] = changed
	}
	return audit.Write(ctx, tx, audit.Entry{
		Action: action, TargetType: "issue_type", TargetID: &t.ID, Actor: actor.UserID, IP: actor.IP, Data: data,
	})
}

func isCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
