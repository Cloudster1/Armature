// Package label owns labels: the organization's words for tagging issues,
// shared across projects so the same word means the same thing everywhere.
package label

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

// Label is one word, with a colour and how often it is used.
type Label struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Color      string    `json:"color"`
	IssueCount int       `json:"issueCount"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Colors are the palette a label can take; a new label is given one from its
// name, so the same word is the same colour in every organization.
var Colors = []string{"gray", "red", "orange", "amber", "green", "teal", "blue", "purple", "pink"}

// MaxNameLength keeps a label a word.
const MaxNameLength = 40

var (
	// ErrNotFound is returned for a label that is not in this organization.
	ErrNotFound = errors.New("label not found")
	// ErrNameTaken is returned when the organization already has the word.
	ErrNameTaken = errors.New("there is already a label with that name")
	// ErrBadName is returned for a name that is blank, has spaces or is too long.
	ErrBadName = errors.New("a label is one word of up to 40 characters, with no spaces")
	// ErrBadColor is returned for a colour outside the palette.
	ErrBadColor = errors.New("that is not one of the label colours")
)

// Service keeps labels and puts them on issues.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

var namePattern = regexp.MustCompile(`^\S+$`)

// CleanName trims a name and checks it is one word.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !namePattern.MatchString(name) || len([]rune(name)) > MaxNameLength {
		return "", ErrBadName
	}
	return name, nil
}

// ColorFor picks a colour from the palette by the name, case insensitively.
func ColorFor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return Colors[int(h.Sum32()%uint32(len(Colors)))]
}

func validColor(color string) bool {
	for _, c := range Colors {
		if c == color {
			return true
		}
	}
	return false
}

const selectLabel = `
SELECT l.id, l.name, l.color, (SELECT count(*) FROM issue_label il WHERE il.label_id = l.id)::int, l.created_at, l.updated_at
FROM label l`

func scanLabel(row pgx.Row) (*Label, error) {
	var l Label
	err := row.Scan(&l.ID, &l.Name, &l.Color, &l.IssueCount, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// DefaultPage is a page of labels unless asked otherwise; MaxPage is the most
// one read returns, so the words of a large installation never travel at once.
const (
	DefaultPage = 50
	MaxPage     = 200
)

// Filter narrows and pages a listing of labels.
type Filter struct {
	// Text matches the name anywhere in it; names starting with it come first.
	Text   string
	Limit  int
	Offset int
}

// Page is one page of labels plus how many matched in all.
type Page struct {
	Labels []Label `json:"labels"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

// List returns one page of the labels whose name holds the text, case aside:
// names starting with it first, then the rest, each alphabetically.
func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	if f.Limit <= 0 || f.Limit > MaxPage {
		f.Limit = DefaultPage
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	f.Text = strings.TrimSpace(f.Text)
	page := Page{Labels: []Label{}, Limit: f.Limit, Offset: f.Offset}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		const matches = ` WHERE strpos(lower(l.name), lower($1)) > 0`
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM label l`+matches, f.Text).Scan(&page.Total); err != nil {
			return err
		}
		// The page is chosen first, so the counts of use run only for its rows.
		const ordered = ` ORDER BY strpos(lower(l.name), lower($1)) <> 1, lower(l.name), l.id`
		rows, err := tx.Query(ctx, `WITH page AS (SELECT l.id FROM label l`+matches+ordered+` LIMIT $2 OFFSET $3)`+
			selectLabel+` JOIN page ON page.id = l.id`+ordered, f.Text, f.Limit, f.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scanLabel(rows)
			if err != nil {
				return err
			}
			page.Labels = append(page.Labels, *l)
		}
		return rows.Err()
	})
	if err != nil {
		return Page{}, fmt.Errorf("list labels: %w", err)
	}
	return page, nil
}

// Input describes a label being made or changed.
type Input struct {
	Name  string
	Color string
}

// Create makes a label. An empty colour is chosen from the name.
func (s *Service) Create(ctx context.Context, in Input) (*Label, db.LSN, error) {
	name, err := CleanName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	color := in.Color
	if color == "" {
		color = ColorFor(name)
	}
	if !validColor(color) {
		return nil, 0, ErrBadColor
	}
	var created *Label
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO label (name, color) VALUES ($1, $2) RETURNING id`, name, color).Scan(&id)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create label: %w", err)
		}
		created, err = scanLabel(tx.QueryRow(ctx, selectLabel+` WHERE l.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// UpdateInput carries what an edit may change; nil leaves a part alone.
type UpdateInput struct {
	Name  *string
	Color *string
}

// Update renames or recolours a label, everywhere it is used.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Label, db.LSN, error) {
	var updated *Label
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := scanLabel(tx.QueryRow(ctx, selectLabel+` WHERE l.id = $1 FOR UPDATE OF l`, id)); err != nil {
			return err
		}
		if in.Name != nil {
			name, err := CleanName(*in.Name)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE label SET name = $2 WHERE id = $1`, id, name); isUniqueViolation(err) {
				return ErrNameTaken
			} else if err != nil {
				return err
			}
		}
		if in.Color != nil {
			if !validColor(*in.Color) {
				return ErrBadColor
			}
			if _, err := tx.Exec(ctx, `UPDATE label SET color = $2 WHERE id = $1`, id, *in.Color); err != nil {
				return err
			}
		}
		var err error
		updated, err = scanLabel(tx.QueryRow(ctx, selectLabel+` WHERE l.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}

// Delete removes a label from the organization and from every issue.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM label WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// SetIssueLabels makes an issue carry exactly these labels, by name. A name
// the organization lacks becomes a label, so nobody has to define it first.
func (s *Service) SetIssueLabels(ctx context.Context, key string, names []string, actor issue.Actor) ([]issue.LabelRef, db.LSN, error) {
	projectKey, num, err := issue.ParseKey(key)
	if err != nil {
		return nil, 0, err
	}
	var out []issue.LabelRef
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		issueID, labels, changes, err := s.SetIssueLabelsIn(ctx, tx, key, names)
		if err != nil {
			return err
		}
		out = labels
		return issue.AnnounceUpdate(ctx, tx, issueID, issue.FormatKey(projectKey, num), actor, changes)
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// SetIssueLabelsIn is SetIssueLabels inside the caller's transaction, leaving
// the change, the words that came and went, for the caller to announce.
func (s *Service) SetIssueLabelsIn(ctx context.Context, tx db.DBTX, key string, names []string) (uuid.UUID, []issue.LabelRef, []issue.Change, error) {
	return s.labelIssue(ctx, tx, key, names, nil, false)
}

// ChangeIssueLabelsIn puts some labels on an issue and takes others off,
// keeping the rest, read inside the caller's transaction so nothing in between is lost.
func (s *Service) ChangeIssueLabelsIn(ctx context.Context, tx db.DBTX, key string, add, remove []string) (uuid.UUID, []issue.LabelRef, []issue.Change, error) {
	return s.labelIssue(ctx, tx, key, add, remove, true)
}

func (s *Service) labelIssue(ctx context.Context, tx db.DBTX, key string, names, remove []string, keep bool) (uuid.UUID, []issue.LabelRef, []issue.Change, error) {
	projectKey, num, err := issue.ParseKey(key)
	if err != nil {
		return uuid.Nil, nil, nil, err
	}
	wanted := map[string]string{}
	for _, raw := range names {
		name, err := CleanName(raw)
		if err != nil {
			return uuid.Nil, nil, nil, fmt.Errorf("%w: %q", ErrBadName, raw)
		}
		wanted[strings.ToLower(name)] = name
	}

	var issueID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id
		WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num).Scan(&issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, nil, issue.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, nil, nil, err
	}

	before, err := labelsOn(ctx, tx, issueID)
	if err != nil {
		return uuid.Nil, nil, nil, err
	}
	had := map[string]bool{}
	for _, l := range before {
		had[strings.ToLower(l.Name)] = true
		if keep {
			wanted[strings.ToLower(l.Name)] = l.Name
		}
	}
	// Taken off after the merge, so a word being removed is never coined.
	for _, raw := range remove {
		delete(wanted, strings.ToLower(strings.TrimSpace(raw)))
	}

	for lower, name := range wanted {
		if had[lower] {
			continue
		}
		// Coin the word unless it exists, then read it. Somebody coining the
		// same word at once makes this wait for them and use theirs.
		if _, err := tx.Exec(ctx, `INSERT INTO label (name, color) VALUES ($1, $2) ON CONFLICT (lower(name)) DO NOTHING`,
			name, ColorFor(name)); err != nil {
			return uuid.Nil, nil, nil, fmt.Errorf("label %q: %w", name, err)
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM label WHERE lower(name) = $1`, lower).Scan(&id); err != nil {
			return uuid.Nil, nil, nil, fmt.Errorf("label %q: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO issue_label (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, issueID, id); err != nil {
			return uuid.Nil, nil, nil, err
		}
	}
	for _, l := range before {
		if _, keep := wanted[strings.ToLower(l.Name)]; !keep {
			if _, err := tx.Exec(ctx, `DELETE FROM issue_label WHERE issue_id = $1 AND label_id = $2`, issueID, l.ID); err != nil {
				return uuid.Nil, nil, nil, err
			}
		}
	}

	out, err := labelsOn(ctx, tx, issueID)
	if err != nil {
		return uuid.Nil, nil, nil, err
	}
	from, to := words(before), words(out)
	if from == to {
		return issueID, out, nil, nil
	}
	return issueID, out, []issue.Change{{Field: "labels", From: from, To: to}}, nil
}

// labelsOn reads an issue's labels alphabetically.
func labelsOn(ctx context.Context, tx db.DBTX, issueID uuid.UUID) ([]issue.LabelRef, error) {
	rows, err := tx.Query(ctx, `
		SELECT l.id, l.name, l.color FROM issue_label il JOIN label l ON l.id = il.label_id
		WHERE il.issue_id = $1 ORDER BY lower(l.name)`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []issue.LabelRef{}
	for rows.Next() {
		var l issue.LabelRef
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// words joins label names for the changelog.
func words(labels []issue.LabelRef) string {
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
