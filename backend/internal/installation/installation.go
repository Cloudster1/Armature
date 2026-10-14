// Package installation is the one way to the installation's name and settings,
// which live on its one row.
package installation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
)

// Installation is what everybody signed in may read about it.
type Installation struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ErrNameRequired is a rename to nothing.
var ErrNameRequired = errors.New("the installation needs a name")

// ErrNotFound is a database nobody has set up yet.
var ErrNotFound = errors.New("the installation was not found")

// Service serves the installation's own page.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// Get reads the installation.
func (s *Service) Get(ctx context.Context) (*Installation, error) {
	var out Installation
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if out.ID, err = ID(ctx, tx); err != nil {
			return err
		}
		out.Name, err = Name(ctx, tx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Rename gives the installation a new name and records who did.
func (s *Service) Rename(ctx context.Context, actor uuid.UUID, name, ip string) (*Installation, db.LSN, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, 0, ErrNameRequired
	}
	var id uuid.UUID
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if id, err = ID(ctx, tx); err != nil {
			return err
		}
		before, err := Name(ctx, tx)
		if err != nil {
			return err
		}
		if err := set(ctx, tx, "name", name); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: "installation.renamed", TargetType: "installation", TargetID: &id, Actor: actor, IP: ip,
			Data: map[string]any{"from": before, "to": name},
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return &Installation{ID: id, Name: name}, lsn, nil
}

// Name is what the installation is called.
func Name(ctx context.Context, tx db.DBTX) (string, error) {
	var name string
	return name, get(ctx, tx, "name", &name)
}

// SecondFactor is who must sign in with a second factor: off, admins or everyone.
func SecondFactor(ctx context.Context, tx db.DBTX) (string, error) {
	var rule string
	return rule, get(ctx, tx, "second_factor", &rule)
}

// SetSecondFactor changes who must sign in with a second factor.
func SetSecondFactor(ctx context.Context, tx db.DBTX, rule string) error {
	return set(ctx, tx, "second_factor", rule)
}

// DefaultTheme is the shared theme shown to whoever has not chosen one, or nil
// for the built-in one.
func DefaultTheme(ctx context.Context, tx db.DBTX) (*uuid.UUID, error) {
	var id *uuid.UUID
	return id, get(ctx, tx, "default_theme_id", &id)
}

// SetDefaultTheme names the default theme; the database refuses one not shared.
func SetDefaultTheme(ctx context.Context, tx db.DBTX, id *uuid.UUID) error {
	return set(ctx, tx, "default_theme_id", id)
}

// AutomationUser is the account every rule acts as, nil before one is made.
func AutomationUser(ctx context.Context, tx db.DBTX) (*uuid.UUID, error) {
	var id *uuid.UUID
	return id, get(ctx, tx, "automation_user_id", &id)
}

// SetAutomationUser names the account every rule acts as.
func SetAutomationUser(ctx context.Context, tx db.DBTX, id uuid.UUID) error {
	return set(ctx, tx, "automation_user_id", id)
}

// ID is the installation's stable id, which a webhook's envelope still carries
// as orgId for one more release.
func ID(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
	var id uuid.UUID
	return id, get(ctx, tx, "id", &id)
}

// get and set take a column from the fixed list above, never from a caller.
func get(ctx context.Context, tx db.DBTX, column string, into any) error {
	err := tx.QueryRow(ctx, `SELECT `+column+` FROM installation`).Scan(into)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read the installation's %s: %w", column, err)
	}
	return nil
}

func set(ctx context.Context, tx db.DBTX, column string, value any) error {
	tag, err := tx.Exec(ctx, `UPDATE installation SET `+column+` = $1`, value)
	if err != nil {
		return fmt.Errorf("change the installation's %s: %w", column, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
