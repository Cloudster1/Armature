package privacy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

var (
	// ErrLastOwner is returned when erasing or removing somebody would leave
	// an organization with nobody who owns it.
	ErrLastOwner = errors.New("the last owner of an organization cannot go")
	// ErrNotAMember is returned when the person is not in the organization.
	ErrNotAMember = errors.New("that person is not a member here")
	// ErrNotFound is returned for a person who does not exist or is already gone.
	ErrNotFound = errors.New("no such person")
	// ErrOwnerRemovesOwner is returned when somebody who is not an owner tries
	// to let an owner go.
	ErrOwnerRemovesOwner = errors.New("only an owner can let another owner go")
)

// LastOwnerError names the organization that would be left without an owner.
type LastOwnerError struct{ Org string }

func (e *LastOwnerError) Error() string {
	return fmt.Sprintf("%s would be left without an owner", e.Org)
}
func (e *LastOwnerError) Is(target error) bool { return target == ErrLastOwner }

// Avatars is the one thing outside the database an erasure has to reach.
type Avatars interface {
	Remove(ctx context.Context, userID uuid.UUID) error
}

// Themes takes a person's themes and their files out with them.
type Themes interface {
	EraseOwner(ctx context.Context, userID uuid.UUID) error
}

// Service serves a person's rights over their data.
type Service struct {
	db      *db.Cluster
	avatars Avatars
	themes  Themes
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// WithAvatars lets an erasure take the picture out of the bucket too.
func (s *Service) WithAvatars(a Avatars) *Service {
	s.avatars = a
	return s
}

// WithThemes lets an erasure take the person's themes and their files too.
func (s *Service) WithThemes(t Themes) *Service {
	s.themes = t
	return s
}

// ErasedName is what an erased person is called wherever a name is joined.
const ErasedName = "Former user"

// tombstoneEmail is unique per person, so two erasures never collide, and at
// a reserved domain, so it can never be mailed or signed in with.
func tombstoneEmail(id uuid.UUID) string { return "erased+" + id.String() + "@erased.invalid" }

// Export is everything the system holds about one person, in their words:
// what they are, where they belong, what they did.
type Export struct {
	ExportedAt    time.Time      `json:"exportedAt"`
	Profile       Profile        `json:"profile"`
	Memberships   []Membership   `json:"memberships"`
	Sessions      []Session      `json:"sessions"`
	APITokens     []Token        `json:"apiTokens"`
	Issues        []IssueRef     `json:"issues"`
	Comments      []Comment      `json:"comments"`
	Worklogs      []Worklog      `json:"worklogs"`
	Watching      []IssueRef     `json:"watching"`
	Notifications []Notification `json:"notifications"`
	SavedFilters  []SavedFilter  `json:"savedFilters"`
	AuditActions  []AuditAction  `json:"auditActions"`
}

type Profile struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Timezone  string    `json:"timezone"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"createdAt"`
}

type Membership struct {
	Organization string   `json:"organization"`
	Slug         string   `json:"slug"`
	Role         string   `json:"role"`
	Groups       []string `json:"groups"`
	Teams        []string `json:"teams"`
	Roles        []string `json:"roles"`
}

type Session struct {
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	UserAgent  string    `json:"userAgent"`
	IP         string    `json:"ip"`
}

type Token struct {
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type IssueRef struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	// Relation says why the issue is here: reporter, assignee or watcher.
	Relation string `json:"relation,omitempty"`
}

type Comment struct {
	IssueKey  string    `json:"issueKey"`
	CreatedAt time.Time `json:"createdAt"`
	Text      string    `json:"text"`
}

type Worklog struct {
	IssueKey  string `json:"issueKey"`
	StartedOn string `json:"startedOn"`
	Minutes   int    `json:"minutes"`
	Note      string `json:"note"`
}

type Notification struct {
	CreatedAt time.Time `json:"createdAt"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
}

type SavedFilter struct {
	Name   string `json:"name"`
	Query  string `json:"query"`
	Shared bool   `json:"shared"`
}

type AuditAction struct {
	At         time.Time `json:"at"`
	Action     string    `json:"action"`
	TargetType string    `json:"targetType"`
	TargetID   string    `json:"targetId,omitempty"`
}

// Export gathers one person's data across every organization they belong to.
// It reads as the admin because the person, not a tenant, is the subject.
func (s *Service) Export(ctx context.Context, userID uuid.UUID) (*Export, error) {
	out := &Export{ExportedAt: time.Now().UTC()}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT id, email::text, name, timezone, locale, created_at FROM app_user WHERE id = $1 AND erased_at IS NULL`, userID).
			Scan(&out.Profile.ID, &out.Profile.Email, &out.Profile.Name, &out.Profile.Timezone, &out.Profile.Locale, &out.Profile.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Memberships, err = collect(ctx, tx, `
			SELECT o.name, o.slug::text, m.org_role,
			       ARRAY(SELECT g.name FROM group_member gm JOIN user_group g ON g.id = gm.group_id WHERE gm.user_id = m.user_id AND gm.org_id = m.org_id ORDER BY g.name),
			       ARRAY(SELECT t.name FROM team_member tm JOIN team t ON t.id = tm.team_id WHERE tm.user_id = m.user_id AND tm.org_id = m.org_id ORDER BY t.name),
			       ARRAY(SELECT r.role::text || COALESCE(' on ' || p.key, '') FROM role_assignment r LEFT JOIN project p ON p.id = r.project_id WHERE r.user_id = m.user_id AND r.org_id = m.org_id ORDER BY 1)
			FROM org_member m JOIN org o ON o.id = m.org_id WHERE m.user_id = $1 ORDER BY o.name`, userID,
			func(row pgx.Rows) (Membership, error) {
				var m Membership
				err := row.Scan(&m.Organization, &m.Slug, &m.Role, &m.Groups, &m.Teams, &m.Roles)
				return m, err
			})
		if err != nil {
			return err
		}
		out.Sessions, err = collect(ctx, tx, `SELECT created_at, last_seen_at, expires_at, COALESCE(user_agent, ''), COALESCE(host(ip), '') FROM user_session WHERE user_id = $1 ORDER BY created_at`, userID,
			func(row pgx.Rows) (Session, error) {
				var x Session
				err := row.Scan(&x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt, &x.UserAgent, &x.IP)
				return x, err
			})
		if err != nil {
			return err
		}
		out.APITokens, err = collect(ctx, tx, `SELECT name, scopes, created_at, last_used_at, expires_at FROM api_token WHERE user_id = $1 ORDER BY created_at`, userID,
			func(row pgx.Rows) (Token, error) {
				var x Token
				err := row.Scan(&x.Name, &x.Scopes, &x.CreatedAt, &x.LastUsedAt, &x.ExpiresAt)
				return x, err
			})
		if err != nil {
			return err
		}
		out.Issues, err = collect(ctx, tx, `
			SELECT p.key || '-' || i.key_num, i.summary, CASE WHEN i.reporter_id = $1 AND i.assignee_id = $1 THEN 'reporter and assignee' WHEN i.reporter_id = $1 THEN 'reporter' ELSE 'assignee' END
			FROM issue i JOIN project p ON p.id = i.project_id WHERE i.reporter_id = $1 OR i.assignee_id = $1 ORDER BY i.created_at`, userID, scanIssueRef)
		if err != nil {
			return err
		}
		out.Comments, err = collect(ctx, tx, `SELECT p.key || '-' || i.key_num, c.created_at, c.body FROM issue_comment c JOIN issue i ON i.id = c.issue_id JOIN project p ON p.id = i.project_id WHERE c.author_id = $1 ORDER BY c.created_at`, userID,
			func(row pgx.Rows) (Comment, error) {
				var x Comment
				var body []byte
				if err := row.Scan(&x.IssueKey, &x.CreatedAt, &body); err != nil {
					return x, err
				}
				x.Text = issue.PlainText(body)
				return x, nil
			})
		if err != nil {
			return err
		}
		out.Worklogs, err = collect(ctx, tx, `SELECT p.key || '-' || i.key_num, to_char(w.started_on, 'YYYY-MM-DD'), w.minutes, w.note FROM issue_worklog w JOIN issue i ON i.id = w.issue_id JOIN project p ON p.id = i.project_id WHERE w.author_id = $1 ORDER BY w.started_on`, userID,
			func(row pgx.Rows) (Worklog, error) {
				var x Worklog
				err := row.Scan(&x.IssueKey, &x.StartedOn, &x.Minutes, &x.Note)
				return x, err
			})
		if err != nil {
			return err
		}
		out.Watching, err = collect(ctx, tx, `SELECT p.key || '-' || i.key_num, i.summary, 'watcher' FROM issue_watcher w JOIN issue i ON i.id = w.issue_id JOIN project p ON p.id = i.project_id WHERE w.user_id = $1 ORDER BY p.key, i.key_num`, userID, scanIssueRef)
		if err != nil {
			return err
		}
		out.Notifications, err = collect(ctx, tx, `SELECT created_at, kind, title FROM notification WHERE user_id = $1 ORDER BY created_at`, userID,
			func(row pgx.Rows) (Notification, error) {
				var x Notification
				err := row.Scan(&x.CreatedAt, &x.Kind, &x.Title)
				return x, err
			})
		if err != nil {
			return err
		}
		out.SavedFilters, err = collect(ctx, tx, `SELECT name, query, shared FROM saved_filter WHERE owner_id = $1 ORDER BY name`, userID,
			func(row pgx.Rows) (SavedFilter, error) {
				var x SavedFilter
				err := row.Scan(&x.Name, &x.Query, &x.Shared)
				return x, err
			})
		if err != nil {
			return err
		}
		out.AuditActions, err = collect(ctx, tx, `SELECT created_at, action, target_type, COALESCE(target_id::text, '') FROM audit_log WHERE actor_user_id = $1 ORDER BY created_at`, userID,
			func(row pgx.Rows) (AuditAction, error) {
				var x AuditAction
				err := row.Scan(&x.At, &x.Action, &x.TargetType, &x.TargetID)
				return x, err
			})
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanIssueRef(row pgx.Rows) (IssueRef, error) {
	var x IssueRef
	err := row.Scan(&x.Key, &x.Summary, &x.Relation)
	return x, err
}

// collect runs a query and scans every row with fn; an empty result is an
// empty slice rather than null, so the file reads the same either way.
func collect[T any](ctx context.Context, tx db.DBTX, query string, arg any, fn func(pgx.Rows) (T, error)) ([]T, error) {
	rows, err := tx.Query(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		item, err := fn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// soleOwnerOf names an organization the person alone owns, or "" when none.
func soleOwnerOf(ctx context.Context, tx db.DBTX, userID uuid.UUID, orgID *uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `
		SELECT o.name FROM org_member m JOIN org o ON o.id = m.org_id
		WHERE m.user_id = $1 AND m.org_role = 'owner' AND ($2::uuid IS NULL OR m.org_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM org_member x JOIN app_user xu ON xu.id = x.user_id
		                  WHERE x.org_id = m.org_id AND x.org_role = 'owner' AND x.user_id <> $1 AND xu.is_active)
		ORDER BY o.name LIMIT 1`, userID, orgID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// Erase takes a person's identity out and leaves their work in: the account
// becomes a tombstone that every join reads as "Former user", what only they
// could use goes, and what the team wrote together stays attributed to the
// tombstone. One transaction, so a half-erased person cannot exist.
func (s *Service) Erase(ctx context.Context, userID uuid.UUID, actor uuid.UUID) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var email string
		err := tx.QueryRow(ctx, `SELECT email::text FROM app_user WHERE id = $1 AND erased_at IS NULL FOR UPDATE`, userID).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if org, err := soleOwnerOf(ctx, tx, userID, nil); err != nil {
			return err
		} else if org != "" {
			return &LastOwnerError{Org: org}
		}
		// The record of the erasure goes to every organization the person
		// was in, before the membership that would have named them goes.
		orgs, err := collect(ctx, tx, `SELECT org_id FROM org_member WHERE user_id = $1`, userID, func(row pgx.Rows) (uuid.UUID, error) {
			var id uuid.UUID
			err := row.Scan(&id)
			return id, err
		})
		if err != nil {
			return err
		}
		for _, org := range orgs {
			if err := audit.Write(ctx, tx, org, audit.Entry{Action: "user.erased", TargetType: "user", TargetID: &userID, Actor: actor}); err != nil {
				return err
			}
		}
		for _, q := range []string{
			`DELETE FROM user_session WHERE user_id = $1`,
			`DELETE FROM api_token WHERE user_id = $1`,
			`DELETE FROM notification WHERE user_id = $1`,
			`DELETE FROM notification_preference WHERE user_id = $1`,
			`DELETE FROM notification_digest WHERE user_id = $1`,
			`DELETE FROM saved_filter_subscription WHERE user_id = $1`,
			`DELETE FROM saved_filter_star WHERE user_id = $1`,
			`DELETE FROM saved_filter WHERE owner_id = $1`,
			`DELETE FROM role_assignment WHERE user_id = $1`,
			`DELETE FROM group_member WHERE user_id = $1`,
			`DELETE FROM team_member WHERE user_id = $1`,
			`DELETE FROM org_member WHERE user_id = $1`,
		} {
			if _, err := tx.Exec(ctx, q, userID); err != nil {
				return fmt.Errorf("erase: %w", err)
			}
		}
		tomb := tombstoneEmail(userID)
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM org_invite WHERE email = $1`, []any{email}},
			{`DELETE FROM portal_code WHERE email = $1`, []any{email}},
			{`UPDATE inbound_mail SET from_email = $2 WHERE from_email = $1`, []any{email, tomb}},
			{`UPDATE git_commit SET author_email = '', author_name = $2 WHERE author_email = $1`, []any{email, ErasedName}},
		} {
			if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
				return fmt.Errorf("erase: %w", err)
			}
		}
		_, err = tx.Exec(ctx, `
			UPDATE app_user SET email = $2, name = $3, password_hash = NULL, avatar_url = NULL,
			       timezone = 'UTC', locale = 'en', is_active = false, erased_at = now()
			WHERE id = $1`, userID, tomb, ErasedName)
		return err
	})
	if err != nil {
		return err
	}
	if s.avatars != nil {
		if err := s.avatars.Remove(ctx, userID); err != nil {
			return fmt.Errorf("remove the picture: %w", err)
		}
	}
	if s.themes != nil {
		if err := s.themes.EraseOwner(ctx, userID); err != nil {
			return fmt.Errorf("remove the themes: %w", err)
		}
	}
	return nil
}

// RemoveMember lets an organization go of a person: the membership and what
// hung off it go, the account stays theirs for their other organizations.
func (s *Service) RemoveMember(ctx context.Context, orgID, userID, actor uuid.UUID) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `SELECT 1 FROM org_member WHERE org_id = $1 AND user_id = $2`, orgID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotAMember
		}
		if org, err := soleOwnerOf(ctx, tx, userID, &orgID); err != nil {
			return err
		} else if org != "" {
			return &LastOwnerError{Org: org}
		}
		// An owner answers for the organization, so only an owner lets one go.
		var theirs, mine string
		if err := tx.QueryRow(ctx, `
			SELECT max(org_role) FILTER (WHERE user_id = $2), max(org_role) FILTER (WHERE user_id = $3)
			FROM org_member WHERE org_id = $1 AND user_id IN ($2, $3)`, orgID, userID, actor).Scan(&theirs, &mine); err != nil {
			return err
		}
		if theirs == "owner" && mine != "owner" {
			return ErrOwnerRemovesOwner
		}
		if err := audit.Write(ctx, tx, orgID, audit.Entry{Action: "member.removed", TargetType: "user", TargetID: &userID, Actor: actor}); err != nil {
			return err
		}
		for _, q := range []string{
			`DELETE FROM api_token WHERE org_id = $1 AND user_id = $2`,
			`UPDATE user_session SET current_org_id = NULL, portal_project_id = NULL WHERE user_id = $2 AND current_org_id = $1`,
			`DELETE FROM role_assignment WHERE org_id = $1 AND user_id = $2`,
			`DELETE FROM group_member WHERE org_id = $1 AND user_id = $2`,
			`DELETE FROM team_member WHERE org_id = $1 AND user_id = $2`,
			`DELETE FROM notification WHERE org_id = $1 AND user_id = $2`,
			// What would keep telling them about work here once they are gone.
			`DELETE FROM saved_filter_subscription WHERE org_id = $1 AND user_id = $2`,
			`DELETE FROM issue_watcher WHERE org_id = $1 AND user_id = $2`,
			`DELETE FROM org_member WHERE org_id = $1 AND user_id = $2`,
		} {
			if _, err := tx.Exec(ctx, q, orgID, userID); err != nil {
				return fmt.Errorf("remove member: %w", err)
			}
		}
		return nil
	})
}
