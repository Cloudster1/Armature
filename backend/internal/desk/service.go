package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Service runs the desk. It observes the issue service for the clocks and
// drives it for the portal.
type Service struct {
	db     *db.Cluster
	issues *issue.Service
	now    func() time.Time
	// repliesByMail is whether the worker reads an inbox, which the portal
	// tells requesters so they know answering the mail works.
	repliesByMail bool
	// files is where a request's attachments live; nil means the portal
	// says files are unavailable rather than failing later.
	files *attachment.Service
}

// WithAttachments lets customers put files on their requests.
func (s *Service) WithAttachments(files *attachment.Service) *Service {
	s.files = files
	return s
}

// RepliesByMail records that the desk reads answers to its mails.
func (s *Service) RepliesByMail(on bool) *Service {
	s.repliesByMail = on
	return s
}

func NewService(cluster *db.Cluster, issues *issue.Service) *Service {
	s := &Service{db: cluster, issues: issues, now: time.Now}
	issues.Observe(s)
	return s
}

// WithClock sets where the desk reads the time, which a test moves by hand.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// The request types and policies a new service project starts with. Goals
// are minutes: a first answer within the working day for anything urgent, and
// a resolution within the week for the rest.
var (
	defaultRequestTypes = []struct {
		name, description, issueType string
		priority                     issue.Priority
	}{
		{"Report a problem", "Something is broken or not working as it should.", "Bug", issue.PriorityHigh},
		{"Ask a question", "You need to know something.", "Task", issue.PriorityMedium},
		{"Request something", "Access, a change, or something new.", "Task", issue.PriorityMedium},
	}
	defaultGoals = map[Metric]map[issue.Priority]int{
		FirstResponse: {issue.PriorityHighest: 60, issue.PriorityHigh: 4 * 60, issue.PriorityMedium: 8 * 60, issue.PriorityLow: 24 * 60, issue.PriorityLowest: 48 * 60},
		Resolution:    {issue.PriorityHighest: 8 * 60, issue.PriorityHigh: 24 * 60, issue.PriorityMedium: 3 * 24 * 60, issue.PriorityLow: 5 * 24 * 60, issue.PriorityLowest: 10 * 24 * 60},
	}
	policyNames = map[Metric]string{FirstResponse: "Time to first response", Resolution: "Time to resolution"}
)

// WaitingOnCustomer is the status a service desk pauses its clocks in.
const WaitingOnCustomer = "Waiting for customer"

// Setup gives a new service project its request types and policies, in the
// caller's transaction. The template calls it; so can an administrator who
// turns an existing project into a desk.
func (s *Service) Setup(ctx context.Context, tx db.DBTX, projectID uuid.UUID) error {
	types := map[string]uuid.UUID{}
	rows, err := tx.Query(ctx, `SELECT id, name FROM issue_type`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		types[name] = id
	}
	rows.Close()

	for i, rt := range defaultRequestTypes {
		typeID, ok := types[rt.issueType]
		if !ok {
			typeID, ok = types["Task"]
			if !ok {
				return errors.New("the organization has no Task issue type for requests to become")
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO request_type (org_id, project_id, name, description, issue_type_id, priority, position)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING`, projectID, rt.name, rt.description, typeID, string(rt.priority), i); err != nil {
			return fmt.Errorf("create request type %q: %w", rt.name, err)
		}
	}

	var pause []uuid.UUID
	var waiting uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM issue_status WHERE lower(name) = lower($1)`, WaitingOnCustomer).Scan(&waiting)
	if err == nil {
		pause = append(pause, waiting)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	for _, metric := range []Metric{FirstResponse, Resolution} {
		goals, _ := json.Marshal(defaultGoals[metric])
		if _, err := tx.Exec(ctx, `
			INSERT INTO sla_policy (org_id, project_id, name, metric, goals, pause_status_ids)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`, projectID, policyNames[metric], string(metric), goals, pause); err != nil {
			return fmt.Errorf("create policy %q: %w", policyNames[metric], err)
		}
	}
	return nil
}

// ------------------------------------------------------ request types ------

const selectRequestType = `
SELECT rt.id, rt.project_id, p.key, rt.name, rt.description, rt.issue_type_id, it.name, rt.priority, rt.position,
       rt.category, rt.details_template, rt.team_id, COALESCE(tm.name, '')
FROM request_type rt
JOIN project p ON p.id = rt.project_id
JOIN issue_type it ON it.id = rt.issue_type_id
LEFT JOIN team tm ON tm.id = rt.team_id`

func scanRequestType(row pgx.Row) (*RequestType, error) {
	var rt RequestType
	err := row.Scan(&rt.ID, &rt.ProjectID, &rt.ProjectKey, &rt.Name, &rt.Description, &rt.IssueTypeID, &rt.IssueTypeName, &rt.Priority, &rt.Position,
		&rt.Category, &rt.DetailsTemplate, &rt.TeamID, &rt.TeamName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rt, nil
}

// RequestTypes lists a project's request types in the order they are offered.
func (s *Service) RequestTypes(ctx context.Context, projectKey string) ([]RequestType, error) {
	out := []RequestType{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRequestType+` WHERE p.key = $1 ORDER BY rt.position, rt.name`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			rt, err := scanRequestType(rows)
			if err != nil {
				return err
			}
			out = append(out, *rt)
		}
		return rows.Err()
	})
	return out, err
}

// RequestTypeInput describes a request type.
type RequestTypeInput struct {
	Name            string
	Description     string
	IssueTypeID     uuid.UUID
	Priority        issue.Priority
	Category        string
	DetailsTemplate string
	TeamID          *uuid.UUID
}

// teamOfProject refuses a team that is not the project's. The trigger refuses
// it too; this is the sentence.
func teamOfProject(ctx context.Context, tx db.DBTX, projectID uuid.UUID, teamID *uuid.UUID) error {
	if teamID == nil {
		return nil
	}
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT project_id FROM team WHERE id = $1`, *teamID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != projectID) {
		return ErrTeamElsewhere
	}
	return err
}

// tidyTemplate keeps the skeleton's lines and drops the trailing blank ones.
func tidyTemplate(text string) string { return strings.TrimRight(text, " \t\r\n") }

// CreateRequestType adds a way for customers to raise something.
func (s *Service) CreateRequestType(ctx context.Context, projectKey string, in RequestTypeInput, actor uuid.UUID) (*RequestType, db.LSN, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, 0, errors.New("a request type needs a name")
	}
	if in.Priority == "" {
		in.Priority = issue.PriorityMedium
	}
	if !in.Priority.Valid() {
		return nil, 0, fmt.Errorf("%q is not a priority", in.Priority)
	}

	var out *RequestType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		projectID, err := s.serviceProject(ctx, tx, projectKey)
		if err != nil {
			return err
		}
		typeID := in.IssueTypeID
		if typeID == uuid.Nil {
			if err := tx.QueryRow(ctx, `SELECT id FROM issue_type WHERE hierarchy_level = 0 ORDER BY position LIMIT 1`).Scan(&typeID); err != nil {
				return err
			}
		}
		if err := teamOfProject(ctx, tx, projectID, in.TeamID); err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO request_type (org_id, project_id, name, description, issue_type_id, priority, position, category, details_template, team_id)
			VALUES (current_org_id(), $1, $2, $3, $4, $5,
			        (SELECT COALESCE(max(position) + 1, 0) FROM request_type WHERE project_id = $1), $6, $7, $8)
			RETURNING id`, projectID, in.Name, strings.TrimSpace(in.Description), typeID, string(in.Priority),
			strings.TrimSpace(in.Category), tidyTemplate(in.DetailsTemplate), in.TeamID).Scan(&id)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if isForeignKeyViolation(err) {
			return errors.New("that issue type does not exist")
		}
		if err != nil {
			return fmt.Errorf("create request type: %w", err)
		}
		out, err = scanRequestType(tx.QueryRow(ctx, selectRequestType+` WHERE rt.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// RequestTypeUpdate carries what an edit may change. Nil leaves a field alone;
// ClearTeam takes the team away, since nil cannot say that.
type RequestTypeUpdate struct {
	Name            *string
	Description     *string
	Category        *string
	DetailsTemplate *string
	IssueTypeID     *uuid.UUID
	Priority        *issue.Priority
	TeamID          *uuid.UUID
	ClearTeam       bool
}

// UpdateRequestType changes an offer. Requests already raised through it keep
// what they came in as; only what the next one gets changes.
func (s *Service) UpdateRequestType(ctx context.Context, id uuid.UUID, in RequestTypeUpdate, actor uuid.UUID) (*RequestType, db.LSN, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, 0, errors.New("a request type needs a name")
	}
	if in.Priority != nil && !in.Priority.Valid() {
		return nil, 0, fmt.Errorf("%q is not a priority", *in.Priority)
	}
	var out *RequestType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanRequestType(tx.QueryRow(ctx, selectRequestType+` WHERE rt.id = $1`, id))
		if err != nil {
			return err
		}
		next := *before
		if in.Name != nil {
			next.Name = strings.TrimSpace(*in.Name)
		}
		if in.Description != nil {
			next.Description = strings.TrimSpace(*in.Description)
		}
		if in.Category != nil {
			next.Category = strings.TrimSpace(*in.Category)
		}
		if in.DetailsTemplate != nil {
			next.DetailsTemplate = tidyTemplate(*in.DetailsTemplate)
		}
		if in.IssueTypeID != nil {
			next.IssueTypeID = *in.IssueTypeID
		}
		if in.Priority != nil {
			next.Priority = *in.Priority
		}
		if in.ClearTeam {
			next.TeamID = nil
		} else if in.TeamID != nil {
			next.TeamID = in.TeamID
		}
		if err := teamOfProject(ctx, tx, before.ProjectID, next.TeamID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE request_type
			SET name = $2, description = $3, category = $4, details_template = $5, issue_type_id = $6, priority = $7, team_id = $8
			WHERE id = $1`, id, next.Name, next.Description, next.Category, next.DetailsTemplate, next.IssueTypeID, string(next.Priority), next.TeamID)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if isForeignKeyViolation(err) {
			return errors.New("that issue type does not exist")
		}
		if err != nil {
			return fmt.Errorf("update request type: %w", err)
		}
		out, err = scanRequestType(tx.QueryRow(ctx, selectRequestType+` WHERE rt.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteRequestType removes a way of raising things. Requests already raised
// through it keep their history; only the offer goes.
func (s *Service) DeleteRequestType(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM request_type WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// serviceProject resolves a key to a service project's id.
func (s *Service) serviceProject(ctx context.Context, tx db.DBTX, projectKey string) (uuid.UUID, error) {
	var (
		id   uuid.UUID
		kind string
	)
	err := tx.QueryRow(ctx, `SELECT id, kind FROM project WHERE key = $1 AND archived_at IS NULL`, project.NormalizeKey(projectKey)).Scan(&id, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, project.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if kind != string(project.KindService) {
		return uuid.Nil, ErrNotAServiceProject
	}
	return id, nil
}

// ------------------------------------------------------------ policies ------

const selectPolicy = `
SELECT sp.id, sp.project_id, p.key, sp.name, sp.metric, sp.goals, sp.pause_status_ids,
       COALESCE((SELECT array_agg(st.name ORDER BY st.name) FROM issue_status st WHERE st.id = ANY(sp.pause_status_ids)), '{}'),
       sp.use_calendar
FROM sla_policy sp
JOIN project p ON p.id = sp.project_id`

func scanPolicy(row pgx.Row) (*Policy, error) {
	var (
		p     Policy
		goals []byte
	)
	err := row.Scan(&p.ID, &p.ProjectID, &p.ProjectKey, &p.Name, &p.Metric, &goals, &p.PauseStatusIDs, &p.PauseStatuses, &p.UseCalendar)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Goals = map[issue.Priority]int{}
	if err := json.Unmarshal(goals, &p.Goals); err != nil {
		return nil, fmt.Errorf("read policy goals: %w", err)
	}
	if p.PauseStatusIDs == nil {
		p.PauseStatusIDs = []uuid.UUID{}
	}
	if p.PauseStatuses == nil {
		p.PauseStatuses = []string{}
	}
	return &p, nil
}

// Policies lists a project's SLA policies.
func (s *Service) Policies(ctx context.Context, projectKey string) ([]Policy, error) {
	out := []Policy{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectPolicy+` WHERE p.key = $1 ORDER BY sp.metric`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPolicy(rows)
			if err != nil {
				return err
			}
			out = append(out, *p)
		}
		return rows.Err()
	})
	return out, err
}

// PolicyInput carries what an edit may change. Nil leaves a field alone.
type PolicyInput struct {
	Goals          map[issue.Priority]int
	PauseStatusIDs *[]uuid.UUID
	// UseCalendar switches the goal to the project's business hours; it
	// needs a calendar to have been set.
	UseCalendar *bool
}

// UpdatePolicy changes a policy's goals. Timers already running keep the goal
// they started with; a promise is not rewritten after it was made.
func (s *Service) UpdatePolicy(ctx context.Context, id uuid.UUID, in PolicyInput, actor uuid.UUID) (*Policy, db.LSN, error) {
	for priority, minutes := range in.Goals {
		if !priority.Valid() {
			return nil, 0, fmt.Errorf("%q is not a priority", priority)
		}
		if minutes < 0 {
			return nil, 0, errors.New("a goal cannot be negative")
		}
	}
	var out *Policy
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanPolicy(tx.QueryRow(ctx, selectPolicy+` WHERE sp.id = $1`, id))
		if err != nil {
			return err
		}
		goals := before.Goals
		if in.Goals != nil {
			// A zero minute goal means "not measured" and is dropped.
			goals = map[issue.Priority]int{}
			for p, m := range in.Goals {
				if m > 0 {
					goals[p] = m
				}
			}
		}
		pause := before.PauseStatusIDs
		if in.PauseStatusIDs != nil {
			pause = *in.PauseStatusIDs
		}
		useCalendar := before.UseCalendar
		if in.UseCalendar != nil {
			useCalendar = *in.UseCalendar
			if useCalendar {
				var has bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM business_calendar WHERE project_id = $1)`, before.ProjectID).Scan(&has); err != nil {
					return err
				}
				if !has {
					return ErrNoCalendar
				}
			}
		}
		encoded, _ := json.Marshal(goals)
		if _, err := tx.Exec(ctx, `UPDATE sla_policy SET goals = $2, pause_status_ids = $3, use_calendar = $4 WHERE id = $1`, id, encoded, pause, useCalendar); err != nil {
			return fmt.Errorf("update policy: %w", err)
		}
		out, err = scanPolicy(tx.QueryRow(ctx, selectPolicy+` WHERE sp.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// -------------------------------------------------------------- timers ------

const selectTimer = `
SELECT t.id, t.issue_id, t.policy_id, sp.name, sp.metric, t.goal_minutes, t.started_at,
       t.elapsed_seconds, t.running_since, t.completed_at, t.breached_at,
       sp.use_calendar, bc.timezone, bc.hours, bc.holidays::text[]
FROM sla_timer t
JOIN sla_policy sp ON sp.id = t.policy_id
LEFT JOIN business_calendar bc ON bc.project_id = sp.project_id`

func (s *Service) scanTimers(rows pgx.Rows) ([]Timer, error) {
	defer rows.Close()
	now := s.now().UTC()
	out := []Timer{}
	for rows.Next() {
		t, err := scanTimer(rows)
		if err != nil {
			return nil, err
		}
		t.read(now)
		out = append(out, *t)
	}
	return out, rows.Err()
}

// scanTimer reads one timer with its calendar, when its policy counts hours.
func scanTimer(row pgx.Row) (*Timer, error) {
	var (
		t           Timer
		useCalendar bool
		timezone    *string
		hours       []byte
		holidays    []string
	)
	if err := row.Scan(&t.ID, &t.IssueID, &t.PolicyID, &t.PolicyName, &t.Metric, &t.GoalMinutes, &t.StartedAt,
		&t.elapsedSeconds, &t.RunningSince, &t.CompletedAt, &t.BreachedAt, &useCalendar, &timezone, &hours, &holidays); err != nil {
		return nil, err
	}
	if useCalendar && timezone != nil {
		if cal, err := parseCalendar(*timezone, hours, holidays); err == nil {
			t.calendar = cal
			t.BusinessHours = true
		}
	}
	return &t, nil
}

// TimersFor reads an issue's clocks.
func (s *Service) TimersFor(ctx context.Context, issueKey string) ([]Timer, error) {
	projectKey, num, err := issue.ParseKey(issueKey)
	if err != nil {
		return nil, err
	}
	var out []Timer
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectTimer+`
			JOIN issue i ON i.id = t.issue_id
			JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2
			ORDER BY sp.metric`, projectKey, num)
		if err != nil {
			return err
		}
		out, err = s.scanTimers(rows)
		return err
	})
	return out, err
}

// IssueCreated starts a clock for each policy of the issue's project that has
// a goal for its priority. An issue an agent files in a service project is
// measured too: the customer it is for is waiting either way.
func (s *Service) IssueCreated(ctx context.Context, tx db.DBTX, created *issue.Issue, actor issue.Actor) error {
	rows, err := tx.Query(ctx, `
		SELECT sp.id, sp.goals FROM sla_policy sp
		JOIN project p ON p.id = sp.project_id
		WHERE p.id = $1 AND p.kind = 'service'`, created.ProjectID)
	if err != nil {
		return err
	}
	type goal struct {
		policyID uuid.UUID
		minutes  int
	}
	var goals []goal
	for rows.Next() {
		var (
			id  uuid.UUID
			raw []byte
		)
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		byPriority := map[issue.Priority]int{}
		if err := json.Unmarshal(raw, &byPriority); err != nil {
			rows.Close()
			return err
		}
		if minutes := byPriority[created.Priority]; minutes > 0 {
			goals = append(goals, goal{id, minutes})
		}
	}
	rows.Close()

	for _, g := range goals {
		if _, err := tx.Exec(ctx, `
			INSERT INTO sla_timer (org_id, issue_id, policy_id, goal_minutes, started_at, running_since)
			VALUES (current_org_id(), $1, $2, $3, now(), now())
			ON CONFLICT (issue_id, policy_id) DO NOTHING`, created.ID, g.policyID, g.minutes); err != nil {
			return fmt.Errorf("start timer: %w", err)
		}
	}
	return nil
}

// IssueTransitioned moves the clocks with the status: a done status completes
// the resolution clock, a pause status stops both, leaving one starts them
// again, and reopening from done starts a fresh resolution clock.
func (s *Service) IssueTransitioned(ctx context.Context, tx db.DBTX, before, after *issue.Issue, actor issue.Actor) error {
	rows, err := tx.Query(ctx, selectTimer+` WHERE t.issue_id = $1`, after.ID)
	if err != nil {
		return err
	}
	timers, err := s.scanTimers(rows)
	if err != nil {
		return err
	}
	if len(timers) == 0 {
		return nil
	}

	pauses := map[uuid.UUID]map[uuid.UUID]bool{}
	prows, err := tx.Query(ctx, `SELECT id, pause_status_ids FROM sla_policy WHERE project_id = $1`, after.ProjectID)
	if err != nil {
		return err
	}
	for prows.Next() {
		var (
			id  uuid.UUID
			ids []uuid.UUID
		)
		if err := prows.Scan(&id, &ids); err != nil {
			prows.Close()
			return err
		}
		set := map[uuid.UUID]bool{}
		for _, sid := range ids {
			set[sid] = true
		}
		pauses[id] = set
	}
	prows.Close()

	toDone := after.Status.Category == workflow.CategoryDone
	fromDone := before.Status.Category == workflow.CategoryDone
	for _, t := range timers {
		paused := pauses[t.PolicyID][after.Status.ID]
		switch {
		case t.CompletedAt != nil && t.Metric == Resolution && fromDone && !toDone:
			// Reopened: the promise starts over, and the old reading is kept
			// as history by the new row replacing it.
			if _, err := tx.Exec(ctx, `
				UPDATE sla_timer SET started_at = now(), elapsed_seconds = 0, running_since = now(),
				    completed_at = NULL, breached_at = NULL WHERE id = $1`, t.ID); err != nil {
				return err
			}
		case t.CompletedAt != nil:
			continue
		case t.Metric == Resolution && toDone:
			if err := s.stop(ctx, tx, t, true); err != nil {
				return err
			}
		case paused && t.RunningSince != nil:
			if err := s.stop(ctx, tx, t, false); err != nil {
				return err
			}
		case !paused && t.RunningSince == nil:
			if _, err := tx.Exec(ctx, `UPDATE sla_timer SET running_since = now() WHERE id = $1`, t.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// stop banks the time run so far and either pauses or completes the clock.
// The banked stretch is counted by the clock, so a calendar's hours hold.
func (s *Service) stop(ctx context.Context, tx db.DBTX, t Timer, complete bool) error {
	now := s.now().UTC()
	clock := t.clock()
	elapsed := clock.Elapsed(now)
	breached := clock.Breached(now)
	_, err := tx.Exec(ctx, `
		UPDATE sla_timer
		SET elapsed_seconds = $3,
		    running_since = NULL,
		    completed_at = CASE WHEN $2 THEN now() ELSE completed_at END,
		    breached_at = CASE WHEN breached_at IS NULL AND $4 THEN now() ELSE breached_at END
		WHERE id = $1`, t.ID, complete, int64(elapsed/time.Second), breached)
	return err
}

// CommentAdded completes the first response clock when an agent answers the
// customer. A note does not count, and neither does the customer's own reply.
// IssueDeleted has nothing to do: the clocks go with the issue.
func (s *Service) IssueDeleted(context.Context, db.DBTX, *issue.Issue, issue.Actor) error { return nil }

func (s *Service) CommentAdded(ctx context.Context, tx db.DBTX, issueID uuid.UUID, internal bool, actor issue.Actor) error {
	if internal || !actor.OrgRole.IsAgent() {
		return nil
	}
	rows, err := tx.Query(ctx, selectTimer+` WHERE t.issue_id = $1 AND sp.metric = 'first_response' AND t.completed_at IS NULL`, issueID)
	if err != nil {
		return err
	}
	timers, err := s.scanTimers(rows)
	if err != nil {
		return err
	}
	for _, t := range timers {
		if err := s.stop(ctx, tx, t, true); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- queue ------

// QueueFilter is which of a desk's requests an agent wants to see.
type QueueFilter string

const (
	QueueOpen       QueueFilter = "open"
	QueueUnassigned QueueFilter = "unassigned"
	QueueMine       QueueFilter = "mine"
	QueueBreached   QueueFilter = "breached"
	QueueAll        QueueFilter = "all"
)

// Queue lists a desk's requests with their clocks, the least time left first.
func (s *Service) Queue(ctx context.Context, projectKey string, filter QueueFilter, agent uuid.UUID) ([]QueueRow, error) {
	f := issue.Filter{ProjectKey: project.NormalizeKey(projectKey)}
	switch filter {
	case QueueOpen, QueueBreached, "":
		f.Categories = []workflow.StatusCategory{workflow.CategoryTodo, workflow.CategoryInProgress}
	case QueueUnassigned:
		f.Categories = []workflow.StatusCategory{workflow.CategoryTodo, workflow.CategoryInProgress}
		f.Unassigned = true
	case QueueMine:
		f.Categories = []workflow.StatusCategory{workflow.CategoryTodo, workflow.CategoryInProgress}
		f.AssigneeID = &agent
	case QueueAll:
	default:
		return nil, fmt.Errorf("%q is not a queue", filter)
	}
	const queueSize = 200
	page, err := s.issues.List(ctx, f, issue.Page{Limit: queueSize, OrderBy: "created", Desc: true})
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, 0, len(page.Issues))
	for _, i := range page.Issues {
		ids = append(ids, i.ID)
	}
	byIssue := map[uuid.UUID][]Timer{}
	rated := map[uuid.UUID]int{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectTimer+` WHERE t.issue_id = ANY($1) ORDER BY sp.metric`, ids)
		if err != nil {
			return err
		}
		timers, err := s.scanTimers(rows)
		if err != nil {
			return err
		}
		for _, t := range timers {
			byIssue[t.IssueID] = append(byIssue[t.IssueID], t)
		}
		scores, err := tx.Query(ctx, `SELECT issue_id, score FROM csat_rating WHERE issue_id = ANY($1) AND score IS NOT NULL`, ids)
		if err != nil {
			return err
		}
		defer scores.Close()
		for scores.Next() {
			var (
				id    uuid.UUID
				score int
			)
			if err := scores.Scan(&id, &score); err != nil {
				return err
			}
			rated[id] = score
		}
		return scores.Err()
	})
	if err != nil {
		return nil, err
	}

	out := []QueueRow{}
	for _, i := range page.Issues {
		timers := byIssue[i.ID]
		if timers == nil {
			timers = []Timer{}
		}
		if filter == QueueBreached && !anyBreached(timers) {
			continue
		}
		row := QueueRow{Issue: i, Timers: timers, RequestTypeName: i.RequestTypeName}
		if score, ok := rated[i.ID]; ok {
			row.Csat = &score
		}
		out = append(out, row)
	}
	sortByUrgency(out)
	return out, nil
}

func anyBreached(timers []Timer) bool {
	for _, t := range timers {
		if t.Breached && t.CompletedAt == nil {
			return true
		}
	}
	return false
}

// sortByUrgency puts the request with the least time left on any running
// clock first; requests with no running clock go last, newest first.
func sortByUrgency(rows []QueueRow) {
	least := func(r QueueRow) (int64, bool) {
		var (
			best  int64
			found bool
		)
		for _, t := range r.Timers {
			if t.CompletedAt != nil {
				continue
			}
			if !found || t.RemainingSeconds < best {
				best, found = t.RemainingSeconds, true
			}
		}
		return best, found
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			a, aok := least(rows[j-1])
			b, bok := least(rows[j])
			if (aok && bok && a <= b) || (aok && !bok) || (!aok && !bok) {
				break
			}
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

// --------------------------------------------------------------- portal ------

// Desks lists the service projects a customer can raise something in: one,
// when the session came through that desk's open door, and only those that
// trust the customer's address. An agent looking through the portal sees all.
func (s *Service) Desks(ctx context.Context, viewer issue.Actor) ([]Desk, error) {
	out := []Desk{}
	var only *uuid.UUID
	if scope, ok := ScopeFrom(ctx); ok {
		only = &scope.ProjectID
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.key, p.name, p.description FROM project p
			LEFT JOIN app_user u ON u.id = $2
			WHERE p.kind = 'service' AND p.archived_at IS NULL AND ($1::uuid IS NULL OR p.id = $1)
			  AND ($3 OR cardinality(p.trusted_domains) = 0 OR lower(split_part(u.email::text, '@', 2)) = ANY(p.trusted_domains))
			ORDER BY p.name`, only, viewer.UserID, !IsCustomer(viewer.OrgRole))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Desk
			if err := rows.Scan(&d.ProjectID, &d.ProjectKey, &d.Name, &d.Description); err != nil {
				return err
			}
			d.RequestTypes = []RequestType{}
			d.RepliesByMail = s.repliesByMail
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		types, err := s.RequestTypes(ctx, out[i].ProjectKey)
		if err != nil {
			return nil, err
		}
		out[i].RequestTypes = types
	}
	return out, nil
}

// RaiseInput is what a customer fills in.
type RaiseInput struct {
	RequestTypeID uuid.UUID
	Summary       string
	Description   string
}

// OpenDoor is a desk whose door does not ask for a code, as the door page
// names it to whoever arrives.
type OpenDoor struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// OpenDoors lists an organization's desks that let people in without a code.
// It runs as the system: nobody is signed in at the door.
func (s *Service) OpenDoors(ctx context.Context, orgID uuid.UUID) ([]OpenDoor, error) {
	out := []OpenDoor{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT key, name FROM project
			WHERE org_id = $1 AND kind = 'service' AND NOT portal_verifies AND archived_at IS NULL ORDER BY name`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d OpenDoor
			if err := rows.Scan(&d.Key, &d.Name); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// Raise files a request through a request type, as the customer.
func (s *Service) Raise(ctx context.Context, in RaiseInput, customer issue.Actor) (*issue.Issue, db.LSN, error) {
	var rt *RequestType
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		rt, err = scanRequestType(tx.QueryRow(ctx, selectRequestType+` WHERE rt.id = $1`, in.RequestTypeID))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	// Outside the door the session came through, the request type is as good
	// as not there.
	if !allowsKey(ctx, rt.ProjectKey) {
		return nil, 0, ErrNotFound
	}
	if IsCustomer(customer.OrgRole) {
		if err := s.trusts(ctx, rt.ProjectID, customer.UserID); err != nil {
			return nil, 0, err
		}
	}
	var description json.RawMessage
	if text := strings.TrimSpace(in.Description); text != "" {
		description = issue.TextDocument(text)
	}
	return s.issues.Create(ctx, issue.CreateInput{
		ProjectKey:    rt.ProjectKey,
		TypeID:        rt.IssueTypeID,
		Summary:       in.Summary,
		Description:   description,
		Priority:      rt.Priority,
		TeamID:        rt.TeamID,
		RequestTypeID: &rt.ID,
	}, customer)
}

// trusts refuses a customer whose address is at a domain the desk does not
// take, naming the ones it does.
func (s *Service) trusts(ctx context.Context, projectID, userID uuid.UUID) error {
	var (
		domains []string
		email   string
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT p.trusted_domains, u.email::text FROM project p, app_user u WHERE p.id = $1 AND u.id = $2`, projectID, userID).Scan(&domains, &email)
	})
	if err != nil {
		return err
	}
	if !project.Trusts(domains, email) {
		return &project.NotTrustedError{Domains: domains}
	}
	return nil
}

// MyRequests lists the requests a customer raised or follows, newest first.
func (s *Service) MyRequests(ctx context.Context, customer uuid.UUID) ([]issue.Issue, error) {
	const pageSize = 100
	filter := issue.Filter{InvolvingUser: &customer, ProjectKind: string(project.KindService)}
	if scope, ok := ScopeFrom(ctx); ok {
		filter.ProjectKey = scope.ProjectKey
	}
	page, err := s.issues.List(ctx, filter, issue.Page{Limit: pageSize, OrderBy: "created", Desc: true})
	if err != nil {
		return nil, err
	}
	if page.Issues == nil {
		return []issue.Issue{}, nil
	}
	return page.Issues, nil
}

// Request reads one request as its customer sees it: the reporter's, or a
// follower's. A request that is not theirs is not found: existence itself is
// privileged.
func (s *Service) Request(ctx context.Context, key string, customer uuid.UUID) (*Request, error) {
	found, err := s.issues.ByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if !allows(ctx, found.ProjectID) {
		return nil, ErrNotFound
	}
	if found.Reporter == nil || found.Reporter.ID != customer {
		watching, err := s.issues.Watches(ctx, key, customer)
		if err != nil {
			return nil, err
		}
		if !watching {
			return nil, ErrNotFound
		}
	}
	comments, err := s.issues.Comments(ctx, key, false)
	if err != nil {
		return nil, err
	}
	if comments == nil {
		comments = []issue.Comment{}
	}
	return &Request{Issue: *found, Comments: comments, RequestTypeName: found.RequestTypeName}, nil
}

// Reply adds the customer's comment to their own request. If the desk was
// waiting on them, the reply is what it was waiting for, and the request goes
// back to the agents through whichever transition leaves the waiting status.
func (s *Service) Reply(ctx context.Context, key, text string, customer issue.Actor) (*issue.Comment, db.LSN, error) {
	if _, err := s.Request(ctx, key, customer.UserID); err != nil {
		return nil, 0, err
	}
	comment, lsn, err := s.issues.AddComment(ctx, key, issue.TextDocument(text), customer)
	if err != nil {
		return nil, 0, err
	}

	found, err := s.issues.ByKey(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if strings.EqualFold(found.Status.Name, WaitingOnCustomer) {
		if chosen := s.wayBack(ctx, key, customer); chosen != nil {
			if _, moved, err := s.issues.Transition(ctx, key, issue.TransitionInput{TransitionID: chosen.ID}, customer); err == nil {
				lsn = moved
			}
		}
	}
	return comment, lsn, nil
}

// CustomerResponded is the transition a customer's reply takes when the desk
// was waiting on them, when the workflow has one by that name.
const CustomerResponded = "Customer responded"

// wayBack picks the transition a reply takes out of the waiting status: the
// one named for it, else the first that is not a global "resolve from
// anywhere", which a customer's reply must never take.
func (s *Service) wayBack(ctx context.Context, key string, customer issue.Actor) *workflow.Transition {
	available, err := s.issues.Transitions(ctx, key, customer)
	if err != nil {
		return nil
	}
	for i := range available {
		if strings.EqualFold(available[i].Name, CustomerResponded) {
			return &available[i]
		}
	}
	for i := range available {
		if !available[i].IsGlobal() {
			return &available[i]
		}
	}
	return nil
}

// IsCustomer reports whether a principal's role is the portal's.
func IsCustomer(role auth.OrgRole) bool { return role == auth.RoleCustomer }

// ---------------------------------------------------------------- events ------

// Breach records that a running clock ran out, once, and tells the stream.
func (s *Service) breach(ctx context.Context, tx db.DBTX, timerID uuid.UUID) error {
	// The watch finds suspects by wall time; a clock that counts only open
	// hours has the last word, so nothing is recorded that has not happened.
	t, err := scanTimer(tx.QueryRow(ctx, selectTimer+` WHERE t.id = $1`, timerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !t.clock().Breached(s.now().UTC()) {
		return nil
	}
	var (
		issueKey string
		metric   Metric
	)
	err = tx.QueryRow(ctx, `
		UPDATE sla_timer t SET breached_at = now()
		FROM issue i, project p, sla_policy sp
		WHERE t.id = $1 AND t.breached_at IS NULL AND i.id = t.issue_id AND p.id = i.project_id AND sp.id = t.policy_id
		RETURNING p.key || '-' || i.key_num, sp.metric`, timerID).Scan(&issueKey, &metric)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return events.EmitInTenant(ctx, tx, events.TopicSLABreached, map[string]any{
		"issueKey": issueKey, "metric": metric, "timerId": timerID,
	})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
