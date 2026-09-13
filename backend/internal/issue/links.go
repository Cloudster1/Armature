package issue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/project"
)

// LinkTypeRef is one kind of relationship, with the wording for each end.
type LinkTypeRef struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Outward string    `json:"outward"`
	Inward  string    `json:"inward"`
}

// LinkTypeBlocks is the relationship the plan reads as a dependency. The rest
// are relationships between issues and say nothing about order.
const LinkTypeBlocks = "Blocks"

// Blocker is one dependency: the blocker must finish before the blocked issue
// starts, which is the only link type a schedule can be wrong about.
type Blocker struct {
	// LinkID is the link itself, so the dependency can be undone where it is seen.
	LinkID     uuid.UUID `json:"linkId"`
	BlockerKey string    `json:"blockerKey"`
	BlockedKey string    `json:"blockedKey"`
}

// ListLinkTypes returns the relationships this organization can express.
func (s *Service) ListLinkTypes(ctx context.Context) ([]LinkTypeRef, error) {
	out := []LinkTypeRef{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id, name, outward, inward FROM issue_link_type ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t LinkTypeRef
			if err := rows.Scan(&t.ID, &t.Name, &t.Outward, &t.Inward); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list link types: %w", err)
	}
	return out, nil
}

// LinkInput describes a relationship to record.
type LinkInput struct {
	// TypeName is matched case-insensitively, so a client can say "blocks".
	TypeName string
	TypeID   uuid.UUID
	// TargetKey is the issue at the other end, read in the outward direction:
	// this issue <phrase> that one.
	TargetKey string
}

// AddLink records a relationship between two issues.
func (s *Service) AddLink(ctx context.Context, key string, in LinkInput, actor Actor) (*Link, db.LSN, error) {
	sourceProject, sourceNum, err := ParseKey(key)
	if err != nil {
		return nil, 0, err
	}
	targetProject, targetNum, err := ParseKey(in.TargetKey)
	if err != nil {
		return nil, 0, err
	}

	var created *Link
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		source, err := issueIDOf(ctx, tx, sourceProject, sourceNum)
		if err != nil {
			return err
		}
		target, err := issueIDOf(ctx, tx, targetProject, targetNum)
		if err != nil {
			return err
		}
		if source == target {
			return ErrLinkToSelf
		}

		linkType, err := resolveLinkType(ctx, tx, in)
		if err != nil {
			return err
		}
		if strings.EqualFold(linkType.Name, LinkTypeBlocks) {
			if err := refuseBlockCycle(ctx, tx, linkType.ID, source, target); err != nil {
				return err
			}
		}

		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO issue_link (org_id, link_type_id, source_id, target_id)
			VALUES (current_org_id(), $1, $2, $3) RETURNING id`,
			linkType.ID, source, target).Scan(&id)
		if isUniqueViolation(err) {
			// The unordered index means "A blocks B" and "B blocks A" are the
			// same row, so a repeat is not an error worth raising.
			return errors.New("those issues are already linked that way")
		}
		if err != nil {
			return fmt.Errorf("link issues: %w", err)
		}

		created, err = readLink(ctx, tx, id, source)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": source,
			"key":     strings.ToUpper(key),
			"changes": []Change{{Field: "link", To: linkType.Outward + " " + created.Issue.Key}},
			"actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// RemoveLink deletes a relationship from either end, and says so on the source
// issue's stream. It is removed from an issue, so it has to touch that one.
func (s *Service) RemoveLink(ctx context.Context, from string, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var source uuid.UUID
		var sourceKey string
		err := tx.QueryRow(ctx, `
			SELECT l.source_id, p.key || '-' || i.key_num
			FROM issue_link l JOIN issue i ON i.id = l.source_id JOIN project p ON p.id = i.project_id
			WHERE l.id = $1
			  AND EXISTS (SELECT 1 FROM issue e JOIN project ep ON ep.id = e.project_id
			              WHERE e.id IN (l.source_id, l.target_id)
			                AND upper(ep.key || '-' || e.key_num) = upper($2))`, id, from).Scan(&source, &sourceKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkNotFound
		}
		if err != nil {
			return err
		}
		removed, err := readLink(ctx, tx, id, source)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM issue_link WHERE id = $1`, id); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": source,
			"key":     sourceKey,
			"changes": []Change{{Field: "link", From: removed.Phrase + " " + removed.Issue.Key}},
		})
	})
}

// maxBlockDepth bounds the walk along blocks links; a chain longer than this
// is not a plan anybody follows.
const maxBlockDepth = 32

// refuseBlockCycle walks the blocks links outward from the new link's target
// and refuses when they reach its source, naming the way round. A trigger
// refuses the same row, so a link written any other way cannot slip through.
func refuseBlockCycle(ctx context.Context, tx db.DBTX, typeID, source, target uuid.UUID) error {
	var path []uuid.UUID
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE downstream AS (
			SELECT l.target_id, ARRAY[l.target_id] AS path
			FROM issue_link l
			WHERE l.link_type_id = $1 AND l.source_id = $2
			UNION ALL
			SELECT l.target_id, d.path || l.target_id
			FROM issue_link l
			JOIN downstream d ON l.source_id = d.target_id
			WHERE l.link_type_id = $1 AND array_length(d.path, 1) < $4 AND NOT l.target_id = ANY (d.path)
		)
		SELECT path FROM downstream WHERE target_id = $3 ORDER BY array_length(path, 1) LIMIT 1`,
		typeID, target, source, maxBlockDepth).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("walk the blocks links: %w", err)
	}
	// The way round: the target, the issues between, and the source last.
	keys, err := keysByID(ctx, tx, append([]uuid.UUID{target}, path...))
	if err != nil {
		return err
	}
	targetKey, sourceKey := keys[0], keys[len(keys)-1]
	through := ""
	if between := keys[1 : len(keys)-1]; len(between) > 0 {
		through = " through " + strings.Join(between, ", ")
	}
	return levelError{ErrLinkCycle, fmt.Sprintf("%s already blocks %s%s, so %s cannot block %s.", targetKey, sourceKey, through, sourceKey, targetKey)}
}

// keysByID names issues by id, in the order given.
func keysByID(ctx context.Context, tx db.DBTX, ids []uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.key || '-' || i.key_num
		FROM unnest($1::uuid[]) WITH ORDINALITY AS wanted(id, position)
		JOIN issue i ON i.id = wanted.id
		JOIN project p ON p.id = i.project_id
		ORDER BY wanted.position`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// Links returns an issue's relationships, seen from that issue's end.
func (s *Service) Links(ctx context.Context, key string) ([]Link, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	out := []Link{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, err := issueIDOf(ctx, tx, projectKey, num)
		if err != nil {
			return err
		}
		out, err = readLinksFor(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Blockers returns the blocking dependencies inside one project.
func (s *Service) Blockers(ctx context.Context, projectKey string) ([]Blocker, error) {
	out := []Blocker{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT l.id, sp.key || '-' || si.key_num, tp.key || '-' || ti.key_num
			FROM issue_link l
			JOIN issue_link_type lt ON lt.id = l.link_type_id
			JOIN issue si ON si.id = l.source_id
			JOIN project sp ON sp.id = si.project_id
			JOIN issue ti ON ti.id = l.target_id
			JOIN project tp ON tp.id = ti.project_id
			WHERE lower(lt.name) = lower($1) AND (sp.key = $2 OR tp.key = $2)
			ORDER BY si.key_num, ti.key_num`, LinkTypeBlocks, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Blocker
			if err := rows.Scan(&b.LinkID, &b.BlockerKey, &b.BlockedKey); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the project's dependencies: %w", err)
	}
	return out, nil
}

// resolveLinkType finds the requested relationship by id or by name.
func resolveLinkType(ctx context.Context, tx db.DBTX, in LinkInput) (*LinkTypeRef, error) {
	var t LinkTypeRef
	var err error
	if in.TypeID != uuid.Nil {
		err = tx.QueryRow(ctx, `
			SELECT id, name, outward, inward FROM issue_link_type WHERE id = $1`, in.TypeID).
			Scan(&t.ID, &t.Name, &t.Outward, &t.Inward)
	} else {
		name := strings.TrimSpace(in.TypeName)
		if name == "" {
			name = LinkTypeBlocks
		}
		err = tx.QueryRow(ctx, `
			SELECT id, name, outward, inward FROM issue_link_type WHERE lower(name) = lower($1)`, name).
			Scan(&t.ID, &t.Name, &t.Outward, &t.Inward)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("there is no such kind of link")
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// readLinksFor reads both directions at once, phrasing each from the point of
// view of the issue that was asked about.
func readLinksFor(ctx context.Context, tx db.DBTX, id uuid.UUID) ([]Link, error) {
	rows, err := tx.Query(ctx, `
		SELECT l.id,
		       CASE WHEN l.source_id = $1 THEN 'outward' ELSE 'inward' END,
		       lt.id, lt.name,
		       CASE WHEN l.source_id = $1 THEN lt.outward ELSE lt.inward END,
		       CASE WHEN l.source_id = $1 THEN l.target_id ELSE l.source_id END
		FROM issue_link l
		JOIN issue_link_type lt ON lt.id = l.link_type_id
		WHERE l.source_id = $1 OR l.target_id = $1
		ORDER BY lt.name, l.created_at`, id)
	if err != nil {
		return nil, fmt.Errorf("read links: %w", err)
	}
	defer rows.Close()

	type row struct {
		link  Link
		other uuid.UUID
	}
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.link.ID, &r.link.Direction, &r.link.TypeID, &r.link.TypeName,
			&r.link.Phrase, &r.other); err != nil {
			return nil, err
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return []Link{}, nil
	}

	ids := make([]uuid.UUID, len(found))
	for i, r := range found {
		ids[i] = r.other
	}
	others, err := readIssues(ctx, tx, selectIssue+` WHERE i.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]Issue, len(others))
	for _, o := range others {
		byID[o.ID] = o
	}

	out := make([]Link, 0, len(found))
	for _, r := range found {
		other, ok := byID[r.other]
		if !ok {
			continue
		}
		r.link.Issue = other
		out = append(out, r.link)
	}
	return out, nil
}

// readLink reads one link back from the end that created it.
func readLink(ctx context.Context, tx db.DBTX, id, from uuid.UUID) (*Link, error) {
	links, err := readLinksFor(ctx, tx, from)
	if err != nil {
		return nil, err
	}
	for i := range links {
		if links[i].ID == id {
			return &links[i], nil
		}
	}
	return nil, ErrLinkNotFound
}

// issueIDOf resolves a project key and number to an id inside this tenant.
func issueIDOf(ctx context.Context, tx db.DBTX, projectKey string, num int64) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id
		WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("%w: %s", ErrNotFound, FormatKey(projectKey, num))
	}
	return id, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
