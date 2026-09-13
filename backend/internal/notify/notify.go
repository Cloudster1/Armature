// Package notify tells people about the work they are on. One fan-out reads
// the event stream and writes an inbox row per person per reason; mail is a
// copy of that row, sent at once or bundled, as the person prefers.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// Kinds are the reasons a person is told, and the keys of their preferences.
const (
	KindAssigned     = "assigned"
	KindMentioned    = "mentioned"
	KindCommented    = "commented"
	KindTransitioned = "transitioned"
	KindWatching     = "watching"
	KindSLABreached  = "sla_breached"
	KindRule         = "rule"
	KindFilter       = "filter"
)

// Kinds lists every reason, in the order a preferences page shows them.
var Kinds = []string{KindAssigned, KindMentioned, KindCommented, KindTransitioned, KindWatching, KindSLABreached, KindRule, KindFilter}

// Digest schedules: mail at once, or bundled into one mail an hour or a day.
const (
	DigestOff    = "off"
	DigestHourly = "hourly"
	DigestDaily  = "daily"
)

// DefaultInboxLimit is how many rows the inbox shows when the caller says nothing.
const DefaultInboxLimit = 50

// MaxInboxLimit caps one page so a slow inbox stays a paging problem, not an outage.
const MaxInboxLimit = 200

// Notification is one thing a person was told.
type Notification struct {
	ID        uuid.UUID  `json:"id"`
	IssueKey  string     `json:"issueKey,omitempty"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Link      string     `json:"link,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}

// Preferences say how a person hears. A kind absent from a map is on; only a
// false turns it off, so a new kind reaches everyone until they say otherwise.
type Preferences struct {
	Mail     map[string]bool `json:"mail"`
	InApp    map[string]bool `json:"inapp"`
	Digest   string          `json:"digest"`
	WatchOwn bool            `json:"watchOwn"`
}

// DefaultPreferences is what an absent row means.
func DefaultPreferences() Preferences {
	return Preferences{Mail: map[string]bool{}, InApp: map[string]bool{}, Digest: DigestOff, WatchOwn: true}
}

func (p Preferences) mails(kind string) bool {
	on, set := p.Mail[kind]
	return !set || on
}

func (p Preferences) shows(kind string) bool {
	on, set := p.InApp[kind]
	return !set || on
}

// Validate refuses a schedule or a kind the product does not know.
func (p Preferences) Validate() error {
	switch p.Digest {
	case DigestOff, DigestHourly, DigestDaily:
	default:
		return errors.New("that is not a digest schedule. Use off, hourly or daily")
	}
	known := map[string]bool{}
	for _, k := range Kinds {
		known[k] = true
	}
	for _, m := range []map[string]bool{p.Mail, p.InApp} {
		for k := range m {
			if !known[k] {
				return fmt.Errorf("%q is not a kind of notification", k)
			}
		}
	}
	return nil
}

// Service reads and marks a person's inbox and keeps their preferences.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectNotifications = `
SELECT n.id, COALESCE(p.key || '-' || i.key_num, ''), n.kind, n.title, n.body, n.link, n.created_at, n.read_at
FROM notification n
LEFT JOIN issue i ON i.id = n.issue_id
LEFT JOIN project p ON p.id = i.project_id
WHERE n.user_id = $1`

// Inbox lists what a person was told, newest first.
func (s *Service) Inbox(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = DefaultInboxLimit
	}
	if limit > MaxInboxLimit {
		limit = MaxInboxLimit
	}
	query := selectNotifications
	if unreadOnly {
		query += ` AND n.read_at IS NULL`
	}
	query += ` ORDER BY n.created_at DESC LIMIT $2`
	out := []Notification{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, query, userID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n Notification
			if err := rows.Scan(&n.ID, &n.IssueKey, &n.Kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &n.ReadAt); err != nil {
				return err
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	return out, err
}

// Unread counts what a person has not opened yet.
func (s *Service) Unread(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notification WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	})
	return n, err
}

// MarkRead marks the given rows read; only the person's own rows are touched.
func (s *Service) MarkRead(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE notification SET read_at = now() WHERE user_id = $1 AND id = ANY($2) AND read_at IS NULL`, userID, ids)
		return err
	})
}

// MarkAllRead clears the badge.
func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE notification SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
		return err
	})
}

// Preferences reads a person's settings, or the defaults when they never saved any.
func (s *Service) Preferences(ctx context.Context, userID uuid.UUID) (Preferences, error) {
	var p Preferences
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return readPreferences(ctx, tx, userID, &p)
	})
	return p, err
}

func readPreferences(ctx context.Context, tx db.DBTX, userID uuid.UUID, p *Preferences) error {
	var mailRaw, inappRaw []byte
	err := tx.QueryRow(ctx, `SELECT mail, inapp, digest, watch_own FROM notification_preference WHERE user_id = $1`, userID).
		Scan(&mailRaw, &inappRaw, &p.Digest, &p.WatchOwn)
	if errors.Is(err, pgx.ErrNoRows) {
		*p = DefaultPreferences()
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(mailRaw, &p.Mail); err != nil {
		return err
	}
	return json.Unmarshal(inappRaw, &p.InApp)
}

// SavePreferences stores how a person wants to hear.
func (s *Service) SavePreferences(ctx context.Context, userID uuid.UUID, p Preferences) (db.LSN, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if p.Mail == nil {
		p.Mail = map[string]bool{}
	}
	if p.InApp == nil {
		p.InApp = map[string]bool{}
	}
	mailRaw, _ := json.Marshal(p.Mail)
	inappRaw, _ := json.Marshal(p.InApp)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO notification_preference (org_id, user_id, mail, inapp, digest, watch_own)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (org_id, user_id) DO UPDATE SET mail = EXCLUDED.mail, inapp = EXCLUDED.inapp, digest = EXCLUDED.digest, watch_own = EXCLUDED.watch_own`,
			userID, mailRaw, inappRaw, p.Digest, p.WatchOwn)
		return err
	})
}
