package theme

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/db"
)

var (
	ErrNotFound      = errors.New("theme not found")
	ErrDuplicateName = errors.New("you already have a theme with that name")
	ErrNotYours      = errors.New("only the owner or an administrator can change a theme")
	ErrCustomerShare = errors.New("a customer cannot share a theme")
	ErrAssetNotFound = errors.New("that file is not in the theme")
)

// Theme is one saved theme as the page and the loader see it.
type Theme struct {
	ID        uuid.UUID `json:"id"`
	OwnerID   uuid.UUID `json:"ownerId"`
	OwnerName string    `json:"ownerName"`
	Name      string    `json:"name"`
	Shared    bool      `json:"shared"`
	Spec      Spec      `json:"spec"`
	Assets    []Asset   `json:"assets"`
	// InUse counts the people who chose it; Active says the reader is one.
	InUse     int       `json:"inUse"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Asset is one uploaded file of a theme.
type Asset struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Input is a theme as the page sends it; nil leaves a field alone on an edit.
type Input struct {
	Name   *string `json:"name,omitempty"`
	Shared *bool   `json:"shared,omitempty"`
	Spec   *Spec   `json:"spec,omitempty"`
}

// Service keeps themes and their files.
type Service struct {
	db    *db.Cluster
	store attachment.Store
}

func NewService(cluster *db.Cluster, store attachment.Store) *Service {
	return &Service{db: cluster, store: store}
}

// selectThemes reads themes with the reader's marks; $1 is the reader.
const selectThemes = `
SELECT t.id, t.owner_id, COALESCE(u.name, ''), t.name, t.shared, t.spec, t.created_at, t.updated_at,
       (SELECT count(*) FROM user_theme ut WHERE ut.theme_id = t.id),
       EXISTS (SELECT 1 FROM user_theme ut WHERE ut.theme_id = t.id AND ut.user_id = $1)
FROM theme t
LEFT JOIN app_user u ON u.id = t.owner_id`

// visible is what a reader may see: their own themes and the shared ones.
const visible = ` (t.owner_id = $1 OR t.shared)`

func scan(row pgx.Row) (*Theme, error) {
	var (
		t   Theme
		raw []byte
	)
	err := row.Scan(&t.ID, &t.OwnerID, &t.OwnerName, &t.Name, &t.Shared, &raw, &t.CreatedAt, &t.UpdatedAt, &t.InUse, &t.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &t.Spec); err != nil {
		return nil, fmt.Errorf("read theme %s: %w", t.ID, err)
	}
	t.Spec.normalise()
	t.Assets = []Asset{}
	return &t, nil
}

// withAssets fills in the files of every theme listed.
func withAssets(ctx context.Context, tx db.DBTX, themes []*Theme) error {
	if len(themes) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(themes))
	byID := map[uuid.UUID]*Theme{}
	for _, t := range themes {
		ids = append(ids, t.ID)
		byID[t.ID] = t
	}
	rows, err := tx.Query(ctx, `
		SELECT theme_id, id, name, content_type, size, created_at FROM theme_asset
		WHERE theme_id = ANY($1) ORDER BY created_at`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			themeID uuid.UUID
			a       Asset
		)
		if err := rows.Scan(&themeID, &a.ID, &a.Name, &a.ContentType, &a.Size, &a.CreatedAt); err != nil {
			return err
		}
		byID[themeID].Assets = append(byID[themeID].Assets, a)
	}
	return rows.Err()
}

func assetSet(t *Theme) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, a := range t.Assets {
		out[a.ID] = true
	}
	return out
}

// List is every theme the reader may see: theirs first, then the shared ones.
func (s *Service) List(ctx context.Context, reader uuid.UUID) ([]Theme, error) {
	out := []Theme{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectThemes+` WHERE`+visible+` ORDER BY (t.owner_id <> $1), lower(t.name)`, reader)
		if err != nil {
			return err
		}
		defer rows.Close()
		var themes []*Theme
		for rows.Next() {
			t, err := scan(rows)
			if err != nil {
				return err
			}
			themes = append(themes, t)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := withAssets(ctx, tx, themes); err != nil {
			return err
		}
		for _, t := range themes {
			out = append(out, *t)
		}
		return nil
	})
	return out, err
}

func readOne(ctx context.Context, tx db.DBTX, reader, id uuid.UUID, where string) (*Theme, error) {
	t, err := scan(tx.QueryRow(ctx, selectThemes+where, reader, id))
	if err != nil {
		return nil, err
	}
	return t, withAssets(ctx, tx, []*Theme{t})
}

// Get is one theme the reader may see.
func (s *Service) Get(ctx context.Context, id, reader uuid.UUID) (*Theme, error) {
	var out *Theme
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = readOne(ctx, tx, reader, id, ` WHERE t.id = $2 AND`+visible)
		return err
	})
	return out, err
}

// Active is the theme the reader chose, or nil for the built-in one.
func (s *Service) Active(ctx context.Context, reader uuid.UUID) (*Theme, error) {
	var out *Theme
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT theme_id FROM user_theme WHERE user_id = $1`, reader).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out, err = readOne(ctx, tx, reader, id, ` WHERE t.id = $2 AND`+visible)
		if errors.Is(err, ErrNotFound) {
			// Unshared since it was chosen: the choice lapses quietly.
			out = nil
			return nil
		}
		return err
	})
	return out, err
}

// Choose makes a theme the reader's own, or nil returns them to the built-in one.
func (s *Service) Choose(ctx context.Context, reader uuid.UUID, id *uuid.UUID) (*Theme, db.LSN, error) {
	var out *Theme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if id == nil {
			_, err := tx.Exec(ctx, `DELETE FROM user_theme WHERE user_id = $1`, reader)
			return err
		}
		t, err := readOne(ctx, tx, reader, *id, ` WHERE t.id = $2 AND`+visible)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_theme (org_id, user_id, theme_id) VALUES (current_org_id(), $1, $2)
			ON CONFLICT (org_id, user_id) DO UPDATE SET theme_id = EXCLUDED.theme_id`, reader, *id); err != nil {
			return err
		}
		t.Active = true
		out = t
		return nil
	})
	return out, lsn, err
}

func cleanName(name *string) (string, error) {
	if name == nil {
		return "", errors.New("a theme needs a name")
	}
	trimmed := strings.TrimSpace(*name)
	if trimmed == "" {
		return "", errors.New("a theme needs a name")
	}
	return trimmed, nil
}

// Create saves a theme for the owner. Files come later, so the spec cannot
// name any yet.
func (s *Service) Create(ctx context.Context, owner uuid.UUID, in Input) (*Theme, db.LSN, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	spec := Spec{}
	if in.Spec != nil {
		spec = *in.Spec
	}
	if err := Validate(&spec, uuid.Nil, nil); err != nil {
		return nil, 0, err
	}
	shared := in.Shared != nil && *in.Shared
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, 0, err
	}
	var out *Theme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO theme (org_id, owner_id, name, shared, spec)
			VALUES (current_org_id(), $1, $2, $3, $4) RETURNING id`, owner, name, shared, encoded).Scan(&id)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if isCheck(err) {
			return ErrCustomerShare
		}
		if err != nil {
			return fmt.Errorf("save theme: %w", err)
		}
		out, err = readOne(ctx, tx, owner, id, ` WHERE t.id = $2`)
		return err
	})
	return out, lsn, err
}

// lockOwned reads a theme for writing and refuses anybody but its owner, or
// an administrator, who may tidy what anybody shared.
func lockOwned(ctx context.Context, tx db.DBTX, id, actor uuid.UUID, administers bool) (*Theme, error) {
	current, err := readOne(ctx, tx, actor, id, ` WHERE t.id = $2 FOR UPDATE OF t`)
	if err != nil {
		return nil, err
	}
	switch {
	case current.OwnerID == actor:
		return current, nil
	case !current.Shared:
		return nil, ErrNotFound
	case administers:
		return current, nil
	}
	return nil, ErrNotYours
}

// Update changes a theme; the owner's to do, or an administrator's when shared.
func (s *Service) Update(ctx context.Context, id, actor uuid.UUID, administers bool, in Input) (*Theme, db.LSN, error) {
	var out *Theme
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		name, shared, spec := current.Name, current.Shared, current.Spec
		if in.Name != nil {
			if name, err = cleanName(in.Name); err != nil {
				return err
			}
		}
		if in.Shared != nil {
			shared = *in.Shared
		}
		if in.Spec != nil {
			spec = *in.Spec
		}
		if err := Validate(&spec, id, assetSet(current)); err != nil {
			return err
		}
		encoded, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE theme SET name = $2, shared = $3, spec = $4 WHERE id = $1`, id, name, shared, encoded)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if isCheck(err) {
			return ErrCustomerShare
		}
		if err != nil {
			return err
		}
		out, err = readOne(ctx, tx, actor, id, ` WHERE t.id = $2`)
		return err
	})
	return out, lsn, err
}

// Delete removes a theme and its files. Everybody who chose it returns to the
// built-in theme, by the cascade.
func (s *Service) Delete(ctx context.Context, id, actor uuid.UUID, administers bool) (db.LSN, error) {
	var keys []string
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		for _, a := range current.Assets {
			keys = append(keys, objectKey(id, a.ID))
		}
		_, err = tx.Exec(ctx, `DELETE FROM theme WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.dropObjects(ctx, keys)
	return lsn, nil
}

// EraseOwner takes a person's themes out with them, files included.
func (s *Service) EraseOwner(ctx context.Context, userID uuid.UUID) error {
	var keys []string
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT a.theme_id, a.id FROM theme_asset a JOIN theme t ON t.id = a.theme_id WHERE t.owner_id = $1`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var themeID, assetID uuid.UUID
			if err := rows.Scan(&themeID, &assetID); err != nil {
				return err
			}
			keys = append(keys, objectKey(themeID, assetID))
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_theme WHERE user_id = $1`, userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM theme WHERE owner_id = $1`, userID)
		return err
	})
	if err != nil {
		return err
	}
	s.dropObjects(ctx, keys)
	return nil
}

// dropObjects removes files after their rows are gone. An object left behind
// is unreachable and cheap; a row without its object would be a broken link.
func (s *Service) dropObjects(ctx context.Context, keys []string) {
	if s.store == nil {
		return
	}
	for _, key := range keys {
		_ = s.store.Delete(ctx, key)
	}
}

func objectKey(themeID, assetID uuid.UUID) string {
	return "theme/" + themeID.String() + "/" + assetID.String()
}

// UploadAsset puts a file on a theme. The type is what the bytes say.
func (s *Service) UploadAsset(ctx context.Context, id, actor uuid.UUID, administers bool, name string, body io.Reader) (*Asset, db.LSN, error) {
	if s.store == nil {
		return nil, 0, attachment.ErrUnavailable
	}
	if _, off := s.store.(attachment.Unavailable); off {
		return nil, 0, attachment.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxAssetBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read upload: %w", err)
	}
	contentType, err := sniff(name, data)
	if err != nil {
		return nil, 0, err
	}
	name = attachment.CleanName(name)
	var out Asset
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		if len(current.Assets) >= MaxAssetsPerSpec {
			return ErrTooManyAssets
		}
		assetID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		out = Asset{ID: assetID, Name: name, ContentType: contentType, Size: int64(len(data))}
		if err := tx.QueryRow(ctx, `
			INSERT INTO theme_asset (id, org_id, theme_id, name, content_type, size)
			VALUES ($1, current_org_id(), $2, $3, $4, $5) RETURNING created_at`,
			assetID, id, name, contentType, len(data)).Scan(&out.CreatedAt); err != nil {
			return fmt.Errorf("record the file: %w", err)
		}
		return s.store.Put(ctx, objectKey(id, assetID), bytes.NewReader(data), int64(len(data)), contentType)
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}

// OpenAsset reads a file of a theme the reader may see. The caller closes it.
func (s *Service) OpenAsset(ctx context.Context, id, assetID, reader uuid.UUID) (io.ReadCloser, *Asset, error) {
	if s.store == nil {
		return nil, nil, ErrAssetNotFound
	}
	var found *Asset
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		t, err := readOne(ctx, tx, reader, id, ` WHERE t.id = $2 AND`+visible)
		if err != nil {
			return err
		}
		for _, a := range t.Assets {
			if a.ID == assetID {
				found = &a
				return nil
			}
		}
		return ErrAssetNotFound
	})
	if err != nil {
		return nil, nil, err
	}
	body, err := s.store.Get(ctx, objectKey(id, assetID))
	if err != nil {
		return nil, nil, ErrAssetNotFound
	}
	return body, found, nil
}

// DeleteAsset takes a file off a theme, unless the theme still names it.
func (s *Service) DeleteAsset(ctx context.Context, id, assetID, actor uuid.UUID, administers bool) (db.LSN, error) {
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := lockOwned(ctx, tx, id, actor, administers)
		if err != nil {
			return err
		}
		if !assetSet(current)[assetID] {
			return ErrAssetNotFound
		}
		for _, used := range current.Spec.AssetIDs() {
			if used == assetID {
				return ErrAssetInUse
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM theme_asset WHERE id = $1 AND theme_id = $2`, assetID, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.dropObjects(ctx, []string{objectKey(id, assetID)})
	return lsn, nil
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheck(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
