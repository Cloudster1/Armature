package issue

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// RemoteLink is a page in another application that is about this issue. The
// application that holds the page keeps it in step, by its address.
type RemoteLink struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
	// Title is the page's own title, as the application last reported it.
	Title string `json:"title"`
	// Source names the application the page lives in.
	Source    string     `json:"source"`
	IconURL   *string    `json:"iconUrl"`
	CreatedBy *uuid.UUID `json:"createdBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// RemoteLinkInput is a page to put on an issue, or to retitle when its address
// is already there.
type RemoteLinkInput struct {
	URL     string
	Title   string
	Source  string
	IconURL string
}

// The bounds on a page link, which the table's checks repeat.
const (
	MaxRemoteLinkURL    = 2048
	MaxRemoteLinkTitle  = 255
	MaxRemoteLinkSource = 100
)

var (
	// ErrBadRemoteLink is returned for a page link that cannot be stored.
	ErrBadRemoteLink = errors.New("that page link cannot be added")
	// ErrRemoteLinkNotFound is returned for a page link that is not on the issue.
	ErrRemoteLinkNotFound = errors.New("page link not found")
)

// checkRemoteLink trims what was sent and refuses what the table would, in
// words that say how to fix it.
func checkRemoteLink(in RemoteLinkInput) (RemoteLinkInput, error) {
	in.URL = strings.TrimSpace(in.URL)
	in.Title = strings.TrimSpace(in.Title)
	in.Source = strings.TrimSpace(in.Source)
	in.IconURL = strings.TrimSpace(in.IconURL)

	if err := checkWebAddress(in.URL, "the page's address"); err != nil {
		return in, err
	}
	if in.IconURL != "" {
		if err := checkWebAddress(in.IconURL, "the icon's address"); err != nil {
			return in, err
		}
	}
	if in.Title == "" {
		return in, fmt.Errorf("%w: give the page a title", ErrBadRemoteLink)
	}
	if len([]rune(in.Title)) > MaxRemoteLinkTitle {
		return in, fmt.Errorf("%w: shorten the title to %d characters or fewer", ErrBadRemoteLink, MaxRemoteLinkTitle)
	}
	if in.Source == "" {
		return in, fmt.Errorf("%w: name the application the page lives in as its source", ErrBadRemoteLink)
	}
	if len([]rune(in.Source)) > MaxRemoteLinkSource {
		return in, fmt.Errorf("%w: shorten the source to %d characters or fewer", ErrBadRemoteLink, MaxRemoteLinkSource)
	}
	return in, nil
}

func checkWebAddress(raw, what string) error {
	if raw == "" {
		return fmt.Errorf("%w: give %s", ErrBadRemoteLink, what)
	}
	if len([]rune(raw)) > MaxRemoteLinkURL {
		return fmt.Errorf("%w: shorten %s to %d characters or fewer", ErrBadRemoteLink, what, MaxRemoteLinkURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.ContainsAny(raw, " \t\r\n") {
		return fmt.Errorf("%w: %s has to be a web address starting with http:// or https://", ErrBadRemoteLink, what)
	}
	return nil
}

const selectRemoteLink = `
SELECT id, url, title, source, icon_url, created_by, created_at, updated_at
FROM issue_remote_link`

func scanRemoteLink(row pgx.Row) (RemoteLink, error) {
	var l RemoteLink
	err := row.Scan(&l.ID, &l.URL, &l.Title, &l.Source, &l.IconURL, &l.CreatedBy, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// RemoteLinks lists the pages about an issue, oldest first.
func (s *Service) RemoteLinks(ctx context.Context, key string) ([]RemoteLink, error) {
	out := []RemoteLink{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, err := s.idByKey(ctx, tx, key)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectRemoteLink+` WHERE issue_id = $1 ORDER BY created_at, id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scanRemoteLink(rows)
			if err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PutRemoteLink puts a page on an issue, or retitles it when its address is
// already there, so a sync can resend everything; only a new page is announced.
func (s *Service) PutRemoteLink(ctx context.Context, key string, in RemoteLinkInput, actor Actor) (link *RemoteLink, created bool, lsn db.LSN, err error) {
	in, err = checkRemoteLink(in)
	if err != nil {
		return nil, false, 0, err
	}
	var icon *string
	if in.IconURL != "" {
		icon = &in.IconURL
	}
	var creator *uuid.UUID
	if actor.UserID != uuid.Nil {
		creator = &actor.UserID
	}

	lsn, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		issueID, issueKey, err := s.idAndKey(ctx, tx, key)
		if err != nil {
			return err
		}
		// xmax is zero only on a row this statement inserted, which tells a
		// new page from a retitled one without a second round trip.
		var inserted bool
		row := tx.QueryRow(ctx, `
			INSERT INTO issue_remote_link (org_id, issue_id, url, title, source, icon_url, created_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)
			ON CONFLICT (issue_id, url) DO UPDATE
				SET title = EXCLUDED.title, source = EXCLUDED.source, icon_url = EXCLUDED.icon_url
			RETURNING id, url, title, source, icon_url, created_by, created_at, updated_at, xmax = 0`,
			issueID, in.URL, in.Title, in.Source, icon, creator)
		var l RemoteLink
		if err := row.Scan(&l.ID, &l.URL, &l.Title, &l.Source, &l.IconURL, &l.CreatedBy, &l.CreatedAt, &l.UpdatedAt, &inserted); err != nil {
			return fmt.Errorf("put page link: %w", err)
		}
		link, created = &l, inserted
		if !inserted {
			return nil
		}
		return events.EmitInTenant(ctx, tx, events.TopicRemoteLinkAdded, map[string]any{
			"issueId": issueID, "key": issueKey, "linkId": l.ID, "url": l.URL, "title": l.Title,
			"source": l.Source, "actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, false, 0, err
	}
	return link, created, lsn, nil
}

// RemoveRemoteLink takes a page off an issue. The link has to be on the issue
// named, so a key cannot reach another issue's pages by their id.
func (s *Service) RemoveRemoteLink(ctx context.Context, key string, id uuid.UUID, actor Actor) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		issueID, issueKey, err := s.idAndKey(ctx, tx, key)
		if err != nil {
			return err
		}
		removed, err := scanRemoteLink(tx.QueryRow(ctx, `
			DELETE FROM issue_remote_link WHERE id = $1 AND issue_id = $2
			RETURNING id, url, title, source, icon_url, created_by, created_at, updated_at`, id, issueID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRemoteLinkNotFound
		}
		if err != nil {
			return fmt.Errorf("remove page link: %w", err)
		}
		return events.EmitInTenant(ctx, tx, events.TopicRemoteLinkRemoved, map[string]any{
			"issueId": issueID, "key": issueKey, "linkId": removed.ID, "url": removed.URL, "title": removed.Title,
			"source": removed.Source, "actorId": actor.UserID,
		})
	})
}
