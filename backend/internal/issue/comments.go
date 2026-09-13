package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// ErrCommentNotFound is returned for a comment that does not exist on the issue.
var ErrCommentNotFound = errors.New("comment not found")

// ErrNotYourComment is returned when editing or deleting somebody else's
// comment without the standing to do so.
var ErrNotYourComment = errors.New("you can only change your own comments")

// AddComment posts a comment everybody on the issue can read, the customer
// included.
func (s *Service) AddComment(ctx context.Context, key string, body json.RawMessage, actor Actor) (*Comment, db.LSN, error) {
	return s.addComment(ctx, key, body, false, actor)
}

// AddNote posts an internal note: a comment between agents that a customer is
// never shown. The caller decides who counts as an agent.
func (s *Service) AddNote(ctx context.Context, key string, body json.RawMessage, actor Actor) (*Comment, db.LSN, error) {
	return s.addComment(ctx, key, body, true, actor)
}

func (s *Service) addComment(ctx context.Context, key string, body json.RawMessage, internal bool, actor Actor) (*Comment, db.LSN, error) {
	if err := validateDoc(body); err != nil {
		return nil, 0, err
	}

	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, 0, err
	}

	var comment *Comment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var issueID uuid.UUID
		var issueKey string
		err := tx.QueryRow(ctx, `
			SELECT i.id, p.key || '-' || i.key_num
			FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&issueID, &issueKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		if err := s.insertComment(ctx, tx, issueID, &actor.UserID, body, internal); err != nil {
			return err
		}

		comment, err = s.latestComment(ctx, tx, issueID, actor.UserID)
		if err != nil {
			return err
		}
		for _, o := range s.observers {
			if err := o.CommentAdded(ctx, tx, issueID, internal, actor); err != nil {
				return err
			}
		}

		// Naming somebody in a comment is asking them to look, so they are
		// made watchers before the event goes out that will tell them.
		mentions, err := s.mentionWatchers(ctx, tx, issueID, issueKey, body, actor)
		if err != nil {
			return err
		}

		return events.EmitInTenant(ctx, tx, events.TopicCommentAdded, map[string]any{
			"issueId":   issueID,
			"key":       issueKey,
			"commentId": comment.ID,
			"internal":  internal,
			"actorId":   actor.UserID,
			"mentions":  mentions,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return comment, lsn, nil
}

// insertComment writes a comment. A nil author marks it as written by the
// system, which is how a workflow post-function's comment is attributed.
func (s *Service) insertComment(ctx context.Context, tx db.DBTX, issueID uuid.UUID, authorID *uuid.UUID, body json.RawMessage, internal bool) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO issue_comment (org_id, issue_id, author_id, body, is_internal)
		VALUES (current_org_id(), $1, $2, $3, $4)`, issueID, authorID, []byte(body), internal)
	if err != nil {
		return fmt.Errorf("add comment: %w", err)
	}
	return nil
}

func (s *Service) latestComment(ctx context.Context, tx db.DBTX, issueID, authorID uuid.UUID) (*Comment, error) {
	var (
		c       Comment
		aID     *uuid.UUID
		aName   *string
		aEmail  *string
		aAvatar *string
	)
	err := tx.QueryRow(ctx, `
		SELECT c.id, c.issue_id, c.body, c.is_internal, c.created_at, c.edited_at, u.id, u.name, u.email, u.avatar_url
		FROM issue_comment c
		LEFT JOIN app_user u ON u.id = c.author_id
		WHERE c.issue_id = $1 AND c.author_id = $2
		ORDER BY c.created_at DESC LIMIT 1`, issueID, authorID,
	).Scan(&c.ID, &c.IssueID, &c.Body, &c.Internal, &c.CreatedAt, &c.EditedAt, &aID, &aName, &aEmail, &aAvatar)
	if err != nil {
		return nil, err
	}
	if aID != nil {
		c.Author = userRef(*aID, aName, aEmail, aAvatar)
	}
	return &c, nil
}

// Comments lists an issue's comments oldest first, which is how a conversation
// reads. Internal notes are left out unless asked for, and only an agent's
// caller should ask.
func (s *Service) Comments(ctx context.Context, key string, includeInternal bool) ([]Comment, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	var out []Comment
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT c.id, c.issue_id, c.body, c.is_internal, c.created_at, c.edited_at, u.id, u.name, u.email, u.avatar_url
			FROM issue_comment c
			JOIN issue i ON i.id = c.issue_id
			JOIN project p ON p.id = i.project_id
			LEFT JOIN app_user u ON u.id = c.author_id
			WHERE p.key = $1 AND i.key_num = $2 AND ($3 OR NOT c.is_internal)
			ORDER BY c.created_at`, projectKey, num, includeInternal)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				c       Comment
				aID     *uuid.UUID
				aName   *string
				aEmail  *string
				aAvatar *string
			)
			if err := rows.Scan(&c.ID, &c.IssueID, &c.Body, &c.Internal, &c.CreatedAt, &c.EditedAt, &aID, &aName, &aEmail, &aAvatar); err != nil {
				return err
			}
			if aID != nil {
				c.Author = userRef(*aID, aName, aEmail, aAvatar)
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return out, nil
}

// EditComment changes a comment's body. Only its author may do so; an
// administrator can delete but not rewrite, because silently editing somebody
// else's words is worse than removing them.
func (s *Service) EditComment(ctx context.Context, commentID uuid.UUID, body json.RawMessage, actor Actor) (*Comment, db.LSN, error) {
	if err := validateDoc(body); err != nil {
		return nil, 0, err
	}

	var comment *Comment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var authorID *uuid.UUID
		err := tx.QueryRow(ctx, `SELECT author_id FROM issue_comment WHERE id = $1`, commentID).Scan(&authorID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCommentNotFound
		}
		if err != nil {
			return err
		}
		if authorID == nil || *authorID != actor.UserID {
			return ErrNotYourComment
		}

		var (
			c      Comment
			aID    *uuid.UUID
			aName  *string
			aEmail *string
		)
		err = tx.QueryRow(ctx, `
			UPDATE issue_comment SET body = $2, edited_at = now()
			WHERE id = $1
			RETURNING id, issue_id, body, created_at, edited_at, author_id`,
			commentID, []byte(body),
		).Scan(&c.ID, &c.IssueID, &c.Body, &c.CreatedAt, &c.EditedAt, &aID)
		if err != nil {
			return err
		}
		if aID != nil {
			var aAvatar *string
			if err := tx.QueryRow(ctx, `SELECT name, email, avatar_url FROM app_user WHERE id = $1`, *aID).
				Scan(&aName, &aEmail, &aAvatar); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			c.Author = userRef(*aID, aName, aEmail, aAvatar)
		}
		comment = &c
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return comment, lsn, nil
}

// DeleteComment removes a comment. Its author may always do so; an
// organization administrator may remove anybody's.
func (s *Service) DeleteComment(ctx context.Context, commentID uuid.UUID, actor Actor) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var authorID *uuid.UUID
		err := tx.QueryRow(ctx, `SELECT author_id FROM issue_comment WHERE id = $1`, commentID).Scan(&authorID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCommentNotFound
		}
		if err != nil {
			return err
		}

		mine := authorID != nil && *authorID == actor.UserID
		if !mine && !actor.OrgRole.CanAdminister() {
			return ErrNotYourComment
		}

		_, err = tx.Exec(ctx, `DELETE FROM issue_comment WHERE id = $1`, commentID)
		return err
	})
}

// History returns the changelog, newest first.
func (s *Service) History(ctx context.Context, key string) ([]HistoryEntry, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	var out []HistoryEntry
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT h.id, h.issue_id, h.changes, h.created_at, u.id, u.name, u.email, u.avatar_url
			FROM issue_history h
			JOIN issue i ON i.id = h.issue_id
			JOIN project p ON p.id = i.project_id
			LEFT JOIN app_user u ON u.id = h.actor_id
			WHERE p.key = $1 AND i.key_num = $2
			ORDER BY h.created_at DESC, h.id DESC`, projectKey, num)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				e       HistoryEntry
				raw     []byte
				aID     *uuid.UUID
				aName   *string
				aEmail  *string
				aAvatar *string
			)
			if err := rows.Scan(&e.ID, &e.IssueID, &raw, &e.CreatedAt, &aID, &aName, &aEmail, &aAvatar); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &e.Changes); err != nil {
				return fmt.Errorf("decode changelog entry %s: %w", e.ID, err)
			}
			if aID != nil {
				e.Actor = userRef(*aID, aName, aEmail, aAvatar)
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	return out, nil
}

// TextDocument wraps plain text in the minimal rich text document shape, so
// that comments written by the system, or by a client with no editor yet, have
// the same structure as ones typed by a person and the client needs only one
// renderer.
func TextDocument(text string) json.RawMessage { return plainTextDoc(text) }

func plainTextDoc(text string) json.RawMessage {
	doc := map[string]any{
		"type": "doc",
		"content": []any{
			map[string]any{
				"type":    "paragraph",
				"content": []any{map[string]any{"type": "text", "text": text}},
			},
		},
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		// The structure is fixed and contains only a caller-supplied string,
		// so this cannot fail in practice.
		return json.RawMessage(`{"type":"doc","content":[]}`)
	}
	return encoded
}

// validateDoc checks that a comment body is a document the client can show,
// and that it says something.
func validateDoc(body json.RawMessage) error {
	if len(body) == 0 {
		return errors.New("a comment needs a body")
	}
	if err := ValidateDocument(body, "comment"); err != nil {
		return err
	}
	if IsEmptyDocument(body) {
		return ErrEmptyDocument
	}
	return nil
}

// mentionWatchers adds everyone a document names as a watcher and returns
// them: mention nodes by id, and "@Name" in the text for what was typed
// plain. A stale or foreign id names nobody.
func (s *Service) mentionWatchers(ctx context.Context, tx db.DBTX, issueID uuid.UUID, issueKey string, body json.RawMessage, actor Actor) ([]uuid.UUID, error) {
	text := PlainText(body)
	nodes := MentionIDs(body)
	if !strings.Contains(text, "@") && len(nodes) == 0 {
		return []uuid.UUID{}, nil
	}
	members, err := s.members(ctx, tx)
	if err != nil {
		return nil, err
	}
	mentioned := ParseMentions(text, members)
	known := map[uuid.UUID]bool{}
	for _, m := range members {
		known[m.ID] = true
	}
	for _, id := range nodes {
		if known[id] && !slices.Contains(mentioned, id) {
			mentioned = append(mentioned, id)
		}
	}
	for _, id := range mentioned {
		if _, err := s.watch(ctx, tx, issueID, issueKey, id, actor); err != nil {
			return nil, err
		}
	}
	if mentioned == nil {
		mentioned = []uuid.UUID{}
	}
	return mentioned, nil
}
