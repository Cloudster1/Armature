// Package audit is the organization's record of who did what to it: the
// administrative events copied off the stream, plus the acts the stream never
// sees because they happen before or beside a tenant, such as a sign-in.
package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/tenant"
)

// MaxPage caps one page of the log, since an export exists for the rest.
const MaxPage = 200

// Entry is one act to record.
type Entry struct {
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Actor      uuid.UUID
	Data       map[string]any
	IP         string
	// EventID, when set, makes the row idempotent against the stream.
	EventID *uuid.UUID
}

// Write records an entry for an organization inside the caller's transaction,
// so the act and its record cannot disagree.
func Write(ctx context.Context, tx db.DBTX, orgID uuid.UUID, e Entry) error {
	if strings.TrimSpace(e.Action) == "" {
		return errors.New("an audit entry needs an action")
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	if e.Data == nil {
		data = []byte("{}")
	}
	var actor *uuid.UUID
	if e.Actor != uuid.Nil {
		actor = &e.Actor
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (org_id, actor_user_id, action, target_type, target_id, data, ip, event_id)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::inet, $8)
		ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING`,
		orgID, actor, e.Action, e.TargetType, e.TargetID, data, e.IP, e.EventID)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

// Record is Write for a caller already inside a tenant.
func Record(ctx context.Context, tx db.DBTX, e Entry) error {
	org, ok := tenant.FromContext(ctx)
	if !ok {
		return errors.New("an audit entry needs an organization")
	}
	return Write(ctx, tx, org.ID, e)
}

// Row is one line of the log as an administrator reads it.
type Row struct {
	ID         uuid.UUID       `json:"id"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   *uuid.UUID      `json:"targetId,omitempty"`
	ActorID    *uuid.UUID      `json:"actorId,omitempty"`
	ActorName  string          `json:"actorName"`
	Data       json.RawMessage `json:"data"`
	IP         string          `json:"ip,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// Filter narrows the log; every part is optional.
type Filter struct {
	Action  string
	ActorID *uuid.UUID
	From    *time.Time
	To      *time.Time
	Before  *time.Time
	Limit   int
}

// Service reads the log.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

const selectRow = `
SELECT a.id, a.action, a.target_type, a.target_id, a.actor_user_id, COALESCE(u.name, ''), a.data, COALESCE(host(a.ip), ''), a.created_at
FROM audit_log a
LEFT JOIN app_user u ON u.id = a.actor_user_id`

func (f Filter) where() (string, []any) {
	clauses := []string{"TRUE"}
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if f.Action != "" {
		add("a.action = $%d", f.Action)
	}
	if f.ActorID != nil {
		add("a.actor_user_id = $%d", *f.ActorID)
	}
	if f.From != nil {
		add("a.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("a.created_at < $%d", *f.To)
	}
	if f.Before != nil {
		add("a.created_at < $%d", *f.Before)
	}
	return strings.Join(clauses, " AND "), args
}

// List reads one page, newest first; a page is cut at MaxPage.
func (s *Service) List(ctx context.Context, f Filter) ([]Row, error) {
	if f.Limit <= 0 || f.Limit > MaxPage {
		f.Limit = MaxPage
	}
	where, args := f.where()
	args = append(args, f.Limit)
	out := []Row{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRow+` WHERE `+where+fmt.Sprintf(` ORDER BY a.created_at DESC, a.id DESC LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

func scanRow(row pgx.Row) (*Row, error) {
	var r Row
	if err := row.Scan(&r.ID, &r.Action, &r.TargetType, &r.TargetID, &r.ActorID, &r.ActorName, &r.Data, &r.IP, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// Actions lists the distinct actions the log holds, for a filter to offer.
func (s *Service) Actions(ctx context.Context) ([]string, error) {
	out := []string{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT action FROM audit_log ORDER BY action`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a string
			if err := rows.Scan(&a); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// MaxExport bounds a CSV export; a year of an ordinary organization fits.
const MaxExport = 50000

// CSV writes the filtered log as a spreadsheet reads it, newest first.
func (s *Service) CSV(ctx context.Context, f Filter) ([]byte, error) {
	where, args := f.where()
	args = append(args, MaxExport)
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"time", "action", "actor", "target_type", "target_id", "ip", "data"})
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRow+` WHERE `+where+fmt.Sprintf(` ORDER BY a.created_at DESC, a.id DESC LIMIT $%d`, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			target := ""
			if r.TargetID != nil {
				target = r.TargetID.String()
			}
			if err := w.Write([]string{r.CreatedAt.UTC().Format(time.RFC3339), r.Action, cell(r.ActorName), r.TargetType, target, r.IP, cell(string(r.Data))}); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	w.Flush()
	return []byte(buf.String()), w.Error()
}

// Copied is the set of topics the log takes from the stream: the acts that
// change who may do what and how the organization is shaped. Issue traffic
// stays in each issue's own history.
var Copied = map[string]string{
	"role.granted":                        "role",
	"role.revoked":                        "role",
	"group.created":                       "group",
	"group.member_added":                  "group",
	"group.member_removed":                "group",
	"member.joined":                       "user",
	"project.created":                     "project",
	"project.archived":                    "project",
	"project.workflow_scheme_changed":     "project",
	"project.workflow_assignment_changed": "project",
	"project.status_posted":               "project",
	"workflow.created":                    "workflow",
	"workflow.updated":                    "workflow",
	"workflow.scheme.created":             "workflow_scheme",
	"workflow.scheme.updated":             "workflow_scheme",
	"workflow.scheme.promoted":            "workflow_scheme",
	"team.created":                        "team",
	"team.member_added":                   "team",
	"team.member_removed":                 "team",
	"version.archived":                    "version",
	"sla.breached":                        "issue",
	"vcs.repository.connected":            "repository",
	"field.promoted":                      "field",
}

// Consumer copies the administrative topics off the stream into the log.
type Consumer struct {
	db    *db.Cluster
	redis *redis.Client
	log   *slog.Logger
	group string
	block time.Duration
}

func NewConsumer(cluster *db.Cluster, rdb *redis.Client, log *slog.Logger) *Consumer {
	return &Consumer{db: cluster, redis: rdb, log: log, group: "audit", block: 5 * time.Second}
}

// Run consumes the stream until the context ends.
func (c *Consumer) Run(ctx context.Context) error {
	err := c.redis.XGroupCreateMkStream(ctx, events.StreamKey, c.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group: %w", err)
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: c.group, Consumer: "worker", Streams: []string{events.StreamKey, ">"}, Count: 50, Block: c.block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.log.Warn("audit read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				e := events.FromMessage(msg)
				hctx, done := events.Continue(ctx, e, c.group)
				err := c.Handle(hctx, e)
				done(err)
				if err != nil {
					c.log.Warn("audit could not record an event", "id", msg.ID, "error", err)
				}
				if err := c.redis.XAck(ctx, events.StreamKey, c.group, msg.ID).Err(); err != nil {
					c.log.Warn("audit could not acknowledge", "id", msg.ID, "error", err)
				}
			}
		}
	}
}

// Handle records one event when its topic is copied. Twice is once: the
// event's id is unique in the log.
func (c *Consumer) Handle(ctx context.Context, e events.Event) error {
	target, ok := Copied[e.Topic]
	if !ok || e.OrgID == uuid.Nil || e.ID == uuid.Nil {
		return nil
	}
	var p map[string]any
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return nil
	}
	entry := Entry{Action: e.Topic, TargetType: target, Data: p, EventID: &e.ID}
	if id, ok := asUUID(p["actorId"]); ok {
		entry.Actor = id
	}
	for _, key := range []string{target + "Id", "assignmentId", "issueId", "id"} {
		if id, ok := asUUID(p[key]); ok {
			entry.TargetID = &id
			break
		}
	}
	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: e.OrgID}))
	_, err := c.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return Write(ctx, tx, e.OrgID, entry)
	})
	return err
}

func asUUID(v any) (uuid.UUID, bool) {
	s, ok := v.(string)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s)
	return id, err == nil && id != uuid.Nil
}

// cell keeps a spreadsheet from reading a value as a formula. csvio.Cell is the
// same rule for issue exports, written twice to keep the packages apart.
func cell(s string) string {
	if s == "" || !strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return s
	}
	return "'" + s
}
