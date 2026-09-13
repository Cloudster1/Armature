// Package filter owns saved filters: a query with a name, kept by the person
// who wrote it, shared with the organization when they say so, starred by
// anyone, and mailed on a schedule. Everything that uses one reads the query
// live, so editing it changes every view at once.
package filter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/nql"
)

var (
	ErrNotFound      = errors.New("saved filter not found")
	ErrDuplicateName = errors.New("you already have a saved filter with that name")
	ErrNotYours      = errors.New("only the owner can change a saved filter")
)

// Schedules a subscription may take.
const (
	Daily  = "daily"
	Weekly = "weekly"
)

// DefaultHour is when a subscription mails when nobody said otherwise.
const DefaultHour = 8

// Filter is one saved query as the page and the runner see it.
type Filter struct {
	ID        uuid.UUID `json:"id"`
	OwnerID   uuid.UUID `json:"ownerId"`
	OwnerName string    `json:"ownerName"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	Shared    bool      `json:"shared"`
	Columns   []string  `json:"columns"`
	// Starred and Subscribed are the reader's own marks.
	Starred      bool          `json:"starred"`
	Subscription *Subscription `json:"subscription,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

// Subscription is when a filter's result is mailed to one person.
type Subscription struct {
	Schedule   string     `json:"schedule"`
	Hour       int        `json:"hour"`
	Weekday    *int       `json:"weekday,omitempty"`
	LastSentAt *time.Time `json:"lastSentAt,omitempty"`
}

// Input is a filter as the page sends it; nil leaves a field alone on an edit.
type Input struct {
	Name    *string  `json:"name,omitempty"`
	Query   *string  `json:"query,omitempty"`
	Shared  *bool    `json:"shared,omitempty"`
	Columns []string `json:"columns,omitempty"`
}

// Validate refuses a schedule the sender does not keep.
func (s Subscription) Validate() error {
	switch s.Schedule {
	case Daily:
	case Weekly:
		if s.Weekday == nil || *s.Weekday < 0 || *s.Weekday > 6 {
			return errors.New("a weekly subscription needs a weekday, 0 for Sunday to 6 for Saturday")
		}
	default:
		return errors.New("a subscription is daily or weekly")
	}
	if s.Hour < 0 || s.Hour > 23 {
		return errors.New("the hour is 0 to 23")
	}
	return nil
}

type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// selectFilters reads filters with the reader's marks; $1 is the reader.
const selectFilters = `
SELECT f.id, f.owner_id, COALESCE(u.name, ''), f.name, f.query, f.shared, f.columns, f.created_at, f.updated_at,
       EXISTS (SELECT 1 FROM saved_filter_star st WHERE st.filter_id = f.id AND st.user_id = $1),
       sub.schedule, sub.hour, sub.weekday, sub.last_sent_at
FROM saved_filter f
LEFT JOIN app_user u ON u.id = f.owner_id
LEFT JOIN saved_filter_subscription sub ON sub.filter_id = f.id AND sub.user_id = $1`

func scan(row pgx.Row) (*Filter, error) {
	var (
		f        Filter
		schedule *string
		hour     *int
		weekday  *int
		lastSent *time.Time
	)
	err := row.Scan(&f.ID, &f.OwnerID, &f.OwnerName, &f.Name, &f.Query, &f.Shared, &f.Columns, &f.CreatedAt, &f.UpdatedAt, &f.Starred, &schedule, &hour, &weekday, &lastSent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if f.Columns == nil {
		f.Columns = []string{}
	}
	if schedule != nil {
		f.Subscription = &Subscription{Schedule: *schedule, Hour: *hour, Weekday: weekday, LastSentAt: lastSent}
	}
	return &f, nil
}

// visible is what a reader may see: their own filters and the shared ones.
const visible = ` (f.owner_id = $1 OR f.shared)`

// List is every filter the reader may see: theirs, and the shared ones.
func (s *Service) List(ctx context.Context, reader uuid.UUID) ([]Filter, error) {
	out := []Filter{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectFilters+` WHERE`+visible+` ORDER BY (f.owner_id <> $1), lower(f.name)`, reader)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, *f)
		}
		return rows.Err()
	})
	return out, err
}

// Get is one filter the reader may see.
func (s *Service) Get(ctx context.Context, id, reader uuid.UUID) (*Filter, error) {
	var out *Filter
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2 AND`+visible, reader, id))
		return err
	})
	return out, err
}

// parseQuery refuses a query the search would refuse, so a saved filter is
// always one that runs.
func parseQuery(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("a saved filter needs a query")
	}
	if _, err := nql.Parse(text); err != nil {
		return "", err
	}
	return text, nil
}

// Create saves a query under a name for the reader.
func (s *Service) Create(ctx context.Context, owner uuid.UUID, in Input) (*Filter, db.LSN, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return nil, 0, errors.New("a saved filter needs a name")
	}
	query := ""
	if in.Query != nil {
		query = *in.Query
	}
	query, err := parseQuery(query)
	if err != nil {
		return nil, 0, err
	}
	shared := in.Shared != nil && *in.Shared
	columns := in.Columns
	if columns == nil {
		columns = []string{}
	}
	var out *Filter
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO saved_filter (org_id, owner_id, name, query, shared, columns)
			VALUES (current_org_id(), $1, $2, $3, $4, $5) RETURNING id`, owner, name, query, shared, columns).Scan(&id)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if isCheck(err) {
			return errors.New("a customer cannot share a saved filter")
		}
		if err != nil {
			return fmt.Errorf("save filter: %w", err)
		}
		out, err = scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2`, owner, id))
		return err
	})
	return out, lsn, err
}

// Update changes a filter; only its owner may.
func (s *Service) Update(ctx context.Context, id, actor uuid.UUID, in Input) (*Filter, db.LSN, error) {
	var out *Filter
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2 FOR UPDATE OF f`, actor, id))
		if err != nil {
			return err
		}
		if current.OwnerID != actor {
			return ErrNotYours
		}
		name, query, shared, columns := current.Name, current.Query, current.Shared, current.Columns
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
			if name == "" {
				return errors.New("a saved filter needs a name")
			}
		}
		if in.Query != nil {
			if query, err = parseQuery(*in.Query); err != nil {
				return err
			}
		}
		if in.Shared != nil {
			shared = *in.Shared
		}
		if in.Columns != nil {
			columns = in.Columns
		}
		_, err = tx.Exec(ctx, `UPDATE saved_filter SET name = $2, query = $3, shared = $4, columns = $5 WHERE id = $1`, id, name, query, shared, columns)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if isCheck(err) {
			return errors.New("a customer cannot share a saved filter")
		}
		if err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2`, actor, id))
		return err
	})
	return out, lsn, err
}

// Delete removes a filter; only its owner may.
func (s *Service) Delete(ctx context.Context, id, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var owner uuid.UUID
		err := tx.QueryRow(ctx, `SELECT owner_id FROM saved_filter WHERE id = $1`, id).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if owner != actor {
			return ErrNotYours
		}
		_, err = tx.Exec(ctx, `DELETE FROM saved_filter WHERE id = $1`, id)
		return err
	})
}

// Star marks a filter the reader may see as one of theirs to keep close.
func (s *Service) Star(ctx context.Context, id, reader uuid.UUID, on bool) (*Filter, db.LSN, error) {
	var out *Filter
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2 AND`+visible, reader, id)); err != nil {
			return err
		}
		var err error
		if on {
			_, err = tx.Exec(ctx, `INSERT INTO saved_filter_star (org_id, filter_id, user_id) VALUES (current_org_id(), $1, $2) ON CONFLICT DO NOTHING`, id, reader)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM saved_filter_star WHERE filter_id = $1 AND user_id = $2`, id, reader)
		}
		if err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2`, reader, id))
		return err
	})
	return out, lsn, err
}

// Subscribe has the filter's result mailed to the reader on a schedule.
func (s *Service) Subscribe(ctx context.Context, id, reader uuid.UUID, sub Subscription) (*Filter, db.LSN, error) {
	if err := sub.Validate(); err != nil {
		return nil, 0, err
	}
	var out *Filter
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2 AND`+visible, reader, id)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO saved_filter_subscription (org_id, filter_id, user_id, schedule, hour, weekday)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (filter_id, user_id) DO UPDATE SET schedule = EXCLUDED.schedule, hour = EXCLUDED.hour, weekday = EXCLUDED.weekday`,
			id, reader, sub.Schedule, sub.Hour, sub.Weekday)
		if err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2`, reader, id))
		return err
	})
	return out, lsn, err
}

// Unsubscribe stops the mail.
func (s *Service) Unsubscribe(ctx context.Context, id, reader uuid.UUID) (*Filter, db.LSN, error) {
	var out *Filter
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM saved_filter_subscription WHERE filter_id = $1 AND user_id = $2`, id, reader); err != nil {
			return err
		}
		var err error
		out, err = scan(tx.QueryRow(ctx, selectFilters+` WHERE f.id = $2 AND`+visible, reader, id))
		return err
	})
	return out, lsn, err
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheck(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
