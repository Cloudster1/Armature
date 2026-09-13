package issue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/tenant"
)

// Watcher is somebody who is told about an issue. A mail address is enough to
// be one: the person behind it is a user row, with or without a password.
type Watcher struct {
	UserID  uuid.UUID  `json:"userId"`
	Name    string     `json:"name"`
	Email   string     `json:"email"`
	AddedBy *uuid.UUID `json:"addedBy,omitempty"`
	AddedAt time.Time  `json:"addedAt"`
}

// ErrNotWatching is returned when a watcher to remove is not there.
var ErrNotWatching = errors.New("that person is not watching this issue")

const selectWatchers = `
SELECT w.user_id, u.name, u.email, w.added_by, w.created_at
FROM issue_watcher w
JOIN app_user u ON u.id = w.user_id
WHERE w.issue_id = $1`

const watchersInOrder = selectWatchers + ` ORDER BY w.created_at, u.name`

// Watchers lists who is told about an issue, in the order they were added.
func (s *Service) Watchers(ctx context.Context, key string) ([]Watcher, error) {
	out := []Watcher{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, err := s.idByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, watchersInOrder, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var w Watcher
			if err := rows.Scan(&w.UserID, &w.Name, &w.Email, &w.AddedBy, &w.AddedAt); err != nil {
				return err
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, err
}

// Watches reports whether one person is a watcher of an issue.
func (s *Service) Watches(ctx context.Context, key string, userID uuid.UUID) (bool, error) {
	var found bool
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, err := s.idByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_watcher WHERE issue_id = $1 AND user_id = $2)`, id, userID).Scan(&found)
	})
	return found, err
}

// AddWatcher makes somebody a watcher and returns them with the plain token
// that stops the watching, which exists only here and in the mail that
// carries it. Adding somebody who already watches changes nothing and returns
// no token. The token rides in the event for the notifier; it can do nothing
// but end the watching, which is why it may.
func (s *Service) AddWatcher(ctx context.Context, key string, userID uuid.UUID, actor Actor) (*Watcher, string, db.LSN, error) {
	var (
		out   *Watcher
		token string
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		issueID, issueKey, err := s.idAndKey(ctx, tx, key)
		if err != nil {
			return err
		}
		secret, err := s.watch(ctx, tx, issueID, issueKey, userID, actor)
		if err != nil {
			return err
		}
		var w Watcher
		err = tx.QueryRow(ctx, selectWatchers+` AND w.user_id = $2`, issueID, userID).Scan(&w.UserID, &w.Name, &w.Email, &w.AddedBy, &w.AddedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("there is no such person to add")
		}
		if err != nil {
			return err
		}
		out = &w
		token = secret
		return nil
	})
	if err != nil {
		return nil, "", 0, err
	}
	return out, token, lsn, nil
}

// watch inserts the watcher row inside a transaction and emits the event; a
// mention in a comment adds watchers the same way a button does. The plain
// token comes back only when a row was made.
func (s *Service) watch(ctx context.Context, tx db.DBTX, issueID uuid.UUID, issueKey string, userID uuid.UUID, actor Actor) (string, error) {
	secret, digest, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO issue_watcher (org_id, issue_id, user_id, added_by, unwatch_token_hash)
		VALUES (current_org_id(), $1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, issueID, userID, actor.UserID, digest)
	if err != nil {
		return "", fmt.Errorf("add watcher: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", nil
	}
	return secret, events.EmitInTenant(ctx, tx, events.TopicWatcherAdded, map[string]any{
		"issueId": issueID, "key": issueKey, "userId": userID, "actorId": actor.UserID, "token": secret,
	})
}

// RemoveWatcher stops somebody being told.
func (s *Service) RemoveWatcher(ctx context.Context, key string, userID uuid.UUID, actor Actor) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, err := s.idByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM issue_watcher WHERE issue_id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotWatching
		}
		return nil
	})
}

// Unwatch ends a watching by the token a mail carried. Nobody is signed in,
// so the token names the organization first, and the row goes inside it.
func (s *Service) Unwatch(ctx context.Context, token string) (db.LSN, error) {
	digest := auth.HashToken(token)
	var orgID uuid.UUID
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT org_id FROM issue_watcher WHERE unwatch_token_hash = $1`, digest).Scan(&orgID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotWatching
	}
	if err != nil {
		return 0, err
	}
	return s.db.Write(db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: orgID})), func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM issue_watcher WHERE unwatch_token_hash = $1`, digest)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotWatching
		}
		return nil
	})
}

// idAndKey resolves a key inside a transaction, to the id and the key as the
// database spells it.
func (s *Service) idAndKey(ctx context.Context, tx db.DBTX, key string) (uuid.UUID, string, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return uuid.Nil, "", err
	}
	var (
		id    uuid.UUID
		found string
	)
	err = tx.QueryRow(ctx, `
		SELECT i.id, p.key || '-' || i.key_num
		FROM issue i JOIN project p ON p.id = i.project_id
		WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&id, &found)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", ErrNotFound
	}
	return id, found, err
}

func (s *Service) idByKey(ctx context.Context, tx db.DBTX, key string) (uuid.UUID, error) {
	id, _, err := s.idAndKey(ctx, tx, key)
	return id, err
}
