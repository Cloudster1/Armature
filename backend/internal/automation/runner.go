package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/tenant"
)

// payload is every field an event may carry that a rule reads.
type payload struct {
	Key      string    `json:"key"`
	IssueKey string    `json:"issueKey"`
	ActorID  uuid.UUID `json:"actorId"`
	RuleID   uuid.UUID `json:"ruleId"`
	Changes  []struct {
		Field string `json:"field"`
	} `json:"changes"`
	// Body is what an incoming call posted, kept for the comment it may become.
	Body json.RawMessage `json:"body"`
}

func (p payload) key() string {
	if p.Key != "" {
		return p.Key
	}
	return p.IssueKey
}

// Handle considers every rule an event could start. Safe to call twice with
// the same event: a run exists per rule and event, and the second pass stops
// at the row that is already there.
func (s *Service) Handle(ctx context.Context, e events.Event) error {
	if e.OrgID == uuid.Nil {
		return nil
	}
	var p payload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return nil
		}
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: e.OrgID}))

	var rules []Rule
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		switch e.Topic {
		case events.TopicAutomationScheduled, events.TopicAutomationIncoming, events.TopicAutomationManual:
			if p.RuleID == uuid.Nil {
				return nil
			}
			rules, err = s.readRules(ctx, tx, `WHERE r.id = $1`, p.RuleID)
		default:
			// Most events are nobody's trigger; they cost nothing here.
			if p.key() == "" || !triggerKinds[e.Topic] {
				return nil
			}
			projectKey, num, err := issue.ParseKey(p.key())
			if err != nil {
				return nil
			}
			// The issue's project, or the organization's rules that watch every project.
			rules, err = s.readRules(ctx, tx, `
				WHERE r.enabled AND r.trigger->>'kind' = $1
				  AND (r.project_id IS NULL OR r.project_id = (
				      SELECT i.project_id FROM issue i JOIN project pr ON pr.id = i.project_id WHERE pr.key = $2 AND i.key_num = $3))
				ORDER BY r.created_at`, e.Topic, projectKey, num)
			return err
		}
		return err
	})
	if err != nil {
		return err
	}
	for _, r := range rules {
		if err := s.run(ctx, r, e, p); err != nil {
			s.log.Warn("a rule could not run", "rule", r.ID, "error", err)
		}
	}
	return nil
}

// run is one rule against one event: claim the run, guard, test, act, record.
func (s *Service) run(ctx context.Context, r Rule, e events.Event, p payload) error {
	var (
		runID uuid.UUID
		actor issue.Actor
	)
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if actor, err = s.automationActor(ctx, tx); err != nil {
			return err
		}
		var issueID *uuid.UUID
		if projectKey, num, err := issue.ParseKey(p.key()); err == nil {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT i.id FROM issue i JOIN project pr ON pr.id = i.project_id WHERE pr.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&id); err == nil {
				issueID = &id
			}
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO automation_run (org_id, rule_id, event_id, issue_id)
			VALUES (current_org_id(), $1, $2, $3)
			ON CONFLICT (rule_id, event_id) DO NOTHING RETURNING id`, r.ID, e.ID, issueID).Scan(&runID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil || runID == uuid.Nil {
		return err
	}
	finish := func(outcome, reason string, results []ActionResult) error {
		if results == nil {
			results = []ActionResult{}
		}
		raw, _ := json.Marshal(results)
		_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE automation_run SET finished_at = now(), outcome = $2, reason = $3, actions = $4 WHERE id = $1`, runID, outcome, reason, raw)
			return err
		})
		return err
	}

	// The loop guard: a rule's own writes come back as events with the
	// automation account as actor, and are not a reason to run again.
	if p.ActorID == actor.UserID && !r.AllowOwnEvents {
		return finish(OutcomeSkipped, "actor is automation", nil)
	}
	var recent int
	if err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM automation_run
			WHERE rule_id = $1 AND id <> $2 AND started_at > now() - interval '1 hour' AND outcome IN ('done', 'failed')`, r.ID, runID).Scan(&recent)
	}); err != nil {
		return err
	}
	if recent >= r.HourlyCap {
		// One capped row an hour says so in the log; the rest of a storm
		// leaves nothing behind, so the log stays a log and not a flood.
		var said bool
		_ = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM automation_run WHERE rule_id = $1 AND id <> $2 AND outcome = 'capped' AND started_at > now() - interval '1 hour')`, r.ID, runID).Scan(&said)
		})
		if said {
			_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, `DELETE FROM automation_run WHERE id = $1`, runID)
				return err
			})
			return err
		}
		return finish(OutcomeCapped, fmt.Sprintf("the rule ran %d times this hour, its cap", recent), nil)
	}
	if r.Trigger.Kind == TriggerIssueUpdated && r.Trigger.Field != "" {
		changed := false
		for _, c := range p.Changes {
			if strings.EqualFold(c.Field, r.Trigger.Field) {
				changed = true
			}
		}
		if !changed {
			return finish(OutcomeSkipped, fmt.Sprintf("%s did not change", r.Trigger.Field), nil)
		}
	}

	// Which issues the actions run over: the event's, or a schedule's query.
	var keys []string
	if e.Topic == events.TopicAutomationScheduled {
		found, err := s.matching(ctx, r.Trigger.Query, r.ProjectKey, actor.UserID, nil)
		if err != nil {
			return finish(OutcomeFailed, "the query did not run: "+err.Error(), nil)
		}
		if len(found) > MaxScheduledIssues {
			found = found[:MaxScheduledIssues]
		}
		keys = found
	} else if p.key() != "" {
		keys = []string{p.key()}
	}

	// Conditions are asked of the event's issue; a scheduled rule's query is
	// its condition, and a call with no issue has nothing to test but the actor.
	if len(keys) == 1 && e.Topic != events.TopicAutomationScheduled {
		ok, why, err := s.conditionsHold(ctx, r, keys[0], p, actor.UserID)
		if err != nil {
			return finish(OutcomeFailed, err.Error(), nil)
		}
		if !ok {
			return finish(OutcomeSkipped, why, nil)
		}
	}

	var results []ActionResult
	failed := false
	act := func(key string) {
		for _, a := range r.Actions {
			res := s.act(ctx, r, a, key, p, e, actor)
			if !res.OK {
				failed = true
			}
			results = append(results, res)
		}
	}
	if len(keys) == 0 {
		act("")
	}
	for _, key := range keys {
		act(key)
	}
	outcome := OutcomeDone
	if failed {
		outcome = OutcomeFailed
	}
	reason := ""
	if e.Topic == events.TopicAutomationScheduled {
		reason = fmt.Sprintf("%d issues matched", len(keys))
	}
	return finish(outcome, reason, results)
}

// matching runs a query, narrowed to a project or to some keys.
func (s *Service) matching(ctx context.Context, query, projectKey string, as uuid.UUID, keys []string) ([]string, error) {
	parsed, err := nql.Parse(query)
	if err != nil {
		return nil, err
	}
	compiled, err := parsed.Compile(nql.Env{UserID: as, Now: s.now()})
	if err != nil {
		return nil, err
	}
	return s.issues.Keys(ctx, issue.Filter{ProjectKey: projectKey, Query: compiled, Keys: keys})
}

// conditionsHold tests every condition; the first that fails names itself.
func (s *Service) conditionsHold(ctx context.Context, r Rule, key string, p payload, as uuid.UUID) (bool, string, error) {
	if len(r.Conditions) == 0 {
		return true, "", nil
	}
	var subject *issue.Issue
	for _, c := range r.Conditions {
		switch c.Kind {
		case ConditionNQL:
			found, err := s.matching(ctx, c.Query, "", as, []string{key})
			if err != nil {
				return false, "", fmt.Errorf("the condition's query did not run: %w", err)
			}
			if len(found) == 0 {
				return false, fmt.Sprintf("%s does not match %q", key, c.Query), nil
			}
		case ConditionFieldEqual:
			if subject == nil {
				var err error
				if subject, err = s.issues.ByKey(ctx, key); err != nil {
					return false, "", err
				}
			}
			if !fieldEquals(subject, c.Field, c.Value) {
				return false, fmt.Sprintf("%s is not %q on %s", c.Field, c.Value, key), nil
			}
		case ConditionActorRole:
			role, err := s.roleOf(ctx, p.ActorID)
			if err != nil {
				return false, "", err
			}
			held := false
			for _, want := range c.Roles {
				if strings.EqualFold(want, role) {
					held = true
				}
			}
			if !held {
				return false, fmt.Sprintf("the actor is %s, not %s", orNobody(role), strings.Join(c.Roles, " or ")), nil
			}
		}
	}
	return true, "", nil
}

func orNobody(role string) string {
	if role == "" {
		return "nobody"
	}
	return role
}

// fieldEquals compares one issue field with a written value, loosely: names
// and priorities by word, the assignee by id or "none", a label by presence.
func fieldEquals(i *issue.Issue, field, value string) bool {
	value = strings.TrimSpace(value)
	switch strings.ToLower(field) {
	case "status":
		return strings.EqualFold(i.Status.Name, value)
	case "priority":
		return strings.EqualFold(string(i.Priority), value)
	case "type":
		return strings.EqualFold(i.Type.Name, value)
	case "assignee":
		if strings.EqualFold(value, "none") {
			return i.Assignee == nil
		}
		return i.Assignee != nil && (i.Assignee.ID.String() == value || strings.EqualFold(i.Assignee.Name, value) || strings.EqualFold(i.Assignee.Email, value))
	case "label":
		for _, l := range i.Labels {
			if strings.EqualFold(l.Name, value) {
				return true
			}
		}
		return false
	}
	return false
}

func (s *Service) roleOf(ctx context.Context, userID uuid.UUID) (string, error) {
	if userID == uuid.Nil {
		return "", nil
	}
	var role string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE user_id = $1`, userID).Scan(&role)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// act does one action on one issue (or none) and says what happened.
func (s *Service) act(ctx context.Context, r Rule, a Action, key string, p payload, e events.Event, actor issue.Actor) ActionResult {
	res := ActionResult{Kind: a.Kind}
	fail := func(format string, args ...any) ActionResult {
		res.Note = fmt.Sprintf(format, args...)
		return res
	}
	needsIssue := map[string]bool{ActionSetField: true, ActionTransition: true, ActionAssign: true, ActionAddLabel: true, ActionAddComment: true, ActionCreateSubissue: true}
	if needsIssue[a.Kind] && key == "" {
		return fail("there is no issue to act on")
	}
	var current *issue.Issue
	if key != "" {
		var err error
		if current, err = s.issues.ByKey(ctx, key); err != nil {
			return fail("%s could not be read: %v", key, err)
		}
		// A rule reaches only its own project, whoever names the issue.
		if r.ProjectKey != "" && !strings.EqualFold(current.ProjectKey, r.ProjectKey) {
			return fail("%s is not in %s, which this rule runs in", key, r.ProjectKey)
		}
	}
	switch a.Kind {
	case ActionSetField:
		in, note, err := setFieldInput(a, s.now())
		if err != nil {
			return fail("%v", err)
		}
		if _, _, err := s.issues.Update(ctx, key, in, actor); err != nil {
			return fail("%s: %v", key, err)
		}
		res.Note = fmt.Sprintf("%s on %s", note, key)
	case ActionAssign:
		var who *uuid.UUID
		switch strings.ToLower(a.Value) {
		case "none", "nobody", "":
		case "reporter":
			if current.Reporter != nil {
				who = &current.Reporter.ID
			}
		case "actor":
			if p.ActorID != uuid.Nil {
				id := p.ActorID
				who = &id
			}
		default:
			id, err := uuid.Parse(a.Value)
			if err != nil {
				return fail("%q is not a person", a.Value)
			}
			who = &id
		}
		if _, _, err := s.issues.Update(ctx, key, issue.UpdateInput{Assignee: &who}, actor); err != nil {
			return fail("%s: %v", key, err)
		}
		res.Note = fmt.Sprintf("assigned %s to %s", key, a.Value)
	case ActionTransition:
		offered, err := s.issues.Transitions(ctx, key, actor)
		if err != nil {
			return fail("%s: %v", key, err)
		}
		var id uuid.UUID
		for _, t := range offered {
			if strings.EqualFold(t.Name, a.Value) {
				id = t.ID
			}
		}
		if id == uuid.Nil {
			return fail("%s does not offer %q from %s", key, a.Value, current.Status.Name)
		}
		if _, _, err := s.issues.Transition(ctx, key, issue.TransitionInput{TransitionID: id}, actor); err != nil {
			return fail("%s: %v", key, err)
		}
		res.Note = fmt.Sprintf("moved %s with %s", key, a.Value)
	case ActionAddLabel:
		names := []string{}
		for _, l := range current.Labels {
			names = append(names, l.Name)
		}
		names = append(names, a.Value)
		if _, _, err := s.labels.SetIssueLabels(ctx, key, names, actor); err != nil {
			return fail("%s: %v", key, err)
		}
		res.Note = fmt.Sprintf("labelled %s %s", key, a.Value)
	case ActionAddComment:
		text := s.fill(ctx, a.Text, current, p)
		if _, _, err := s.issues.AddComment(ctx, key, issue.TextDocument(text), actor); err != nil {
			return fail("%s: %v", key, err)
		}
		res.Note = fmt.Sprintf("commented on %s", key)
	case ActionCreateSubissue:
		typeID, err := s.subtaskType(ctx)
		if err != nil {
			return fail("%v", err)
		}
		made, _, err := s.issues.Create(ctx, issue.CreateInput{ProjectKey: current.ProjectKey, TypeID: typeID, Summary: s.fill(ctx, a.Text, current, p), ParentKey: key}, actor)
		if err != nil {
			return fail("under %s: %v", key, err)
		}
		res.Note = fmt.Sprintf("created %s under %s", made.Key, key)
	case ActionCreateIssue:
		if r.ProjectKey == "" {
			return fail("an organization rule has no project to create in")
		}
		made, _, err := s.issues.Create(ctx, issue.CreateInput{ProjectKey: r.ProjectKey, Summary: s.fill(ctx, a.Text, current, p)}, actor)
		if err != nil {
			return fail("in %s: %v", r.ProjectKey, err)
		}
		res.Note = fmt.Sprintf("created %s", made.Key)
	case ActionSendWebhook:
		if s.webhooks == nil || a.EndpointID == nil {
			return fail("there is no webhook to send to")
		}
		if _, err := s.webhooks.Enqueue(ctx, *a.EndpointID, e); err != nil {
			return fail("webhook: %v", err)
		}
		res.Note = "queued the webhook"
	case ActionSendMail:
		if s.mailer == nil {
			return fail("mail is not set up on this deployment")
		}
		to, err := s.addresses(ctx, a.To, current)
		if err != nil {
			return fail("%v", err)
		}
		if len(to) == 0 {
			return fail("nobody to mail for %q", a.To)
		}
		for _, address := range to {
			if err := s.mailer.Send(ctx, mail.Mail{To: address, Subject: s.fill(ctx, a.Subject, current, p), Body: s.fill(ctx, a.Text, current, p)}); err != nil {
				return fail("mail to %s: %v", address, err)
			}
		}
		res.Note = fmt.Sprintf("mailed %s", strings.Join(to, ", "))
	default:
		return fail("%q is not an action", a.Kind)
	}
	res.OK = true
	return res
}

// setFieldInput reads a set_field action into an update.
func setFieldInput(a Action, now time.Time) (issue.UpdateInput, string, error) {
	var in issue.UpdateInput
	value := strings.TrimSpace(a.Value)
	switch strings.ToLower(a.Field) {
	case "priority":
		p := issue.Priority(strings.ToLower(value))
		if !p.Valid() {
			return in, "", fmt.Errorf("%q is not a priority", value)
		}
		in.Priority = &p
		return in, "set priority to " + string(p), nil
	case "summary":
		in.Summary = &value
		return in, "set the summary", nil
	case "due":
		days, err := strconv.Atoi(strings.TrimPrefix(value, "+"))
		if err != nil {
			return in, "", fmt.Errorf("due takes +N days, not %q", value)
		}
		at := now.AddDate(0, 0, days)
		ptr := &at
		in.DueDate = &ptr
		return in, fmt.Sprintf("set due to %s", at.Format("2006-01-02")), nil
	}
	return in, "", fmt.Errorf("%q cannot be set by a rule; priority, summary and due can", a.Field)
}

// fill writes the issue and the actor into a template.
func (s *Service) fill(ctx context.Context, text string, i *issue.Issue, p payload) string {
	out := text
	if i != nil {
		out = strings.ReplaceAll(out, "{{issue.key}}", i.Key)
		out = strings.ReplaceAll(out, "{{issue.summary}}", i.Summary)
	}
	if strings.Contains(out, "{{actor.name}}") {
		name := "Somebody"
		if p.ActorID != uuid.Nil {
			_ = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
				return tx.QueryRow(ctx, `SELECT name FROM app_user WHERE id = $1`, p.ActorID).Scan(&name)
			})
		}
		out = strings.ReplaceAll(out, "{{actor.name}}", name)
	}
	return out
}

func (s *Service) subtaskType(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM issue_type WHERE is_subtask ORDER BY position LIMIT 1`).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errors.New("this organization has no subtask type")
	}
	return id, err
}

// addresses resolves who a mail action means.
func (s *Service) addresses(ctx context.Context, to string, i *issue.Issue) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(to)) {
	case "assignee":
		if i == nil || i.Assignee == nil {
			return nil, nil
		}
		return s.emails(ctx, []uuid.UUID{i.Assignee.ID})
	case "reporter":
		if i == nil || i.Reporter == nil {
			return nil, nil
		}
		return s.emails(ctx, []uuid.UUID{i.Reporter.ID})
	case "watchers":
		if i == nil {
			return nil, nil
		}
		watchers, err := s.issues.Watchers(ctx, i.Key)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, w := range watchers {
			out = append(out, w.Email)
		}
		return out, nil
	}
	if !strings.Contains(to, "@") {
		return nil, fmt.Errorf("%q is neither assignee, reporter, watchers nor an address", to)
	}
	return []string{to}, nil
}

func (s *Service) emails(ctx context.Context, ids []uuid.UUID) ([]string, error) {
	var out []string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT email FROM app_user WHERE id = ANY($1) AND is_active`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e string
			if err := rows.Scan(&e); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// RunNow runs one rule by hand against an issue, or with none, and returns
// the run. The event is minted here, so a second press is a second run.
func (s *Service) RunNow(ctx context.Context, ruleID uuid.UUID, issueKey string, actor uuid.UUID) (*Run, error) {
	rule, err := s.Get(ctx, ruleID)
	if err != nil {
		return nil, err
	}
	if rule.ProjectKey != "" && issueKey != "" {
		if key, _, err := issue.ParseKey(issueKey); err != nil || !strings.EqualFold(key, rule.ProjectKey) {
			return nil, fmt.Errorf("%w: %s is not in %s, which this rule runs in", ErrOutsideRule, issueKey, rule.ProjectKey)
		}
	}
	org, _ := tenant.FromContext(ctx)
	eventID := uuid.New()
	raw, _ := json.Marshal(map[string]any{"ruleId": rule.ID, "key": issueKey, "actorId": actor})
	if err := s.Handle(ctx, events.Event{ID: eventID, OrgID: org.ID, Topic: events.TopicAutomationManual, Payload: raw}); err != nil {
		return nil, err
	}
	var run *Run
	err = s.db.Read(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		var err error
		run, err = scanRun(tx.QueryRow(ctx, selectRuns+` WHERE ru.rule_id = $1 AND ru.event_id = $2`, ruleID, eventID))
		return err
	})
	return run, err
}

// Incoming records a call on a hook's address as an event, so the rule runs
// through the stream like any other and the caller is answered at once.
func (s *Service) Incoming(ctx context.Context, rule *Rule, orgID uuid.UUID, body json.RawMessage) error {
	var posted struct {
		IssueKey string `json:"issueKey"`
	}
	_ = json.Unmarshal(body, &posted)
	if len(body) == 0 {
		body = json.RawMessage(`{}`)
	}
	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: orgID}))
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return events.EmitInTenant(ctx, tx, events.TopicAutomationIncoming, map[string]any{
			"ruleId": rule.ID, "key": posted.IssueKey, "body": body,
		})
	})
	return err
}

// Tick runs every scheduled rule that is due, across organizations. The run
// happens here rather than through the stream: the run row it leaves is what
// the next tick reads as the last run, so a rule cannot fire twice while an
// event is in flight.
func (s *Service) Tick(ctx context.Context) (int, error) {
	type candidate struct {
		orgID, ruleID uuid.UUID
		schedule      Schedule
		last          time.Time
	}
	var found []candidate
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT r.org_id, r.id, r.trigger->'schedule',
			       COALESCE((SELECT max(started_at) FROM automation_run WHERE rule_id = r.id), r.created_at)
			FROM automation_rule r WHERE r.enabled AND r.trigger->>'kind' = $1`, TriggerScheduled)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				c   candidate
				raw []byte
			)
			if err := rows.Scan(&c.orgID, &c.ruleID, &raw, &c.last); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &c.schedule); err != nil {
				continue
			}
			found = append(found, c)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	now := s.now()
	fired := 0
	for _, c := range found {
		if !c.schedule.Due(c.last, now) {
			continue
		}
		raw, _ := json.Marshal(map[string]any{"ruleId": c.ruleID, "at": now})
		if err := s.Handle(ctx, events.Event{ID: uuid.New(), OrgID: c.orgID, Topic: events.TopicAutomationScheduled, Payload: raw}); err != nil {
			s.log.Warn("could not run a scheduled rule", "rule", c.ruleID, "error", err)
			continue
		}
		fired++
	}
	return fired, nil
}
