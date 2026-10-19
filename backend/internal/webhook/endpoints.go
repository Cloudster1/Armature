package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// Input is what makes or changes an endpoint.
type Input struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Topics  []string `json:"topics"`
	Enabled *bool    `json:"enabled,omitempty"`
	// Version is the envelope it is posted. Left out, a new endpoint gets the
	// current one and an edit keeps the one it has.
	Version *int `json:"version,omitempty"`
}

// endpointColumns is what scanEndpoint reads, for every select and returning;
// the client's name is read beside its id, so a rename shows at once.
const endpointColumns = `id, name, url, topics, enabled, version, created_at, updated_at,
	client_id, (SELECT c.name FROM oauth_client c WHERE c.id = client_id)`

const selectEndpoints = `SELECT ` + endpointColumns + ` FROM webhook_endpoint`

func scanEndpoint(row pgx.Row) (*Endpoint, error) {
	var (
		e          Endpoint
		clientID   *uuid.UUID
		clientName *string
	)
	if err := row.Scan(&e.ID, &e.Name, &e.URL, &e.Topics, &e.Enabled, &e.Version, &e.CreatedAt, &e.UpdatedAt, &clientID, &clientName); err != nil {
		return nil, err
	}
	if e.Topics == nil {
		e.Topics = []string{}
	}
	if clientID != nil && clientName != nil {
		e.Partner = &Partner{ClientID: *clientID, Name: *clientName}
	}
	return &e, nil
}

// List is the organization's endpoints, by name.
func (s *Service) List(ctx context.Context) ([]Endpoint, error) {
	out := []Endpoint{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectEndpoints+` ORDER BY lower(name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEndpoint(rows)
			if err != nil {
				return err
			}
			out = append(out, *e)
		}
		return rows.Err()
	})
	return out, err
}

func validate(in Input) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("a webhook needs a name")
	}
	if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		return errors.New("the address has to start with http:// or https://")
	}
	for _, t := range in.Topics {
		if t != TopicAny && (!events.Known(t) || strings.HasPrefix(t, "automation.")) {
			return fmt.Errorf("%q is not an event a webhook can take", t)
		}
	}
	if in.Version != nil {
		if _, ok := envelopes[*in.Version]; !ok {
			offered := make([]string, 0, len(envelopes))
			for _, v := range Versions() {
				offered = append(offered, strconv.Itoa(v))
			}
			return fmt.Errorf("there is no webhook envelope version %d. Choose %s, or leave the version out for the current one.", *in.Version, strings.Join(offered, " or "))
		}
	}
	return nil
}

// auditedAddress is where an endpoint points, for the audit log: the path,
// query and user part can carry a credential, as a chat service's hook does.
func auditedAddress(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// record writes what an administrator did to an endpoint, never its secret,
// and which connected app owns it.
func record(ctx context.Context, tx db.DBTX, action string, e *Endpoint, actor uuid.UUID) error {
	data := map[string]any{"name": e.Name, "address": auditedAddress(e.URL), "topics": e.Topics, "enabled": e.Enabled}
	if e.Partner != nil {
		data["clientId"] = e.Partner.ClientID
	}
	return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: "webhook", TargetID: &e.ID, Actor: actor, Data: data})
}

// Create makes an endpoint and returns it with the secret, this once.
func (s *Service) Create(ctx context.Context, in Input, actor uuid.UUID) (*Endpoint, db.LSN, error) {
	if err := validate(in); err != nil {
		return nil, 0, err
	}
	if err := s.reachable(ctx, in.URL); err != nil {
		return nil, 0, err
	}
	secret, _, err := auth.GenerateToken()
	if err != nil {
		return nil, 0, err
	}
	secret = secretPrefix + secret
	if in.Topics == nil {
		in.Topics = []string{}
	}
	version := CurrentVersion
	if in.Version != nil {
		version = *in.Version
	}
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			INSERT INTO webhook_endpoint (name, url, secret, topics, enabled, version)
			VALUES ($1, $2, $3, $4, true, $5)
			RETURNING `+endpointColumns, strings.TrimSpace(in.Name), in.URL, secret, in.Topics, version))
		if isUnique(err) {
			return errors.New("a webhook with that name is already here")
		}
		if err != nil {
			return err
		}
		out = e
		return record(ctx, tx, "webhook.created", e, actor)
	})
	if err != nil {
		return nil, 0, err
	}
	out.Secret = secret
	return out, lsn, nil
}

// Update changes name, address, topics or whether it is on. A partner's own
// endpoint may only be turned off and on here; the partner changes the rest.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input, actor uuid.UUID) (*Endpoint, db.LSN, error) {
	if err := validate(in); err != nil {
		return nil, 0, err
	}
	if in.Topics == nil {
		in.Topics = []string{}
	}
	current, err := s.endpoint(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if current.Partner != nil {
		if in.URL != current.URL || !sameTopics(in.Topics, current.Topics) || (in.Version != nil && *in.Version != current.Version) {
			return nil, 0, &PartnerOwnedError{Name: current.Partner.Name}
		}
		in.Name = current.Name
	}
	if in.URL != current.URL {
		if err := s.reachable(ctx, in.URL); err != nil {
			return nil, 0, err
		}
	}
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			UPDATE webhook_endpoint SET name = $2, url = $3, topics = $4, enabled = COALESCE($5, enabled), version = COALESCE($6, version)
			WHERE id = $1
			RETURNING `+endpointColumns, id, strings.TrimSpace(in.Name), in.URL, in.Topics, in.Enabled, in.Version))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if isUnique(err) {
			return errors.New("a webhook with that name is already here")
		}
		if err != nil {
			return err
		}
		out = e
		return record(ctx, tx, "webhook.updated", e, actor)
	})
	return out, lsn, err
}

// endpoint reads one endpoint from the primary, which a change is about to
// be judged against.
func (s *Service) endpoint(ctx context.Context, id uuid.UUID) (*Endpoint, error) {
	var out *Endpoint
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, selectEndpoints+` WHERE id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		out = e
		return err
	})
	return out, err
}

func sameTopics(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

// RotateSecret issues a new secret; the old one stops working at once. The log
// records that it rotated and nothing of either secret.
func (s *Service) RotateSecret(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Endpoint, db.LSN, error) {
	current, err := s.endpoint(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if current.Partner != nil {
		return nil, 0, &PartnerOwnedError{Name: current.Partner.Name}
	}
	return s.rotate(ctx, id, actor)
}

// rotate gives an endpoint a new secret, whoever owns it.
func (s *Service) rotate(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Endpoint, db.LSN, error) {
	secret, _, err := auth.GenerateToken()
	if err != nil {
		return nil, 0, err
	}
	secret = secretPrefix + secret
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			UPDATE webhook_endpoint SET secret = $2 WHERE id = $1
			RETURNING `+endpointColumns, id, secret))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out = e
		return record(ctx, tx, "webhook.secret_rotated", e, actor)
	})
	if err != nil {
		return nil, 0, err
	}
	out.Secret = secret
	return out, lsn, nil
}

// Delete removes the endpoint and its log.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			DELETE FROM webhook_endpoint WHERE id = $1
			RETURNING `+endpointColumns, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return record(ctx, tx, "webhook.deleted", e, actor)
	})
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "webhook_endpoint_name_idx")
}
