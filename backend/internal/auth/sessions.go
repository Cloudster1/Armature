package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
)

var (
	// ErrNoSuchSession is a session that has ended, or was never the caller's.
	ErrNoSuchSession = errors.New("that session was not found. It may have ended already")
	// ErrCurrentSession is the session in hand, which signing out ends.
	ErrCurrentSession = errors.New("that is the session you are using. Use Sign out to end it")
)

// Session is one place a person is signed in, as they see it on their profile.
type Session struct {
	ID        uuid.UUID `json:"id"`
	UserAgent string    `json:"userAgent"`
	IP        string    `json:"ip"`
	// OrgName is the organization the session acts in, empty before one is chosen.
	OrgName    string    `json:"orgName"`
	Proof      Proof     `json:"proof"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Current    bool      `json:"current"`
}

// Sessions lists a person's live sessions, newest first. user_session has no
// row level security, so the user_id filter is the whole of the wall.
func (s *Service) Sessions(ctx context.Context, userID, current uuid.UUID) ([]Session, error) {
	var out []Session
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT s.id, COALESCE(s.user_agent, ''), COALESCE(host(s.ip), ''), COALESCE(o.name, ''),
			       s.proof, s.created_at, s.last_seen_at, s.id = $2
			FROM user_session s
			LEFT JOIN org o ON o.id = s.current_org_id AND o.archived_at IS NULL
			WHERE s.user_id = $1 AND s.expires_at > now()
			ORDER BY s.created_at DESC, s.id DESC`, userID, current)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Session, error) {
			var x Session
			err := row.Scan(&x.ID, &x.UserAgent, &x.IP, &x.OrgName, &x.Proof, &x.CreatedAt, &x.LastSeenAt, &x.Current)
			return x, err
		})
		return err
	})
	return out, err
}

// EndSession signs a person out of one of their other sessions.
func (s *Service) EndSession(ctx context.Context, userID, current, sessionID uuid.UUID, ip string) (db.LSN, error) {
	if sessionID == current {
		return 0, ErrCurrentSession
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var agent string
		err := tx.QueryRow(ctx, `
			DELETE FROM user_session WHERE id = $1 AND user_id = $2
			RETURNING COALESCE(user_agent, '')`, sessionID, userID).Scan(&agent)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchSession
		}
		if err != nil {
			return fmt.Errorf("end session: %w", err)
		}
		return auditEveryOrg(ctx, tx, userID, audit.Entry{
			Action: "auth.session_ended", TargetType: "session", TargetID: &sessionID, Actor: userID, IP: ip,
			Data: map[string]any{"userAgent": agent},
		})
	})
}

// EndOtherSessions signs a person out everywhere but the session in hand,
// without the password change that used to be the only way.
func (s *Service) EndOtherSessions(ctx context.Context, userID, current uuid.UUID, ip string) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var ended int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_session WHERE user_id = $1 AND id <> $2`, userID, current).Scan(&ended); err != nil {
			return fmt.Errorf("count sessions: %w", err)
		}
		if err := endEverySession(ctx, tx, userID, &current); err != nil {
			return err
		}
		return auditEveryOrg(ctx, tx, userID, audit.Entry{
			Action: "auth.sessions_ended", TargetType: "user", TargetID: &userID, Actor: userID, IP: ip,
			Data: map[string]any{"ended": ended},
		})
	})
}
