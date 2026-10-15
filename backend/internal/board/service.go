package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Service implements board use cases.
type Service struct {
	db     *db.Cluster
	issues *issue.Service
}

func NewService(cluster *db.Cluster, issues *issue.Service) *Service {
	return &Service{db: cluster, issues: issues}
}

// ForProject returns the board a project opens on: the first List names, which
// for most projects is its only one.
func (s *Service) ForProject(ctx context.Context, projectKey string) (*Board, error) {
	var b *Board
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		b, err = s.load(ctx, tx, selectBoard+` WHERE p.key = $1 ORDER BY `+boardOrder+` LIMIT 1`,
			project.NormalizeKey(projectKey))
		return err
	})
	return b, err
}

// ByID returns one board by id, which is how a project with several is read.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Board, error) {
	var b *Board
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		b, err = s.load(ctx, tx, selectBoard+` WHERE b.id = $1`, id)
		return err
	})
	return b, err
}

// List returns a project's boards without their cards, for choosing between.
//
// The board the project opens on comes first, then the rest of the project-wide
// boards by age, then the teams'. The client relies on the first being the one
// ForProject answers with; ordering by name would let a board called "Alpha"
// take the heading while the oldest board's cards were on screen.
func (s *Service) List(ctx context.Context, projectKey string) ([]Summary, error) {
	out := []Summary{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT b.id, b.name, b.description, b.type, b.team_id, COALESCE(t.name, ''),
			       (SELECT count(*) FROM board_swimlane l WHERE l.board_id = b.id)
			FROM board b
			JOIN project p ON p.id = b.project_id
			LEFT JOIN team t ON t.id = b.team_id
			WHERE p.key = $1
			ORDER BY `+boardOrder, project.NormalizeKey(projectKey))
		if err != nil {
			return fmt.Errorf("list boards: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var b Summary
			if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.Type, &b.TeamID, &b.TeamName, &b.Swimlanes); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// boardOrder is how List orders a project's boards and so which ForProject opens
// on; one clause for both, or the panel edits a board that is not on screen.
const boardOrder = `(b.team_id IS NOT NULL), b.created_at, b.name`

// selectBoard is the shared projection for reading one board.
const selectBoard = `
SELECT b.id, b.project_id, p.key, b.name, b.description, b.type, b.group_by,
       b.team_id, COALESCE(t.name, ''), b.done_for_days, b.created_at, b.updated_at
FROM board b
JOIN project p ON p.id = b.project_id
LEFT JOIN team t ON t.id = b.team_id`

// ForSprint returns one sprint's board: the board of the sprint's stream, its
// team's or the project's own, showing only what is committed to that sprint,
// running or not. A scrum board follows the running sprint on its own; this is
// the same board pointed at a chosen one, so every sprint can be tracked.
func (s *Service) ForSprint(ctx context.Context, sprintID uuid.UUID) (*Board, error) {
	var b *Board
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			projectID uuid.UUID
			teamID    *uuid.UUID
			ref       SprintRef
		)
		err := tx.QueryRow(ctx, `
			SELECT id, project_id, team_id, name, goal, ends_on FROM sprint WHERE id = $1`, sprintID,
		).Scan(&ref.ID, &projectID, &teamID, &ref.Name, &ref.Goal, &ref.EndsOn)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSprintNotFound
		}
		if err != nil {
			return fmt.Errorf("find the sprint: %w", err)
		}

		// The team's own board first, a scrum board over a kanban one, the
		// oldest otherwise; the project's board when the team has none.
		var boardID uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT id FROM board
			WHERE project_id = $1 AND (team_id IS NOT DISTINCT FROM $2 OR team_id IS NULL)
			ORDER BY (team_id IS NULL), (type <> 'scrum'), created_at
			LIMIT 1`, projectID, teamID).Scan(&boardID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find the sprint's board: %w", err)
		}

		b, err = s.loadShowing(ctx, tx, &ref, selectBoard+` WHERE b.id = $1`, boardID)
		return err
	})
	return b, err
}

// load reads a board and everything on it.
//
// Three queries: the board, its swimlanes with their statuses, and the cards.
// The cards are fetched in one go and distributed into swimlanes in memory,
// because a board is a few hundred cards at most and a query per swimlane
// would be a query per column on every render.
func (s *Service) load(ctx context.Context, tx db.DBTX, query string, args ...any) (*Board, error) {
	return s.loadShowing(ctx, tx, nil, query, args...)
}

// loadShowing reads a board pointed at one sprint, or at whatever its type
// says when the sprint is nil.
func (s *Service) loadShowing(ctx context.Context, tx db.DBTX, showing *SprintRef, query string, args ...any) (*Board, error) {
	var b Board
	err := tx.QueryRow(ctx, query, args...).Scan(
		&b.ID, &b.ProjectID, &b.ProjectKey, &b.Name, &b.Description, &b.Type, &b.GroupBy,
		&b.TeamID, &b.TeamName, &b.DoneForDays, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load board: %w", err)
	}

	// Empty rather than nil throughout: a nil slice marshals to null, and a
	// client that reasonably writes board.unmapped.length then crashes on a
	// board that simply has nothing in it.
	b.Swimlanes = []Swimlane{}
	b.Unmapped = []Card{}

	swimlanes, byStatus, err := s.loadSwimlanes(ctx, tx, b.ID)
	if err != nil {
		return nil, err
	}
	if swimlanes != nil {
		b.Swimlanes = swimlanes
	}

	scope := cardScope{projectID: b.ProjectID, teamID: b.TeamID}
	if showing != nil {
		// A sprint's board shows what is committed to the sprint, full stop.
		// The team filter is the board's way of finding its stream; the
		// sprint has already said whose work this is.
		b.Sprint = showing
		scope.teamID = nil
		scope.sprint, scope.sprintID = oneSprint, showing.ID
	} else if b.Type == TypeScrum {
		// A scrum board is the sprint that is running in its stream: its
		// team's, or the project's own. Between sprints it shows nothing,
		// because nothing has been committed to.
		running, err := runningSprint(ctx, tx, b.ProjectID, b.TeamID)
		if err != nil {
			return nil, err
		}
		if running == nil {
			return &b, nil
		}
		b.Sprint = running
		scope.sprint, scope.sprintID = oneSprint, running.ID
	} else {
		scope.doneForDays = b.DoneForDays
	}

	cards, err := s.loadCards(ctx, tx, scope, b.GroupBy)
	if err != nil {
		return nil, err
	}
	if scope.leavesDone() {
		if b.LeftOut, err = countLeftOut(ctx, tx, scope); err != nil {
			return nil, err
		}
		if b.LeftOut > 0 {
			if b.LeftOutQuery, err = leftOutQuery(ctx, tx, &b); err != nil {
				return nil, err
			}
		}
	}

	// Place each card in the swimlane that claims its status. Anything left
	// over is surfaced rather than dropped: a status nobody mapped is a
	// configuration mistake the user needs to see, not a disappearing card.
	position := map[uuid.UUID]int{}
	for i, lane := range b.Swimlanes {
		position[lane.ID] = i
	}
	for _, card := range cards {
		laneID, mapped := byStatus[card.Status.ID]
		if !mapped {
			b.Unmapped = append(b.Unmapped, card)
			continue
		}
		i := position[laneID]
		b.Swimlanes[i].Cards = append(b.Swimlanes[i].Cards, card)
	}

	return &b, nil
}

// Backlog is the unfinished work in a board's scope and in no sprint: a team's
// board draws its own team's work, the project's board everything.
func (s *Service) Backlog(ctx context.Context, boardID uuid.UUID) ([]Card, error) {
	var out []Card
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			projectID uuid.UUID
			teamID    *uuid.UUID
			groupBy   Grouping
		)
		err := tx.QueryRow(ctx, `SELECT project_id, team_id, group_by FROM board WHERE id = $1`, boardID).
			Scan(&projectID, &teamID, &groupBy)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		out, err = s.loadCards(ctx, tx, cardScope{projectID: projectID, teamID: teamID, sprint: noSprint}, groupBy)
		return err
	})
	if out == nil {
		out = []Card{}
	}
	return out, err
}

func (s *Service) loadSwimlanes(ctx context.Context, tx db.DBTX, boardID uuid.UUID) ([]Swimlane, map[uuid.UUID]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT l.id, l.name, l.position, l.wip_limit,
		       st.id, st.name, st.category, st.description, st.position
		FROM board_swimlane l
		LEFT JOIN board_swimlane_status ls ON ls.swimlane_id = l.id
		LEFT JOIN issue_status st ON st.id = ls.status_id
		WHERE l.board_id = $1
		ORDER BY l.position, l.name, st.position`, boardID)
	if err != nil {
		return nil, nil, fmt.Errorf("load swimlanes: %w", err)
	}
	defer rows.Close()

	var (
		lanes    []Swimlane
		index    = map[uuid.UUID]int{}
		byStatus = map[uuid.UUID]uuid.UUID{}
	)
	for rows.Next() {
		var (
			lane        Swimlane
			statusID    *uuid.UUID
			statusName  *string
			statusCat   *workflow.StatusCategory
			statusDesc  *string
			statusOrder *int
		)
		if err := rows.Scan(
			&lane.ID, &lane.Name, &lane.Position, &lane.WIPLimit,
			&statusID, &statusName, &statusCat, &statusDesc, &statusOrder,
		); err != nil {
			return nil, nil, err
		}

		i, seen := index[lane.ID]
		if !seen {
			lane.Cards = []Card{}
			lane.Statuses = []workflow.Status{}
			lanes = append(lanes, lane)
			i = len(lanes) - 1
			index[lane.ID] = i
		}

		if statusID != nil {
			lanes[i].Statuses = append(lanes[i].Statuses, workflow.Status{
				ID:          *statusID,
				Name:        *statusName,
				Category:    *statusCat,
				Description: *statusDesc,
				Position:    *statusOrder,
			})
			byStatus[*statusID] = lane.ID
		}
	}
	return lanes, byStatus, rows.Err()
}

// sprintFilter is how a reading of the cards treats sprints: a kanban board
// ignores them, a backlog wants what is in none, a scrum board wants one.
type sprintFilter int

const (
	anySprint sprintFilter = iota
	noSprint
	oneSprint
)

// cardScope is which of a project's issues one reading draws.
type cardScope struct {
	projectID uuid.UUID
	// teamID narrows to one team's work. Nil is the whole project, which is
	// what a project without teams has always had.
	teamID   *uuid.UUID
	sprint   sprintFilter
	sprintID uuid.UUID
	// doneForDays leaves out work finished longer ago than this; 0 keeps it all.
	doneForDays int
}

// leavesDone says whether the reading leaves finished work out, which only a
// kanban board does: a sprint's work is the sprint's, however old.
func (c cardScope) leavesDone() bool { return c.sprint == anySprint && c.doneForDays > 0 }

// where is the scope as a condition on issue i, its type t and status st, with
// the arguments it binds.
func (c cardScope) where() (string, []any) {
	args := []any{c.projectID, issue.LevelStandard}
	where := "i.project_id = $1 AND t.hierarchy_level >= $2"
	if c.teamID != nil {
		args = append(args, *c.teamID)
		where += fmt.Sprintf(" AND i.team_id = $%d", len(args))
	}
	switch c.sprint {
	case noSprint:
		// Done work in no sprint is history, not something to plan.
		where += " AND i.sprint_id IS NULL AND st.category <> 'done'"
	case oneSprint:
		args = append(args, c.sprintID)
		where += fmt.Sprintf(" AND i.sprint_id = $%d", len(args))
	}
	return where, args
}

// finishedBefore is done work resolved more than the bound days of 24 hours ago, the
// last change standing in when a workflow sets no resolved date, as on the plan.
func finishedBefore(param int) string {
	return fmt.Sprintf("(st.category = 'done' AND COALESCE(i.resolved_at, i.updated_at) < now() - make_interval(hours => $%d::int * 24))", param)
}

// countLeftOut counts the finished work a kanban board leaves out, so the
// board can say how much there is and where to find it.
func countLeftOut(ctx context.Context, tx db.DBTX, scope cardScope) (int, error) {
	where, args := scope.where()
	args = append(args, scope.doneForDays)
	var n int
	err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM issue i
		JOIN issue_type t ON t.id = i.issue_type_id
		JOIN issue_status st ON st.id = i.status_id
		WHERE `+where+` AND `+finishedBefore(len(args)), args...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count the finished work left out: %w", err)
	}
	return n, nil
}

// leftOutQuery is the search for the work a kanban board leaves out, written here
// so the rule lives in one place. It names the project's subtask types, never cards.
func leftOutQuery(ctx context.Context, tx db.DBTX, b *Board) (string, error) {
	days := fmt.Sprintf("-%dd", b.DoneForDays)
	q := "project = " + b.ProjectKey + " AND statusCategory = done AND (resolved < " + days +
		" OR (resolved IS EMPTY AND updated < " + days + "))"
	if b.TeamID != nil {
		q += " AND team = " + quoteNQL(b.TeamName)
	}
	rows, err := tx.Query(ctx, `
		SELECT t.name FROM issue_type t
		WHERE t.hierarchy_level < $1 AND EXISTS (SELECT 1 FROM issue i WHERE i.project_id = $2 AND i.issue_type_id = t.id)
		ORDER BY t.position, t.name`, issue.LevelStandard, b.ProjectID)
	if err != nil {
		return "", fmt.Errorf("list the subtask types: %w", err)
	}
	subtasks, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", fmt.Errorf("list the subtask types: %w", err)
	}
	if len(subtasks) > 0 {
		quoted := make([]string, len(subtasks))
		for i, name := range subtasks {
			quoted[i] = quoteNQL(name)
		}
		q += " AND type NOT IN (" + strings.Join(quoted, ", ") + ")"
	}
	return q, nil
}

// quoteNQL writes a name as a query string, whatever it holds.
func quoteNQL(name string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
}

// runningSprint finds the sprint a scrum board follows: the one running for its
// team, or for the project's own unassigned work when the board has no team.
// The unique index on (project, team) where active means there is at most one.
func runningSprint(ctx context.Context, tx db.DBTX, projectID uuid.UUID, teamID *uuid.UUID) (*SprintRef, error) {
	var ref SprintRef
	err := tx.QueryRow(ctx, `
		SELECT id, name, goal, ends_on FROM sprint
		WHERE project_id = $1 AND team_id IS NOT DISTINCT FROM $2 AND state = 'active'`,
		projectID, teamID).Scan(&ref.ID, &ref.Name, &ref.Goal, &ref.EndsOn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find the running sprint: %w", err)
	}
	return &ref, nil
}

// loadCards reads the project's work items, ordered by rank.
//
// The filter is the hierarchy level, not the presence of a parent: a story
// belonging to an epic is still a card, while a subtask moves with its parent.
func (s *Service) loadCards(ctx context.Context, tx db.DBTX, scope cardScope, groupBy Grouping) ([]Card, error) {
	where, args := scope.where()
	if scope.leavesDone() {
		args = append(args, scope.doneForDays)
		where += " AND NOT " + finishedBefore(len(args))
	}

	rows, err := tx.Query(ctx, `
		SELECT i.id, p.key || '-' || i.key_num, i.summary,
		       t.id, t.name, t.icon, t.hierarchy_level, t.is_subtask,
		       st.id, st.name, st.category, st.description, st.position,
		       i.priority, a.id, a.name, a.email, a.avatar_url,
		       COALESCE(pp.key || '-' || parent.key_num, ''),
		       parent.id, COALESCE(parent.summary, ''),
		       pt.id, COALESCE(pt.name, ''), COALESCE(pt.icon, ''), COALESCE(pt.hierarchy_level, 0),
		       i.rank, i.updated_at,
		       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', l.id, 'name', l.name, 'color', l.color) ORDER BY lower(l.name))
		                 FROM issue_label il JOIN label l ON l.id = il.label_id WHERE il.issue_id = i.id), '[]'::jsonb)
		FROM issue i
		JOIN project p ON p.id = i.project_id
		JOIN issue_type t ON t.id = i.issue_type_id
		JOIN issue_status st ON st.id = i.status_id
		LEFT JOIN app_user a ON a.id = i.assignee_id
		LEFT JOIN issue parent ON parent.id = i.parent_id
		LEFT JOIN issue_type pt ON pt.id = parent.issue_type_id
		LEFT JOIN project pp ON pp.id = parent.project_id
		WHERE `+where+`
		ORDER BY i.rank`, args...)
	if err != nil {
		return nil, fmt.Errorf("load cards: %w", err)
	}
	defer rows.Close()

	var out []Card
	for rows.Next() {
		var (
			c                          Card
			assigneeID                 *uuid.UUID
			assigneeName, assigneeMail *string
			assigneeAvatar             *string
			parentID, parentTypeID     *uuid.UUID
			parentSummary              string
			parentType                 issue.TypeRef
			labels                     []byte
		)
		if err := rows.Scan(
			&c.ID, &c.Key, &c.Summary,
			&c.Type.ID, &c.Type.Name, &c.Type.Icon, &c.Type.Level, &c.Type.IsSubtask,
			&c.Status.ID, &c.Status.Name, &c.Status.Category, &c.Status.Description, &c.Status.Position,
			&c.Priority, &assigneeID, &assigneeName, &assigneeMail, &assigneeAvatar,
			&c.ParentKey, &parentID, &parentSummary,
			&parentTypeID, &parentType.Name, &parentType.Icon, &parentType.Level,
			&c.Rank, &c.UpdatedAt, &labels,
		); err != nil {
			return nil, err
		}
		c.Labels = []issue.LabelRef{}
		if err := json.Unmarshal(labels, &c.Labels); err != nil {
			return nil, fmt.Errorf("decode card labels: %w", err)
		}
		if assigneeID != nil {
			c.Assignee = &issue.UserRef{ID: *assigneeID, Name: derefString(assigneeName), Email: derefString(assigneeMail), AvatarURL: derefString(assigneeAvatar)}
		}
		if parentID != nil && parentTypeID != nil {
			parentType.ID = *parentTypeID
			parentType.IsSubtask = parentType.Level < issue.LevelStandard
			c.Parent = &issue.ParentRef{ID: *parentID, Key: c.ParentKey, Summary: parentSummary, Type: parentType}
		}
		c.Group = groupValue(groupBy, c)
		out = append(out, c)
	}
	return out, rows.Err()
}

// groupValue is the row a card belongs to under the board's grouping.
func groupValue(groupBy Grouping, c Card) string {
	switch groupBy {
	case GroupAssignee:
		if c.Assignee == nil {
			return "Unassigned"
		}
		return c.Assignee.Name
	case GroupPriority:
		return string(c.Priority)
	case GroupType:
		return c.Type.Name
	default:
		return ""
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Provisioner gives every new project a board. It satisfies
// project.Provisioner, which is how a board comes into existence in the same
// transaction as the project without projects having to know boards exist.
type Provisioner struct{}

// ProvisionProject creates the project's first board, of the type the project
// was set up with. A project that names no type gets a kanban board, which is
// the one that works before anybody has planned a sprint.
func (Provisioner) ProvisionProject(ctx context.Context, tx db.DBTX, p *project.Project, in project.CreateInput, statuses []workflow.Status) error {
	kind := Type(in.BoardType)
	if kind == "" {
		kind = TypeKanban
	}
	if !kind.Valid() {
		return fmt.Errorf("%w: %q", ErrBadType, in.BoardType)
	}
	_, err := Create(ctx, tx, p.ID, p.Name+" board", kind, statuses)
	return err
}

// Create makes a board for a project, with one swimlane per status of the
// workflow the project's issue types follow.
//
// It runs in the caller's transaction so a project and its board are created
// together: a project whose board has to be assembled by hand before it can be
// used is a project that is not ready.
func Create(ctx context.Context, tx db.DBTX, projectID uuid.UUID, name string, kind Type, statuses []workflow.Status) (uuid.UUID, error) {
	return createBoard(ctx, tx, projectID, CreateInput{
		Name:        name,
		Description: describe(kind),
		Type:        kind,
	}, statuses)
}

// describe is the one line a board made for somebody carries until they change
// it, saying what the type means for what they will see.
func describe(kind Type) string {
	if kind == TypeScrum {
		return "Shows the sprint that is running. Plan the next one from the backlog."
	}
	return "Shows the work in flight and what was finished lately. Each swimlane can carry a limit."
}

func createBoard(ctx context.Context, tx db.DBTX, projectID uuid.UUID, in CreateInput, statuses []workflow.Status) (uuid.UUID, error) {
	var boardID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO board (project_id, name, description, type, team_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		projectID, in.Name, in.Description, string(in.Type), in.TeamID,
	).Scan(&boardID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create board: %w", err)
	}

	// One swimlane per status, in workflow order. That is the arrangement a
	// team recognises immediately, and merging two statuses into one lane is a
	// smaller step from there than splitting a single lane would be.
	for i, status := range statuses {
		var laneID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO board_swimlane (board_id, name, position)
			VALUES ($1, $2, $3)
			RETURNING id`, boardID, status.Name, i,
		).Scan(&laneID); err != nil {
			return uuid.Nil, fmt.Errorf("create swimlane %q: %w", status.Name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_swimlane_status (board_id, swimlane_id, status_id)
			VALUES ($1, $2, $3)`, boardID, laneID, status.ID,
		); err != nil {
			return uuid.Nil, fmt.Errorf("map status %q into its swimlane: %w", status.Name, err)
		}
	}

	return boardID, nil
}
