package issue

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Levels count from the ordinary working level at zero, so a new level above
// the current top never renumbers what is already underneath it.
const (
	LevelSubtask    = -1
	LevelStandard   = 0
	LevelEpic       = 1
	LevelInitiative = 2

	// MaxLevel matches the range the database will accept.
	MaxLevel = 3
)

// CanParent reports whether an issue at parentLevel may hold one at childLevel.
// Exactly one level, so a roll-up never has to ask how many levels were skipped.
func CanParent(parentLevel, childLevel int) bool {
	return parentLevel == childLevel+1
}

// NeedsParent reports whether a type at this level can stand on its own: a
// subtask with nothing above it is an issue that has lost its context.
func NeedsParent(level int) bool { return level < LevelStandard }

// levelError carries a sentence written for the user while still matching its
// sentinel through errors.Is, which wrapping with %w cannot do without repeating.
type levelError struct {
	sentinel error
	message  string
}

func (e levelError) Error() string { return e.message }
func (e levelError) Unwrap() error { return e.sentinel }

// article picks "a" or "an" for a type name the user will read back.
func article(word string) string {
	word = strings.ToLower(word)
	if word == "" {
		return "a"
	}
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "an " + word
	}
	return "a " + word
}

// Progress counts direct children only: rolling in every descendant would make
// an epic's bar track how finely its stories happen to be broken down.
type Progress struct {
	Total      int `json:"total"`
	Done       int `json:"done"`
	InProgress int `json:"inProgress"`
	Todo       int `json:"todo"`
}

// Percent is the finished share, rounded down. An issue with no children is
// not zero percent done; it has nothing to report.
func (p Progress) Percent() int {
	if p.Total == 0 {
		return 0
	}
	return p.Done * 100 / p.Total
}

func (p *Progress) count(category workflow.StatusCategory) {
	p.Total++
	switch category {
	case workflow.CategoryDone:
		p.Done++
	case workflow.CategoryInProgress:
		p.InProgress++
	default:
		p.Todo++
	}
}

// Node is one issue in a tree, with whatever hangs off it.
type Node struct {
	Issue    Issue    `json:"issue"`
	Progress Progress `json:"progress"`
	Children []Node   `json:"children"`
}

// Hierarchy is one issue seen in context: what is above it, what is directly
// below it, and how that work is going.
type Hierarchy struct {
	// Ancestors runs from the top down to the issue's own parent.
	Ancestors []Issue  `json:"ancestors"`
	Issue     Issue    `json:"issue"`
	Children  []Node   `json:"children"`
	Progress  Progress `json:"progress"`
	// ChildTypes are the types a new child may take, so the client offers
	// exactly the choices the server would accept.
	ChildTypes []TypeOption `json:"childTypes"`
}

// TypeOption is an issue type offered in a picker.
type TypeOption struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Icon  string    `json:"icon"`
	Level int       `json:"level"`
}

// maxDepth bounds the recursive walks: a query that trusts the data to be well
// shaped is a query that hangs when it is not.
const maxDepth = 8

// parentCandidate is what the checks below need to know about a proposed parent.
type parentCandidate struct {
	ID        uuid.UUID
	Key       string
	Summary   string
	ProjectID uuid.UUID
	Type      TypeRef
}

// findParent resolves an issue key to a candidate parent, or reports that there
// is no such issue in this organization.
func findParent(ctx context.Context, tx db.DBTX, parentKey string) (*parentCandidate, error) {
	projectKey, num, err := ParseKey(parentKey)
	if err != nil {
		return nil, err
	}

	var c parentCandidate
	err = tx.QueryRow(ctx, `
		SELECT i.id, p.key || '-' || i.key_num, i.summary, i.project_id,
		       t.id, t.name, t.icon, t.hierarchy_level, t.is_subtask
		FROM issue i
		JOIN project p ON p.id = i.project_id
		JOIN issue_type t ON t.id = i.issue_type_id
		WHERE p.key = $1 AND i.key_num = $2`, projectKey, num,
	).Scan(&c.ID, &c.Key, &c.Summary, &c.ProjectID,
		&c.Type.ID, &c.Type.Name, &c.Type.Icon, &c.Type.Level, &c.Type.IsSubtask)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, strings.ToUpper(parentKey))
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// checkParent is the readable half of the tree rule; the database trigger is
// the half that makes it true no matter who writes.
func checkParent(parent *parentCandidate, childID, childProjectID uuid.UUID, childType TypeRef) error {
	if parent.ID == childID {
		return ErrParentIsSelf
	}
	if parent.ProjectID != childProjectID {
		return levelError{ErrParentOtherProject, parent.Key + " is in another project"}
	}
	if !CanParent(parent.Type.Level, childType.Level) {
		return levelError{ErrParentLevel, fmt.Sprintf("%s is %s, which cannot be the parent of %s",
			parent.Key, article(parent.Type.Name), article(childType.Name))}
	}
	return nil
}

// resolveParent validates the parent relationship for an issue being created.
func (s *Service) resolveParent(ctx context.Context, tx db.DBTX, parentKey string, projectID uuid.UUID, childType TypeRef) (*uuid.UUID, error) {
	if strings.TrimSpace(parentKey) == "" {
		if NeedsParent(childType.Level) {
			return nil, levelError{ErrNeedsParent, article(childType.Name) + " needs a parent issue"}
		}
		return nil, nil
	}

	parent, err := findParent(ctx, tx, parentKey)
	if err != nil {
		return nil, err
	}
	// The issue has no id yet, so uuid.Nil stands in for "nothing to collide with".
	if err := checkParent(parent, uuid.Nil, projectID, childType); err != nil {
		return nil, err
	}
	return &parent.ID, nil
}

// SetParent moves an issue underneath another one, or out from under the one it
// is beneath. Kept out of Update so it cannot take an unrelated edit down with it.
func (s *Service) SetParent(ctx context.Context, key string, parentKey *string, actor Actor) (*Issue, db.LSN, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, 0, err
	}

	var updated *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanIssue(tx.QueryRow(ctx,
			selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
		if err != nil {
			return err
		}

		var (
			newParentID *uuid.UUID
			toKey       = "None"
		)
		if parentKey != nil && strings.TrimSpace(*parentKey) != "" {
			parent, err := findParent(ctx, tx, *parentKey)
			if err != nil {
				return err
			}
			if err := checkParent(parent, before.ID, before.ProjectID, before.Type); err != nil {
				return err
			}
			// The level rule already makes a loop inexpressible, but only for
			// as long as levels are the only way in, and a loop hangs readers.
			if err := s.refuseCycle(ctx, tx, parent.ID, before.ID); err != nil {
				return err
			}
			newParentID, toKey = &parent.ID, parent.Key
		} else if NeedsParent(before.Type.Level) {
			return levelError{ErrNeedsParent, article(before.Type.Name) + " needs a parent issue"}
		}

		if equalUUID(before.ParentID, newParentID) {
			updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE issue SET parent_id = $2 WHERE id = $1`, before.ID, newParentID); err != nil {
			return err
		}

		fromKey := before.ParentKey
		if fromKey == "" {
			fromKey = "None"
		}
		changes := []Change{{Field: "parent", From: fromKey, To: toKey}}
		if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
			return err
		}

		updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
		if err != nil {
			return err
		}

		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": updated.ID,
			"key":     updated.Key,
			"changes": changes,
			"actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}

// refuseCycle walks up from start and fails if it reaches forbidden.
func (s *Service) refuseCycle(ctx context.Context, tx db.DBTX, start, forbidden uuid.UUID) error {
	var reaches bool
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT i.id, i.parent_id, 0 AS depth FROM issue i WHERE i.id = $1
			UNION ALL
			SELECT p.id, p.parent_id, up.depth + 1
			FROM issue p JOIN up ON p.id = up.parent_id
			WHERE up.depth < $3
		)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = $2)`, start, forbidden, maxDepth).Scan(&reaches)
	if err != nil {
		return err
	}
	if reaches {
		return levelError{ErrParentIsSelf, "that would put the issue underneath itself"}
	}
	return nil
}

// Children returns the issues directly underneath one.
func (s *Service) Children(ctx context.Context, key string) ([]Issue, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	out := []Issue{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var parentID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&parentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out, err = readIssues(ctx, tx, selectIssue+` WHERE i.parent_id = $1 ORDER BY i.key_num`, parentID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Hierarchy returns one issue in context: ancestors, direct children with their
// own roll-ups, and the types a new child of it could take.
func (s *Service) Hierarchy(ctx context.Context, key string) (*Hierarchy, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	out := &Hierarchy{Ancestors: []Issue{}, Children: []Node{}, ChildTypes: []TypeOption{}}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanIssue(tx.QueryRow(ctx,
			selectIssue+` WHERE p.key = $1 AND i.key_num = $2`, projectKey, num))
		if err != nil {
			return err
		}
		out.Issue = *found

		if out.Ancestors, err = ancestorsOf(ctx, tx, found.ID); err != nil {
			return err
		}

		children, err := readIssues(ctx, tx, selectIssue+` WHERE i.parent_id = $1 ORDER BY i.key_num`, found.ID)
		if err != nil {
			return err
		}

		// One query for every grandchild count, rather than one per child.
		ids := make([]uuid.UUID, len(children))
		for i, c := range children {
			ids[i] = c.ID
		}
		grandchildren, err := progressFor(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, c := range children {
			out.Progress.count(c.Status.Category)
			out.Children = append(out.Children, Node{Issue: c, Progress: grandchildren[c.ID], Children: []Node{}})
		}

		out.ChildTypes, err = typesAtLevel(ctx, tx, found.Type.Level-1)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Tree returns a project's whole hierarchy, read in one query and assembled in
// memory rather than walked level by level over the network.
func (s *Service) Tree(ctx context.Context, projectKey string) ([]Node, error) {
	var issues []Issue
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		issues, err = readIssues(ctx, tx,
			selectIssue+` WHERE p.key = $1 ORDER BY it.hierarchy_level DESC, i.key_num LIMIT 2000`,
			project.NormalizeKey(projectKey))
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		// An empty project and a missing one are different answers, and only
		// the database knows which this is.
		if err := s.requireProject(ctx, projectKey); err != nil {
			return nil, err
		}
	}
	return BuildForest(issues), nil
}

func (s *Service) requireProject(ctx context.Context, projectKey string) error {
	return s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`,
			project.NormalizeKey(projectKey)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		return err
	})
}

// BuildForest arranges a flat list into the trees it describes. An issue whose
// parent is absent becomes a root rather than being dropped.
func BuildForest(issues []Issue) []Node {
	nodes := make(map[uuid.UUID]*Node, len(issues))
	order := make([]uuid.UUID, 0, len(issues))
	for _, i := range issues {
		nodes[i.ID] = &Node{Issue: i, Children: []Node{}}
		order = append(order, i.ID)
	}

	// A second pass, so a child listed before its parent still finds it.
	childOf := make(map[uuid.UUID][]uuid.UUID, len(issues))
	roots := make([]uuid.UUID, 0, len(issues))
	for _, id := range order {
		parentID := nodes[id].Issue.ParentID
		if parentID != nil && nodes[*parentID] != nil {
			childOf[*parentID] = append(childOf[*parentID], id)
			continue
		}
		roots = append(roots, id)
	}

	var assemble func(id uuid.UUID) Node
	assemble = func(id uuid.UUID) Node {
		n := *nodes[id]
		for _, childID := range childOf[id] {
			child := assemble(childID)
			n.Progress.count(child.Issue.Status.Category)
			n.Children = append(n.Children, child)
		}
		sortNodes(n.Children)
		return n
	}

	out := make([]Node, 0, len(roots))
	for _, id := range roots {
		out = append(out, assemble(id))
	}
	sortNodes(out)
	return out
}

// sortNodes puts higher levels first, then orders by issue number, so a tree
// reads the same way twice.
func sortNodes(nodes []Node) {
	sort.SliceStable(nodes, func(a, b int) bool {
		if nodes[a].Issue.Type.Level != nodes[b].Issue.Type.Level {
			return nodes[a].Issue.Type.Level > nodes[b].Issue.Type.Level
		}
		return keyOrder(nodes[a].Issue.Key) < keyOrder(nodes[b].Issue.Key)
	})
}

// keyOrder is the number in an issue key. Comparing the keys as text would put
// P-10 before P-2, which is only correct if you read them as words.
func keyOrder(key string) int64 {
	_, num, err := ParseKey(key)
	if err != nil {
		return 0
	}
	return num
}

// ancestorsOf walks up from an issue, returning the chain from the top down.
func ancestorsOf(ctx context.Context, tx db.DBTX, id uuid.UUID) ([]Issue, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE up AS (
			SELECT i.id, i.parent_id, 0 AS depth FROM issue i WHERE i.id = $1
			UNION ALL
			SELECT p.id, p.parent_id, up.depth + 1
			FROM issue p JOIN up ON p.id = up.parent_id
			WHERE up.depth < $2
		)
		SELECT id FROM up WHERE depth > 0 ORDER BY depth DESC`, id, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("walk up the hierarchy: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var ancestorID uuid.UUID
		if err := rows.Scan(&ancestorID); err != nil {
			return nil, err
		}
		ids = append(ids, ancestorID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []Issue{}, nil
	}

	found, err := readIssues(ctx, tx, selectIssue+` WHERE i.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	// The walk knows the order; the projection does not preserve it.
	byID := make(map[uuid.UUID]Issue, len(found))
	for _, f := range found {
		byID[f.ID] = f
	}
	out := make([]Issue, 0, len(ids))
	for _, ancestorID := range ids {
		if f, ok := byID[ancestorID]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}

// progressFor counts the direct children of many issues at once.
func progressFor(ctx context.Context, tx db.DBTX, ids []uuid.UUID) (map[uuid.UUID]Progress, error) {
	out := map[uuid.UUID]Progress{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT i.parent_id,
		       count(*),
		       count(*) FILTER (WHERE st.category = 'done'),
		       count(*) FILTER (WHERE st.category = 'in_progress'),
		       count(*) FILTER (WHERE st.category = 'todo')
		FROM issue i
		JOIN issue_status st ON st.id = i.status_id
		WHERE i.parent_id = ANY($1)
		GROUP BY i.parent_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("roll up child progress: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			parentID uuid.UUID
			p        Progress
		)
		if err := rows.Scan(&parentID, &p.Total, &p.Done, &p.InProgress, &p.Todo); err != nil {
			return nil, err
		}
		out[parentID] = p
	}
	return out, rows.Err()
}

// typesAtLevel lists the issue types available at one level of the hierarchy.
func typesAtLevel(ctx context.Context, tx db.DBTX, level int) ([]TypeOption, error) {
	out := []TypeOption{}
	if level < LevelSubtask || level > MaxLevel {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id, name, icon, hierarchy_level FROM issue_type
		WHERE hierarchy_level = $1 ORDER BY position, name`, level)
	if err != nil {
		return nil, fmt.Errorf("list the types at level %d: %w", level, err)
	}
	defer rows.Close()

	for rows.Next() {
		var t TypeOption
		if err := rows.Scan(&t.ID, &t.Name, &t.Icon, &t.Level); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// readIssues runs one of the shared projections and scans it into a slice.
func readIssues(ctx context.Context, tx db.DBTX, query string, args ...any) ([]Issue, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}
