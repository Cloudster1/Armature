package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/webhook"
)

// ErrNotFound is returned for a rule that is not here.
var ErrNotFound = errors.New("there is no such rule")

// ErrOutsideRule is returned when a run names an issue outside the project the
// rule belongs to.
var ErrOutsideRule = errors.New("that issue is outside this rule's project")

// Service keeps rules and runs them. The same object serves the API's pages
// and the worker's consumer, so a rule run by hand and one run by the stream
// take exactly the same path.
type Service struct {
	db       *db.Cluster
	issues   *issue.Service
	labels   *label.Service
	mailer   mail.Mailer
	webhooks *webhook.Service
	log      *slog.Logger
	now      func() time.Time
}

func NewService(cluster *db.Cluster, issues *issue.Service, labels *label.Service, log *slog.Logger) *Service {
	return &Service{db: cluster, issues: issues, labels: labels, log: log, now: time.Now}
}

// WithMailer lets the send mail action send; without one it says so.
func (s *Service) WithMailer(m mail.Mailer) *Service {
	s.mailer = m
	return s
}

// WithWebhooks lets the send webhook action queue a delivery.
func (s *Service) WithWebhooks(w *webhook.Service) *Service {
	s.webhooks = w
	return s
}

const selectRules = `
SELECT r.id, r.project_id, COALESCE(p.key, ''), r.name, r.enabled, r.trigger, r.conditions, r.actions,
       r.allow_own_events, r.hourly_cap, r.created_at, r.updated_at,
       (SELECT max(started_at) FROM automation_run WHERE rule_id = r.id)
FROM automation_rule r LEFT JOIN project p ON p.id = r.project_id`

func scanRule(row pgx.Row) (*Rule, error) {
	var (
		r                            Rule
		trigger, conditions, actions []byte
	)
	if err := row.Scan(&r.ID, &r.ProjectID, &r.ProjectKey, &r.Name, &r.Enabled, &trigger, &conditions, &actions, &r.AllowOwnEvents, &r.HourlyCap, &r.CreatedAt, &r.UpdatedAt, &r.LastRunAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(trigger, &r.Trigger); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(conditions, &r.Conditions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(actions, &r.Actions); err != nil {
		return nil, err
	}
	if r.Conditions == nil {
		r.Conditions = []Condition{}
	}
	return &r, nil
}

func (s *Service) readRules(ctx context.Context, tx db.DBTX, where string, args ...any) ([]Rule, error) {
	rows, err := tx.Query(ctx, selectRules+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// List is a project's rules, or with an empty key the organization's own.
func (s *Service) List(ctx context.Context, projectKey string) ([]Rule, error) {
	var out []Rule
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if projectKey == "" {
			out, err = s.readRules(ctx, tx, `WHERE r.project_id IS NULL ORDER BY lower(r.name)`)
		} else {
			out, err = s.readRules(ctx, tx, `WHERE p.key = $1 ORDER BY lower(r.name)`, projectKey)
		}
		return err
	})
	return out, err
}

// Get is one rule.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Rule, error) {
	var out *Rule
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanRule(tx.QueryRow(ctx, selectRules+` WHERE r.id = $1`, id))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return out, err
}

// Create makes a rule for a project, or for the organization with an empty key.
func (s *Service) Create(ctx context.Context, projectKey string, in Input, actor uuid.UUID) (*Rule, db.LSN, error) {
	if err := in.Validate(); err != nil {
		return nil, 0, err
	}
	r := Rule{Name: strings.TrimSpace(in.Name), Enabled: true, Trigger: in.Trigger, Conditions: in.Conditions, Actions: in.Actions, AllowOwnEvents: in.AllowOwnEvents, HourlyCap: in.HourlyCap}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if r.HourlyCap == 0 {
		r.HourlyCap = DefaultHourlyCap
	}
	if r.Trigger.Kind == TriggerIncoming && r.Trigger.Token == "" {
		token, _, err := auth.GenerateToken()
		if err != nil {
			return nil, 0, err
		}
		r.Trigger.Token = token
	}
	trigger, conditions, actions := r.marshal()
	var out *Rule
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID *uuid.UUID
		if projectKey != "" {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, projectKey).Scan(&id); err != nil {
				return errors.New("there is no such project")
			}
			projectID = &id
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO automation_rule (org_id, project_id, name, enabled, trigger, conditions, actions, allow_own_events, hourly_cap, created_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			projectID, r.Name, r.Enabled, trigger, conditions, actions, r.AllowOwnEvents, r.HourlyCap, actor).Scan(&id)
		if err != nil && strings.Contains(err.Error(), "automation_rule_name_idx") {
			return errors.New("a rule with that name is already here")
		}
		if err != nil {
			return err
		}
		out, err = scanRule(tx.QueryRow(ctx, selectRules+` WHERE r.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// Update rewrites a rule.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Rule, db.LSN, error) {
	if err := in.Validate(); err != nil {
		return nil, 0, err
	}
	var out *Rule
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanRule(tx.QueryRow(ctx, selectRules+` WHERE r.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		next := *current
		next.Name, next.Trigger, next.Conditions, next.Actions, next.AllowOwnEvents = strings.TrimSpace(in.Name), in.Trigger, in.Conditions, in.Actions, in.AllowOwnEvents
		if in.Enabled != nil {
			next.Enabled = *in.Enabled
		}
		if in.HourlyCap > 0 {
			next.HourlyCap = in.HourlyCap
		}
		// An incoming hook keeps its address across edits.
		if next.Trigger.Kind == TriggerIncoming {
			if current.Trigger.Token != "" {
				next.Trigger.Token = current.Trigger.Token
			} else if next.Trigger.Token == "" {
				token, _, err := auth.GenerateToken()
				if err != nil {
					return err
				}
				next.Trigger.Token = token
			}
		} else {
			next.Trigger.Token = ""
		}
		trigger, conditions, actions := next.marshal()
		_, err = tx.Exec(ctx, `
			UPDATE automation_rule SET name = $2, enabled = $3, trigger = $4, conditions = $5, actions = $6, allow_own_events = $7, hourly_cap = $8
			WHERE id = $1`, id, next.Name, next.Enabled, trigger, conditions, actions, next.AllowOwnEvents, next.HourlyCap)
		if err != nil && strings.Contains(err.Error(), "automation_rule_name_idx") {
			return errors.New("a rule with that name is already here")
		}
		if err != nil {
			return err
		}
		out, err = scanRule(tx.QueryRow(ctx, selectRules+` WHERE r.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// Delete removes a rule and its log.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM automation_rule WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

const selectRuns = `
SELECT ru.id, ru.rule_id, ru.event_id, COALESCE(p.key || '-' || i.key_num, ''), ru.started_at, ru.finished_at, ru.outcome, ru.reason, ru.actions
FROM automation_run ru
LEFT JOIN issue i ON i.id = ru.issue_id
LEFT JOIN project p ON p.id = i.project_id`

func scanRun(row pgx.Row) (*Run, error) {
	var (
		r       Run
		actions []byte
	)
	if err := row.Scan(&r.ID, &r.RuleID, &r.EventID, &r.IssueKey, &r.StartedAt, &r.FinishedAt, &r.Outcome, &r.Reason, &actions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(actions, &r.Actions); err != nil {
		return nil, err
	}
	if r.Actions == nil {
		r.Actions = []ActionResult{}
	}
	return &r, nil
}

// Runs is a rule's log, newest first.
func (s *Service) Runs(ctx context.Context, ruleID uuid.UUID, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = DefaultRuns
	}
	out := []Run{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM automation_rule WHERE id = $1)`, ruleID).Scan(&found); err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, selectRuns+` WHERE ru.rule_id = $1 ORDER BY ru.started_at DESC LIMIT $2`, ruleID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRun(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

// ByToken finds the rule behind an incoming hook's address, whichever
// organization owns it, since the caller has no session.
func (s *Service) ByToken(ctx context.Context, token string) (*Rule, uuid.UUID, error) {
	var (
		out   *Rule
		orgID uuid.UUID
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT r.org_id FROM automation_rule r WHERE r.enabled AND r.trigger->>'kind' = $1 AND r.trigger->>'token' = $2 LIMIT 1`, TriggerIncoming, token)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return ErrNotFound
		}
		if err := rows.Scan(&orgID); err != nil {
			return err
		}
		rows.Close()
		r, err := scanRule(tx.QueryRow(ctx, selectRules+` WHERE r.trigger->>'token' = $1`, token))
		if err != nil {
			return err
		}
		out = r
		return nil
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	return out, orgID, nil
}

// automationActor is who a rule acts as: the organization's automation account.
func (s *Service) automationActor(ctx context.Context, tx db.DBTX) (issue.Actor, error) {
	var id *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT automation_user_id FROM org WHERE id = current_org_id()`).Scan(&id); err != nil {
		return issue.Actor{}, err
	}
	if id == nil {
		return issue.Actor{}, errors.New("this organization has no automation account")
	}
	return issue.Actor{UserID: *id, OrgRole: auth.RoleMember}, nil
}

func fmtReason(format string, args ...any) string { return fmt.Sprintf(format, args...) }
