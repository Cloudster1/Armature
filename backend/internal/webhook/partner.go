package webhook

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// A connected app owns at most one endpoint, which it makes and changes itself
// once an administrator allowed it. The endpoint is an ordinary one: the same
// envelope, signatures, retries and log as every other.

// PartnerInput is what a connected app makes its endpoint with.
type PartnerInput struct {
	URL    string   `json:"url"`
	Topics []string `json:"topics"`
	// Version is the envelope it is posted; left out, the current one.
	Version *int `json:"version,omitempty"`
}

// PartnerChanges is what a connected app changes; what is left out keeps its value.
type PartnerChanges struct {
	URL     *string   `json:"url,omitempty"`
	Topics  *[]string `json:"topics,omitempty"`
	Version *int      `json:"version,omitempty"`
	Enabled *bool     `json:"enabled,omitempty"`
}

// App is the connected app asking, by its client.
type App struct {
	ClientID uuid.UUID
	Name     string
}

// endpointName is what a connected app's endpoint is called in the list.
func (a App) endpointName() string { return a.Name + auth.ConnectedAppSuffix }

var (
	// ErrPartnerExists is a second endpoint asked for by one app.
	ErrPartnerExists = errors.New("this app has a webhook already")
)

// PartnerOwnedError is an administrator changing what a connected app manages.
type PartnerOwnedError struct{ Name string }

func (e *PartnerOwnedError) Error() string {
	return fmt.Sprintf("%s manages this webhook. Change it in %s, or remove it here", e.Name, e.Name)
}

// FieldError is a value refused, with the sentence for its field.
type FieldError struct{ Field, Sentence string }

func (e *FieldError) Error() string { return e.Field + ": " + e.Sentence }

// NameTakenError is an app's endpoint named like one an administrator made.
type NameTakenError struct{ Name string }

func (e *NameTakenError) Error() string {
	return fmt.Sprintf("A webhook called %s is already here. Rename it in Settings, Webhooks, and connect again.", e.Name)
}

// validatePartner holds an app to named topics: never every event, never a
// rule's own.
func validatePartner(app App, url string, topics []string, version *int) error {
	if err := validate(Input{Name: app.endpointName(), URL: url, Version: version}); err != nil {
		return err
	}
	if len(topics) == 0 {
		return &FieldError{Field: "topics", Sentence: fmt.Sprintf("Name the topics %s takes, such as issue.updated.", app.Name)}
	}
	for _, t := range topics {
		if t == TopicAny {
			return &FieldError{Field: "topics", Sentence: fmt.Sprintf("Name the topics %s takes; a connected app cannot take every event.", app.Name)}
		}
		if !events.Known(t) || strings.HasPrefix(t, "automation.") {
			return &FieldError{Field: "topics", Sentence: fmt.Sprintf("%s is not an event a webhook can take. GET /automation/catalog lists them.", t)}
		}
	}
	return nil
}

// sortedTopics keeps topics in one order, so an answer reads the same each time.
func sortedTopics(topics []string) []string {
	out := slices.Clone(topics)
	slices.Sort(out)
	return slices.Compact(out)
}

// PartnerEndpoint is the app's endpoint, or nil when it has none.
func (s *Service) PartnerEndpoint(ctx context.Context, clientID uuid.UUID) (*Endpoint, error) {
	var out *Endpoint
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, selectEndpoints+` WHERE client_id = $1`, clientID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		out = e
		return err
	})
	return out, err
}

// CreatePartner makes the app's endpoint and returns it with its secret, this once.
func (s *Service) CreatePartner(ctx context.Context, app App, in PartnerInput, actor uuid.UUID) (*Endpoint, db.LSN, error) {
	if err := validatePartner(app, in.URL, in.Topics, in.Version); err != nil {
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
	version := CurrentVersion
	if in.Version != nil {
		version = *in.Version
	}
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Asked first: its own endpoint takes the same name, which would
		// otherwise read as somebody else's.
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_endpoint WHERE client_id = $1)`, app.ClientID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrPartnerExists
		}
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			INSERT INTO webhook_endpoint (name, url, secret, topics, enabled, version, client_id)
			VALUES ($1, $2, $3, $4, true, $5, $6)
			RETURNING `+endpointColumns, app.endpointName(), in.URL, secret, sortedTopics(in.Topics), version, app.ClientID))
		switch {
		case err != nil && strings.Contains(err.Error(), "webhook_endpoint_client_id_key"):
			return ErrPartnerExists
		case isUnique(err):
			return &NameTakenError{Name: app.endpointName()}
		case err != nil:
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

// UpdatePartner changes the app's endpoint; ErrNotFound when it has none.
func (s *Service) UpdatePartner(ctx context.Context, app App, in PartnerChanges, actor uuid.UUID) (*Endpoint, db.LSN, error) {
	current, err := s.PartnerEndpoint(ctx, app.ClientID)
	if err != nil {
		return nil, 0, err
	}
	if current == nil {
		return nil, 0, ErrNotFound
	}
	url, topics, version := current.URL, current.Topics, current.Version
	if in.URL != nil {
		url = *in.URL
	}
	if in.Topics != nil {
		topics = *in.Topics
	}
	if in.Version != nil {
		version = *in.Version
	}
	if err := validatePartner(app, url, topics, &version); err != nil {
		return nil, 0, err
	}
	if url != current.URL {
		if err := s.reachable(ctx, url); err != nil {
			return nil, 0, err
		}
	}
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			UPDATE webhook_endpoint SET url = $2, topics = $3, version = $4, enabled = COALESCE($5, enabled)
			WHERE client_id = $1
			RETURNING `+endpointColumns, app.ClientID, url, sortedTopics(topics), version, in.Enabled))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out = e
		return record(ctx, tx, "webhook.updated", e, actor)
	})
	return out, lsn, err
}

// RotatePartnerSecret gives the app's endpoint a new secret; the old one stops at once.
func (s *Service) RotatePartnerSecret(ctx context.Context, app App, actor uuid.UUID) (*Endpoint, db.LSN, error) {
	current, err := s.PartnerEndpoint(ctx, app.ClientID)
	if err != nil {
		return nil, 0, err
	}
	if current == nil {
		return nil, 0, ErrNotFound
	}
	return s.rotate(ctx, current.ID, actor)
}

// TestPartner posts a ping to the app's endpoint now.
func (s *Service) TestPartner(ctx context.Context, app App) (*Delivery, db.LSN, error) {
	current, err := s.PartnerEndpoint(ctx, app.ClientID)
	if err != nil {
		return nil, 0, err
	}
	if current == nil {
		return nil, 0, ErrNotFound
	}
	return s.Test(ctx, current.ID)
}

// DeletePartner removes the app's endpoint and its log; having none is no error.
func (s *Service) DeletePartner(ctx context.Context, app App, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			DELETE FROM webhook_endpoint WHERE client_id = $1
			RETURNING `+endpointColumns, app.ClientID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return record(ctx, tx, "webhook.deleted", e, actor)
	})
}
