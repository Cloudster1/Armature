package project

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
	"github.com/armature/armature/backend/internal/workflow"
)

// Provisioner prepares whatever a new project needs beyond its own row.
//
// It exists so that projects do not have to know what a board is. A board
// depends on issues, and issues depend on projects; having projects depend on
// boards in turn would close that loop. Instead the caller supplies the
// provisioners, each of which runs inside the transaction that creates the
// project, so a project and everything it needs to be usable commit together.
type Provisioner interface {
	ProvisionProject(ctx context.Context, tx db.DBTX, created *Project, in CreateInput, statuses []workflow.Status) error
}

// Service implements project use cases. Every method is tenant scoped: it goes
// through the cluster's ordinary read and write paths, so row level security
// applies without the queries having to remember it.
type Service struct {
	db           *db.Cluster
	workflows    *workflow.Store
	provisioners []Provisioner
}

func NewService(cluster *db.Cluster, provisioners ...Provisioner) *Service {
	return &Service{db: cluster, workflows: workflow.NewStore(), provisioners: provisioners}
}

// CreateInput describes a new project.
type CreateInput struct {
	Key         string
	Name        string
	Description string
	Kind        Kind
	LeadID      *uuid.UUID
	// SchemeID gives the project a scheme of its own. When zero the project
	// inherits the organization's, which is what almost every project wants.
	SchemeID uuid.UUID
	// BoardType is what the project's first board should be, as the board
	// package names them. Projects do not interpret it; the provisioner does.
	BoardType string
	// Template is the key of the template this project is being made from, kept
	// on the project as a record of how it began.
	Template string
	// Features narrows what the project has; empty means everything its kind
	// allows.
	Features []Feature
}

// Create makes a project. The returned LSN lets the caller pin their own next
// read so the new project is visible immediately.
func (s *Service) Create(ctx context.Context, in CreateInput, actor uuid.UUID) (*Project, db.LSN, error) {
	if err := in.check(); err != nil {
		return nil, 0, err
	}
	var p *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		p, err = s.CreateIn(ctx, tx, in, actor)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return p, lsn, nil
}

// check validates the input and fills the defaults, so that both ways of
// creating a project apply the same rules.
func (in *CreateInput) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("a project needs a name")
	}

	in.Key = NormalizeKey(in.Key)
	if in.Key == "" {
		in.Key = SuggestKey(in.Name)
	}
	if !ValidKey(in.Key) {
		return fmt.Errorf("%q is not a usable project key: use 2 to 10 letters or digits, starting with a letter", in.Key)
	}

	if in.Kind == "" {
		in.Kind = KindSoftware
	}
	if !in.Kind.Valid() {
		return fmt.Errorf("%q is not a project kind", in.Kind)
	}
	if len(in.Features) == 0 {
		in.Features = DefaultFeatures(in.Kind)
	}
	for _, f := range in.Features {
		if !f.Valid() {
			return fmt.Errorf("%w: %q", ErrBadFeature, f)
		}
		if f.DeskOnly() && in.Kind != KindService {
			return fmt.Errorf("%w: only a service desk has %s", ErrBadFeature, f.Word())
		}
	}
	return nil
}

// CreateIn makes a project inside the caller's transaction, so that whatever
// else the project needs, such as a workflow a template brings with it, commits
// together with it or not at all.
func (s *Service) CreateIn(ctx context.Context, tx db.DBTX, in CreateInput, actor uuid.UUID) (*Project, error) {
	if err := in.check(); err != nil {
		return nil, err
	}

	// A null scheme is not a missing one: it means the project takes the
	// organization's, which is what almost every project should do.
	var schemeID *uuid.UUID
	if in.SchemeID != uuid.Nil {
		schemeID = &in.SchemeID
	}

	var (
		p        Project
		features []string
	)
	err := tx.QueryRow(ctx, `
		INSERT INTO project (org_id, key, name, description, kind, lead_id, workflow_scheme_id, template, features)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, key, name, description, kind, lead_id, workflow_scheme_id, template, portal_verifies, trusted_domains, features, created_at, updated_at`,
		in.Key, in.Name, strings.TrimSpace(in.Description), string(in.Kind), in.LeadID, schemeID, in.Template, featureNames(in.Features),
	).Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Kind, &p.LeadID, &p.WorkflowSchemeID, &p.Template, &p.PortalVerifies, &p.TrustedDomains, &features, &p.CreatedAt, &p.UpdatedAt)
	if isUniqueViolation(err) {
		return nil, ErrKeyTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	p.Features = parseFeatures(features)

	if len(s.provisioners) > 0 {
		statuses, err := s.workflows.ProjectStatuses(ctx, tx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, provisioner := range s.provisioners {
			if err := provisioner.ProvisionProject(ctx, tx, &p, in, statuses); err != nil {
				return nil, err
			}
		}
	}

	err = events.EmitInTenant(ctx, tx, "project.created", map[string]any{
		"projectId": p.ID,
		"key":       p.Key,
		"name":      p.Name,
		"template":  in.Template,
		"actorId":   actor,
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// selectProject is the shared projection. The issue counts are subqueries
// rather than joins so that a project with no issues still comes back.
const selectProject = `
SELECT p.id, p.key, p.name, p.description, p.kind, p.lead_id,
       COALESCE(u.name, ''), p.workflow_scheme_id, p.template,
       (SELECT count(*) FROM issue i WHERE i.project_id = p.id),
       (SELECT count(*) FROM issue i
          JOIN issue_status st ON st.id = i.status_id
         WHERE i.project_id = p.id AND st.category <> 'done'),
       p.created_at, p.updated_at, p.archived_at, p.portal_verifies, p.trusted_domains, p.features,
       s.id, s.status, s.note, to_char(s.target_on, 'YYYY-MM-DD'), s.author_id, COALESCE(su.name, ''), s.created_at
FROM project p
LEFT JOIN app_user u ON u.id = p.lead_id
LEFT JOIN LATERAL (
    SELECT id, status, note, target_on, author_id, created_at
    FROM project_status_update WHERE project_id = p.id
    ORDER BY created_at DESC, id DESC LIMIT 1
) s ON true
LEFT JOIN app_user su ON su.id = s.author_id`

func scanProject(row pgx.Row) (*Project, error) {
	var (
		p          Project
		statusID   *uuid.UUID
		health     *string
		note       *string
		target     *string
		author     *uuid.UUID
		authorName string
		postedAt   *time.Time
		features   []string
	)
	err := row.Scan(
		&p.ID, &p.Key, &p.Name, &p.Description, &p.Kind, &p.LeadID,
		&p.LeadName, &p.WorkflowSchemeID, &p.Template,
		&p.IssueCount, &p.OpenIssueCount,
		&p.CreatedAt, &p.UpdatedAt, &p.ArchivedAt, &p.PortalVerifies, &p.TrustedDomains, &features,
		&statusID, &health, &note, &target, &author, &authorName, &postedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.TrustedDomains == nil {
		p.TrustedDomains = []string{}
	}
	p.Features = parseFeatures(features)
	if statusID != nil && health != nil && postedAt != nil {
		p.Status = &StatusUpdate{ID: *statusID, ProjectKey: p.Key, Status: Health(*health), Note: *note, TargetOn: target, AuthorID: author, AuthorName: authorName, CreatedAt: *postedAt}
	}
	return &p, nil
}

// List returns the organization's projects, archived ones only when asked for.
func (s *Service) List(ctx context.Context, includeArchived bool) ([]Project, error) {
	var out []Project
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		query := selectProject
		if !includeArchived {
			query += ` WHERE p.archived_at IS NULL`
		}
		query += ` ORDER BY p.key`

		rows, err := tx.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			p, err := scanProject(rows)
			if err != nil {
				return err
			}
			out = append(out, *p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return out, nil
}

// ByKey reads one project by its key, which is what appears in URLs.
func (s *Service) ByKey(ctx context.Context, key string) (*Project, error) {
	var p *Project
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		p, err = scanProject(tx.QueryRow(ctx, selectProject+` WHERE p.key = $1`, NormalizeKey(key)))
		return err
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// UpdateInput carries the fields an update may change. A nil field is left
// alone, which is what distinguishes "not mentioned" from "set to empty".
type UpdateInput struct {
	Name           *string
	Description    *string
	LeadID         **uuid.UUID
	Kind           *Kind
	PortalVerifies *bool
	TrustedDomains *[]string
	Features       *[]string
}

// Update changes a project's details. The key is deliberately not changeable:
// it is embedded in every issue key, every link and every bookmark.
func (s *Service) Update(ctx context.Context, key string, in UpdateInput) (*Project, db.LSN, error) {
	var p *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			id         uuid.UUID
			archivedAt *string
		)
		err := tx.QueryRow(ctx, `SELECT id, archived_at::text FROM project WHERE key = $1`, NormalizeKey(key)).
			Scan(&id, &archivedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if archivedAt != nil {
			return ErrArchived
		}

		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			if name == "" {
				return errors.New("a project needs a name")
			}
			if _, err := tx.Exec(ctx, `UPDATE project SET name = $2 WHERE id = $1`, id, name); err != nil {
				return err
			}
		}
		if in.Description != nil {
			if _, err := tx.Exec(ctx, `UPDATE project SET description = $2 WHERE id = $1`, id, strings.TrimSpace(*in.Description)); err != nil {
				return err
			}
		}
		if in.LeadID != nil {
			if _, err := tx.Exec(ctx, `UPDATE project SET lead_id = $2 WHERE id = $1`, id, *in.LeadID); err != nil {
				return err
			}
		}
		if in.Kind != nil {
			if !in.Kind.Valid() {
				return fmt.Errorf("%q is not a project kind", *in.Kind)
			}
			if _, err := tx.Exec(ctx, `UPDATE project SET kind = $2 WHERE id = $1`, id, string(*in.Kind)); err != nil {
				return err
			}
		}
		if in.PortalVerifies != nil {
			tag, err := tx.Exec(ctx, `UPDATE project SET portal_verifies = $2 WHERE id = $1 AND ($2 OR kind = 'service')`, id, *in.PortalVerifies)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotADesk
			}
			// Turning the code back on ends every session that skipped it.
			if *in.PortalVerifies {
				if _, err := tx.Exec(ctx, `DELETE FROM user_session WHERE portal_project_id = $1`, id); err != nil {
					return err
				}
			}
		}
		if in.TrustedDomains != nil {
			domains, err := NormalizeDomains(*in.TrustedDomains)
			if err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `UPDATE project SET trusted_domains = $2 WHERE id = $1 AND (cardinality($2::text[]) = 0 OR kind = 'service')`, id, domains)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotADesk
			}
			// Narrowing the list ends the code-less sessions it no longer
			// covers, as closing the door does.
			if _, err := tx.Exec(ctx, `
				DELETE FROM user_session s USING app_user u
				WHERE s.user_id = u.id AND s.portal_project_id = $1
				  AND cardinality($2::text[]) > 0
				  AND NOT lower(split_part(u.email::text, '@', 2)) = ANY($2)`, id, domains); err != nil {
				return err
			}
		}

		if in.Features != nil {
			features, err := NormalizeFeatures(*in.Features)
			if err != nil {
				return err
			}
			// A desk page on anything but a desk is refused as the door is.
			tag, err := tx.Exec(ctx, `UPDATE project SET features = $2 WHERE id = $1 AND (NOT ($2::text[] && '{queues,desk}') OR kind = 'service')`, id, featureNames(features))
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotADesk
			}
		}

		p, err = scanProject(tx.QueryRow(ctx, selectProject+` WHERE p.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return p, lsn, nil
}

// SetWorkflowScheme gives a project a scheme of its own, or takes it away so
// the project follows the organization again.
//
// A scheme it names need only cover the issue types the project disagrees
// about; everything it leaves out still falls through to the organization.
func (s *Service) SetWorkflowScheme(ctx context.Context, key string, schemeID *uuid.UUID, actor uuid.UUID) (*Project, db.LSN, error) {
	var p *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if schemeID != nil {
			var exists bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM workflow_scheme WHERE id = $1)`, *schemeID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: no such workflow scheme", ErrNotFound)
			}
		}

		tag, err := tx.Exec(ctx, `
			UPDATE project SET workflow_scheme_id = $2
			WHERE key = $1 AND archived_at IS NULL`, NormalizeKey(key), schemeID)
		if err != nil {
			return fmt.Errorf("set the project's workflow scheme: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		p, err = scanProject(tx.QueryRow(ctx, selectProject+` WHERE p.key = $1`, NormalizeKey(key)))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "project.workflow_scheme_changed", map[string]any{
			"projectId": p.ID, "key": p.Key, "schemeId": schemeID, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return p, lsn, nil
}

// Archive hides a project without deleting anything. Issues, history and links
// are all still there, which is what makes it recoverable and what makes it the
// right default instead of a delete.
func (s *Service) Archive(ctx context.Context, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE project SET archived_at = now()
			WHERE key = $1 AND archived_at IS NULL`, NormalizeKey(key))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return events.EmitInTenant(ctx, tx, "project.archived", map[string]any{"key": NormalizeKey(key)})
	})
}

// Restore brings an archived project back.
func (s *Service) Restore(ctx context.Context, key string) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE project SET archived_at = NULL
			WHERE key = $1 AND archived_at IS NOT NULL`, NormalizeKey(key))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// KeyAvailable reports whether a key can still be taken, so the creation form
// can say so before the user submits.
func (s *Service) KeyAvailable(ctx context.Context, key string) (bool, error) {
	key = NormalizeKey(key)
	if !ValidKey(key) {
		return false, nil
	}
	var exists bool
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM project WHERE key = $1)`, key).Scan(&exists)
	})
	return !exists, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// SetWorkflowAssignment decides which workflow one issue type follows in a
// project, or hands that type back to the organization with a nil workflow.
func (s *Service) SetWorkflowAssignment(ctx context.Context, key string, issueTypeID uuid.UUID, workflowID *uuid.UUID, actor uuid.UUID) (*Project, db.LSN, error) {
	var p *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanProject(tx.QueryRow(ctx, selectProject+` WHERE p.key = $1 AND p.archived_at IS NULL`, NormalizeKey(key)))
		if err != nil {
			return err
		}
		var typeExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_type WHERE id = $1)`, issueTypeID).Scan(&typeExists); err != nil {
			return err
		}
		if !typeExists {
			return fmt.Errorf("%w: this organization has no such issue type.", workflow.ErrInvalid)
		}

		// The scheme is bookkeeping: a project needs one of its own to disagree in.
		schemeID, created, err := workflow.EnsureOwnScheme(ctx, tx, current.ID, current.Name, current.Key, current.WorkflowSchemeID)
		if err != nil {
			return err
		}
		if err := workflow.SetSchemeItem(ctx, tx, schemeID, issueTypeID, workflowID); err != nil {
			return err
		}

		next := &schemeID
		empty, err := workflow.SchemeIsEmpty(ctx, tx, schemeID)
		if err != nil {
			return err
		}
		if empty {
			// A project that disagrees about nothing follows the organization
			// again, and the scheme that said nothing goes with it.
			next = nil
			if _, err := tx.Exec(ctx, `UPDATE project SET workflow_scheme_id = NULL WHERE id = $1`, current.ID); err != nil {
				return fmt.Errorf("hand the decision back: %w", err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM workflow_scheme WHERE id = $1`, schemeID); err != nil {
				return fmt.Errorf("drop the empty scheme: %w", err)
			}
		} else if created {
			if _, err := tx.Exec(ctx, `UPDATE project SET workflow_scheme_id = $2 WHERE id = $1`, current.ID, schemeID); err != nil {
				return fmt.Errorf("set the project's workflow scheme: %w", err)
			}
		}

		p, err = scanProject(tx.QueryRow(ctx, selectProject+` WHERE p.key = $1`, current.Key))
		if err != nil {
			return err
		}
		if err := events.EmitInTenant(ctx, tx, "project.workflow_assignment_changed", map[string]any{
			"projectId": p.ID, "key": p.Key, "issueTypeId": issueTypeID, "workflowId": workflowID, "schemeId": next, "actorId": actor,
		}); err != nil {
			return err
		}
		if (current.WorkflowSchemeID == nil) != (next == nil) || (next != nil && current.WorkflowSchemeID != nil && *next != *current.WorkflowSchemeID) {
			return events.EmitInTenant(ctx, tx, "project.workflow_scheme_changed", map[string]any{
				"projectId": p.ID, "key": p.Key, "schemeId": next, "actorId": actor,
			})
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return p, lsn, nil
}
