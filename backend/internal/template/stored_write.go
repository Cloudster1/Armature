package template

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// Make stores a template. With from, a built-in key or a stored id, it starts
// as a copy of that one and the input changes the copy.
func (s *Service) Make(ctx context.Context, from string, in Input, actor Actor) (*ProjectTemplate, db.LSN, error) {
	var out *ProjectTemplate
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		base, err := s.startFrom(ctx, tx, from)
		if err != nil {
			return err
		}
		if in.Name == nil && from != "" {
			name, err := copyName(ctx, tx, base.Name)
			if err != nil {
				return err
			}
			base.Name = name
		} else {
			base.Name = ""
		}
		next, err := merged(ctx, tx, base, in)
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO project_template (name, description, kind, board_type, planning_method, features, workflow_scheme_id, desk, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			next.Name, next.Description, string(next.Kind), string(next.BoardType), string(next.PlanningMethod),
			featureNames(next.Features), next.SchemeID, next.Desk, actor.UserID).Scan(&id)
		if isCode(err, "23505") {
			return ErrNameTaken
		}
		if err != nil {
			return err
		}
		if err := writeTypes(ctx, tx, id, nil, next.TypeIDs); err != nil {
			return err
		}
		if out, err = storedListed(ctx, tx, id); err != nil {
			return err
		}
		data := summary(out)
		if from != "" {
			data["from"] = from
		}
		return record(ctx, tx, "project_template.created", id, actor, data)
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// draft is a template being written, before it is checked.
type draft struct {
	Name           string
	Description    string
	Kind           project.Kind
	BoardType      board.Type
	PlanningMethod project.PlanningMethod
	Features       []project.Feature
	SchemeID       *uuid.UUID
	Desk           bool
	TypeIDs        []uuid.UUID
}

// startFrom is what a new template begins as: a copy of from, or a software
// project on a kanban board with every built-in type.
func (s *Service) startFrom(ctx context.Context, tx db.DBTX, from string) (*draft, error) {
	if from == "" {
		d := &draft{Kind: project.KindSoftware, BoardType: board.TypeKanban, PlanningMethod: project.MethodKanban,
			Features: project.DefaultFeatures(project.KindSoftware)}
		types, err := builtinTypes(ctx, tx, Template{})
		if err != nil {
			return nil, err
		}
		for _, t := range types {
			d.TypeIDs = append(d.TypeIDs, t.ID)
		}
		return d, nil
	}
	if id, err := uuid.Parse(from); err == nil {
		o, err := storedListed(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		d := &draft{
			Name: o.Name, Description: o.Description, Kind: o.Kind, BoardType: o.BoardType,
			PlanningMethod: o.PlanningMethod, Features: o.Features, Desk: o.Desk,
		}
		if o.Scheme != nil {
			d.SchemeID = o.Scheme.ID
		}
		for _, t := range o.IssueTypes {
			if !t.Retired {
				d.TypeIDs = append(d.TypeIDs, t.ID)
			}
		}
		return d, nil
	}
	tpl, ok := Find(from)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknown, from)
	}
	d := &draft{
		Name: tpl.Name, Description: tpl.Description, Kind: tpl.Kind, BoardType: tpl.BoardType,
		PlanningMethod: tpl.PlanningMethod, Features: slices.Clone(tpl.Features), Desk: tpl.Desk,
	}
	// The workflow a built-in brings becomes a real scheme, the same one a
	// project made from the built-in would get.
	if tpl.workflow != nil {
		id, err := s.ensureScheme(ctx, tx, tpl.workflow)
		if err != nil {
			return nil, err
		}
		d.SchemeID = &id
	}
	types, err := builtinTypes(ctx, tx, tpl)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		d.TypeIDs = append(d.TypeIDs, t.ID)
	}
	return d, nil
}

// copyName is the first of "Scrum copy", "Scrum copy 2" and on that no template has.
func copyName(ctx context.Context, tx db.DBTX, of string) (string, error) {
	base := of + " copy"
	for i := 1; i <= copyTries; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s %d", base, i)
		}
		if len(name) > MaxNameLength {
			break
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project_template WHERE lower(name) = lower($1))`, name).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return name, nil
		}
	}
	return "", invalid("give the copy a name of its own")
}

// merged applies an input over a draft and checks the result as a whole.
func merged(ctx context.Context, tx db.DBTX, d *draft, in Input) (*draft, error) {
	next := *d
	if in.Name != nil {
		next.Name = *in.Name
	}
	next.Name = strings.TrimSpace(next.Name)
	if next.Name == "" || len([]rune(next.Name)) > MaxNameLength {
		return nil, invalid("give the template a name of up to %d characters", MaxNameLength)
	}
	if in.Description != nil {
		next.Description = strings.TrimSpace(*in.Description)
	}
	if len([]rune(next.Description)) > MaxDescriptionLength {
		return nil, invalid("keep the description to %d characters", MaxDescriptionLength)
	}
	if in.Kind != nil {
		next.Kind = *in.Kind
	}
	if !next.Kind.Valid() {
		return nil, invalid("a project is software, service or business; pick one of those")
	}
	if in.BoardType != nil {
		next.BoardType = *in.BoardType
	}
	if !next.BoardType.Valid() {
		return nil, invalid("a board is either scrum or kanban; pick one of those")
	}
	if in.PlanningMethod != nil {
		next.PlanningMethod = *in.PlanningMethod
	}
	if !next.PlanningMethod.Valid() {
		return nil, invalid("a project plans as scrum or kanban; pick one of those")
	}
	if in.Features != nil {
		next.Features = slices.Clone(in.Features)
	}
	// Pages only a desk has go with a kind that is not service, unless the
	// caller named them, which is refused below.
	if in.Kind != nil && *in.Kind != project.KindService && in.Features == nil {
		next.Features = slices.DeleteFunc(next.Features, project.Feature.DeskOnly)
	}
	if len(next.Features) == 0 {
		next.Features = project.DefaultFeatures(next.Kind)
	}
	for _, f := range next.Features {
		if !f.Valid() {
			return nil, fmt.Errorf("%w: %q", project.ErrBadFeature, f)
		}
		if f.DeskOnly() && next.Kind != project.KindService {
			return nil, invalid("only a service project has %s; make the template's kind service or leave %s out", f.Word(), f.Word())
		}
	}
	next.Features = slices.Compact(sortedFeatures(next.Features))
	if in.Desk != nil {
		next.Desk = *in.Desk
	}
	if in.Kind != nil && *in.Kind != project.KindService && in.Desk == nil {
		next.Desk = false
	}
	if next.Desk && next.Kind != project.KindService {
		return nil, invalid("only a service project is set up as a desk; make the template's kind service or leave the desk off")
	}
	switch {
	case in.DefaultScheme:
		next.SchemeID = nil
	case in.SchemeID != nil:
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflow_scheme WHERE id = $1)`, *in.SchemeID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, invalid("that workflow scheme was not found; pick one from the list again")
		}
		id := *in.SchemeID
		next.SchemeID = &id
	}
	if in.IssueTypeIDs != nil {
		next.TypeIDs = slices.Clone(in.IssueTypeIDs)
	}
	if err := checkTypes(ctx, tx, next.TypeIDs, d.TypeIDs); err != nil {
		return nil, err
	}
	return &next, nil
}

func sortedFeatures(in []project.Feature) []project.Feature {
	order := project.AllFeatures()
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b project.Feature) int {
		return slices.Index(order, a) - slices.Index(order, b)
	})
	return out
}

// checkTypes refuses a set without a standard type that is not retired, a type
// named twice, and a retired type the template did not have already.
func checkTypes(ctx context.Context, tx db.DBTX, ids, had []uuid.UUID) error {
	if len(ids) == 0 {
		return invalid("a template needs at least one issue type; keep a standard one such as Task")
	}
	seen := map[uuid.UUID]bool{}
	standard := false
	for _, id := range ids {
		if seen[id] {
			return invalid("a type is listed twice; name each type once")
		}
		seen[id] = true
		var (
			name    string
			level   int
			retired bool
		)
		err := tx.QueryRow(ctx, `SELECT name, hierarchy_level, retired_at IS NOT NULL FROM issue_type WHERE id = $1`, id).Scan(&name, &level, &retired)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid("one of the issue types was not found; pick them from the list again")
		}
		if err != nil {
			return err
		}
		if retired && !slices.Contains(had, id) {
			return invalid("%s is retired and goes into nothing new; reinstate it first or pick another type", name)
		}
		standard = standard || (level == issue.LevelStandard && !retired)
	}
	if !standard {
		return invalid("a template needs a standard issue type that is not retired, such as Task; add one")
	}
	return nil
}

// writeTypes makes ids, in order, the template's types. A type it had already
// is moved rather than written again, since a retired one could not be.
func writeTypes(ctx context.Context, tx db.DBTX, templateID uuid.UUID, had, ids []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM project_template_issue_type WHERE template_id = $1 AND NOT (issue_type_id = ANY($2))`, templateID, ids); err != nil {
		return err
	}
	for i, id := range ids {
		var err error
		if slices.Contains(had, id) {
			_, err = tx.Exec(ctx, `UPDATE project_template_issue_type SET position = $3 WHERE template_id = $1 AND issue_type_id = $2`, templateID, id, i+1)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO project_template_issue_type (template_id, issue_type_id, position) VALUES ($1, $2, $3)`, templateID, id, i+1)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Update changes a stored template. Projects made from it keep what they got.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input, actor Actor) (*ProjectTemplate, db.LSN, error) {
	var out *ProjectTemplate
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM project_template WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		before, err := storedListed(ctx, tx, id)
		if err != nil {
			return err
		}
		d := &draft{
			Name: before.Name, Description: before.Description, Kind: before.Kind, BoardType: before.BoardType,
			PlanningMethod: before.PlanningMethod, Features: before.Features, Desk: before.Desk,
		}
		if before.Scheme != nil {
			d.SchemeID = before.Scheme.ID
		}
		for _, t := range before.IssueTypes {
			d.TypeIDs = append(d.TypeIDs, t.ID)
		}
		next, err := merged(ctx, tx, d, in)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE project_template SET name = $2, description = $3, kind = $4, board_type = $5, planning_method = $6,
			       features = $7, workflow_scheme_id = $8, desk = $9, updated_at = now()
			WHERE id = $1`,
			id, next.Name, next.Description, string(next.Kind), string(next.BoardType), string(next.PlanningMethod),
			featureNames(next.Features), next.SchemeID, next.Desk)
		if isCode(err, "23505") {
			return ErrNameTaken
		}
		if err != nil {
			return err
		}
		if err := writeTypes(ctx, tx, id, d.TypeIDs, next.TypeIDs); err != nil {
			return err
		}
		if out, err = storedListed(ctx, tx, id); err != nil {
			return err
		}
		return record(ctx, tx, "project_template.updated", id, actor, summary(out))
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// Archive takes a template off the chooser; projects made from it keep everything.
func (s *Service) Archive(ctx context.Context, id uuid.UUID, actor Actor) (*ProjectTemplate, db.LSN, error) {
	return s.setArchived(ctx, id, true, actor)
}

// Restore offers an archived template again.
func (s *Service) Restore(ctx context.Context, id uuid.UUID, actor Actor) (*ProjectTemplate, db.LSN, error) {
	return s.setArchived(ctx, id, false, actor)
}

func (s *Service) setArchived(ctx context.Context, id uuid.UUID, archive bool, actor Actor) (*ProjectTemplate, db.LSN, error) {
	var out *ProjectTemplate
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var archived bool
		err := tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM project_template WHERE id = $1 FOR UPDATE`, id).Scan(&archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// Asking twice is answered with how it already is, and recorded once.
		if archived != archive {
			action, query := "project_template.restored", `UPDATE project_template SET archived_at = NULL, updated_at = now() WHERE id = $1`
			if archive {
				action, query = "project_template.archived", `UPDATE project_template SET archived_at = now(), updated_at = now() WHERE id = $1`
			}
			if _, err := tx.Exec(ctx, query, id); err != nil {
				return err
			}
			if out, err = storedListed(ctx, tx, id); err != nil {
				return err
			}
			return record(ctx, tx, action, id, actor, map[string]any{"name": out.Name})
		}
		out, err = storedListed(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

func summary(o *ProjectTemplate) map[string]any {
	types := make([]string, 0, len(o.IssueTypes))
	for _, t := range o.IssueTypes {
		types = append(types, t.Name)
	}
	data := map[string]any{
		"name": o.Name, "kind": string(o.Kind), "boardType": string(o.BoardType), "planningMethod": string(o.PlanningMethod),
		"features": o.Features, "desk": o.Desk, "issueTypes": types,
	}
	if o.Scheme != nil {
		data["scheme"] = o.Scheme.Name
	}
	return data
}

func record(ctx context.Context, tx db.DBTX, action string, id uuid.UUID, actor Actor, data map[string]any) error {
	return audit.Write(ctx, tx, audit.Entry{
		Action: action, TargetType: "project_template", TargetID: &id, Actor: actor.UserID, IP: actor.IP, Data: data,
	})
}

func featureNames(features []project.Feature) []string {
	out := make([]string, len(features))
	for i, f := range features {
		out[i] = string(f)
	}
	return out
}

func isCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
