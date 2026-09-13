package desk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/tenant"
)

// The desk's extras: what a customer reads before asking, what an agent keeps
// ready to say, what a customer says once it is over, and when the desk is open.

var (
	ErrArticleNotFound  = errors.New("article not found")
	ErrResponseNotFound = errors.New("canned response not found")
	ErrRatingNotFound   = errors.New("that rating link is not one we sent")
	ErrAlreadyRated     = errors.New("this request was already rated")
	ErrNotAService      = errors.New("this is not a service desk project")
	// TopicRated is emitted when a customer rates a resolved request.
	TopicRated = "csat.rated"
)

// MaxArticleSearch bounds what a portal search lists.
const MaxArticleSearch = 8

// Article is one page of the desk's knowledge base.
type Article struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Published  bool      `json:"published"`
	AuthorName string    `json:"authorName,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ArticleInput is an article as the editor sends it; nil leaves a field alone.
type ArticleInput struct {
	Title     *string `json:"title,omitempty"`
	Body      *string `json:"body,omitempty"`
	Published *bool   `json:"published,omitempty"`
}

// CannedResponse is a reply an agent keeps ready, with placeholders.
type CannedResponse struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type CannedInput struct {
	Name *string `json:"name,omitempty"`
	Body *string `json:"body,omitempty"`
}

// Rating is what a customer said of a resolved request.
type Rating struct {
	IssueKey string     `json:"issueKey"`
	Score    *int       `json:"score,omitempty"`
	Comment  string     `json:"comment,omitempty"`
	SentAt   time.Time  `json:"sentAt"`
	RatedAt  *time.Time `json:"ratedAt,omitempty"`
}

// RatingPage is what the rating link shows before the customer answers.
type RatingPage struct {
	IssueKey string `json:"issueKey"`
	Summary  string `json:"summary"`
	OrgName  string `json:"orgName"`
	Rated    bool   `json:"rated"`
}

const selectArticle = `
SELECT a.id, a.project_id, p.key, a.title, a.body, a.published, COALESCE(u.name, ''), a.created_at, a.updated_at
FROM kb_article a JOIN project p ON p.id = a.project_id LEFT JOIN app_user u ON u.id = a.author_id`

func scanArticle(row pgx.Row) (*Article, error) {
	var a Article
	err := row.Scan(&a.ID, &a.ProjectID, &a.ProjectKey, &a.Title, &a.Body, &a.Published, &a.AuthorName, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrArticleNotFound
	}
	return &a, err
}

func (s *Service) readArticles(ctx context.Context, tx db.DBTX, where string, args ...any) ([]Article, error) {
	rows, err := tx.Query(ctx, selectArticle+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Article{}
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// Articles is a desk's knowledge base as agents see it, drafts included.
func (s *Service) Articles(ctx context.Context, projectKey string) ([]Article, error) {
	var out []Article
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.readArticles(ctx, tx, `WHERE p.key = $1 ORDER BY a.published DESC, lower(a.title)`, project.NormalizeKey(projectKey))
		return err
	})
	return out, err
}

// SearchArticles is what a customer finds before raising a request: the
// published articles of a desk whose words match, best first.
func (s *Service) SearchArticles(ctx context.Context, projectKey, query string) ([]Article, error) {
	query = strings.TrimSpace(query)
	if !allowsKey(ctx, project.NormalizeKey(projectKey)) {
		return []Article{}, nil
	}
	var out []Article
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if query == "" {
			out, err = s.readArticles(ctx, tx, `WHERE p.key = $1 AND a.published ORDER BY lower(a.title) LIMIT $2`, project.NormalizeKey(projectKey), MaxArticleSearch)
			return err
		}
		out, err = s.readArticles(ctx, tx, `
			WHERE p.key = $1 AND a.published
			  AND (to_tsvector('simple', a.title || ' ' || a.body) @@ plainto_tsquery('simple', $2) OR a.title ILIKE '%' || $2 || '%')
			ORDER BY ts_rank(to_tsvector('simple', a.title || ' ' || a.body), plainto_tsquery('simple', $2)) DESC, lower(a.title)
			LIMIT $3`, project.NormalizeKey(projectKey), query, MaxArticleSearch)
		return err
	})
	return out, err
}

// PublishedArticle is one article as a customer reads it.
func (s *Service) PublishedArticle(ctx context.Context, id uuid.UUID) (*Article, error) {
	var out *Article
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanArticle(tx.QueryRow(ctx, selectArticle+` WHERE a.id = $1 AND a.published`, id))
		return err
	})
	if err == nil && !allows(ctx, out.ProjectID) {
		return nil, ErrNotFound
	}
	return out, err
}

// CreateArticle writes a draft, or a published page when it says so.
func (s *Service) CreateArticle(ctx context.Context, projectKey string, in ArticleInput, author uuid.UUID) (*Article, db.LSN, error) {
	title := ""
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
	}
	if title == "" {
		return nil, 0, errors.New("an article needs a title")
	}
	body := ""
	if in.Body != nil {
		body = strings.TrimSpace(*in.Body)
	}
	published := in.Published != nil && *in.Published
	var out *Article
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO kb_article (org_id, project_id, title, body, published, author_id)
			SELECT current_org_id(), id, $2, $3, $4, $5 FROM project WHERE key = $1 RETURNING id`,
			project.NormalizeKey(projectKey), title, body, published, author).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if isUniqueViolation(err) {
			return errors.New("an article with that title is already here")
		}
		if isCheck(err) {
			return ErrNotAService
		}
		if err != nil {
			return err
		}
		out, err = scanArticle(tx.QueryRow(ctx, selectArticle+` WHERE a.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// UpdateArticle changes a page, or publishes and unpublishes it.
func (s *Service) UpdateArticle(ctx context.Context, id uuid.UUID, in ArticleInput) (*Article, db.LSN, error) {
	var out *Article
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanArticle(tx.QueryRow(ctx, selectArticle+` WHERE a.id = $1 FOR UPDATE OF a`, id))
		if err != nil {
			return err
		}
		title, body, published := current.Title, current.Body, current.Published
		if in.Title != nil {
			title = strings.TrimSpace(*in.Title)
			if title == "" {
				return errors.New("an article needs a title")
			}
		}
		if in.Body != nil {
			body = strings.TrimSpace(*in.Body)
		}
		if in.Published != nil {
			published = *in.Published
		}
		_, err = tx.Exec(ctx, `UPDATE kb_article SET title = $2, body = $3, published = $4 WHERE id = $1`, id, title, body, published)
		if isUniqueViolation(err) {
			return errors.New("an article with that title is already here")
		}
		if err != nil {
			return err
		}
		out, err = scanArticle(tx.QueryRow(ctx, selectArticle+` WHERE a.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// DeleteArticle removes a page.
func (s *Service) DeleteArticle(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM kb_article WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrArticleNotFound
		}
		return nil
	})
}

const selectCanned = `
SELECT c.id, c.project_id, p.key, c.name, c.body, c.created_at, c.updated_at
FROM canned_response c JOIN project p ON p.id = c.project_id`

func scanCanned(row pgx.Row) (*CannedResponse, error) {
	var c CannedResponse
	err := row.Scan(&c.ID, &c.ProjectID, &c.ProjectKey, &c.Name, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrResponseNotFound
	}
	return &c, err
}

// CannedResponses is what a desk's agents keep ready, by name.
func (s *Service) CannedResponses(ctx context.Context, projectKey string) ([]CannedResponse, error) {
	out := []CannedResponse{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectCanned+` WHERE p.key = $1 ORDER BY lower(c.name)`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCanned(rows)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return rows.Err()
	})
	return out, err
}

// CreateCanned keeps a reply ready.
func (s *Service) CreateCanned(ctx context.Context, projectKey string, in CannedInput) (*CannedResponse, db.LSN, error) {
	name, body := "", ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Body != nil {
		body = strings.TrimSpace(*in.Body)
	}
	if name == "" || body == "" {
		return nil, 0, errors.New("a canned response needs a name and its words")
	}
	var out *CannedResponse
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO canned_response (org_id, project_id, name, body)
			SELECT current_org_id(), id, $2, $3 FROM project WHERE key = $1 RETURNING id`, project.NormalizeKey(projectKey), name, body).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if isUniqueViolation(err) {
			return errors.New("a canned response with that name is already here")
		}
		if err != nil {
			return err
		}
		out, err = scanCanned(tx.QueryRow(ctx, selectCanned+` WHERE c.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// UpdateCanned changes a reply's name or words.
func (s *Service) UpdateCanned(ctx context.Context, id uuid.UUID, in CannedInput) (*CannedResponse, db.LSN, error) {
	var out *CannedResponse
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanCanned(tx.QueryRow(ctx, selectCanned+` WHERE c.id = $1 FOR UPDATE OF c`, id))
		if err != nil {
			return err
		}
		name, body := current.Name, current.Body
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.Body != nil {
			body = strings.TrimSpace(*in.Body)
		}
		if name == "" || body == "" {
			return errors.New("a canned response needs a name and its words")
		}
		_, err = tx.Exec(ctx, `UPDATE canned_response SET name = $2, body = $3 WHERE id = $1`, id, name, body)
		if isUniqueViolation(err) {
			return errors.New("a canned response with that name is already here")
		}
		if err != nil {
			return err
		}
		out, err = scanCanned(tx.QueryRow(ctx, selectCanned+` WHERE c.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// DeleteCanned removes a reply.
func (s *Service) DeleteCanned(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM canned_response WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrResponseNotFound
		}
		return nil
	})
}

// Vars are what a canned response may name.
type Vars struct {
	CustomerName string
	IssueKey     string
	Summary      string
	AgentName    string
}

// Render writes the words in. An unknown placeholder stays as it is, so the
// agent sees what was not filled rather than a hole.
func Render(body string, v Vars) string {
	pairs := []string{
		"{{customer.name}}", v.CustomerName,
		"{{issue.key}}", v.IssueKey,
		"{{issue.summary}}", v.Summary,
		"{{agent.name}}", v.AgentName,
	}
	return strings.NewReplacer(pairs...).Replace(body)
}

// RenderCanned fills a reply for one request as the agent asking.
func (s *Service) RenderCanned(ctx context.Context, id uuid.UUID, issueKey string, agent uuid.UUID) (string, error) {
	var out string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, err := scanCanned(tx.QueryRow(ctx, selectCanned+` WHERE c.id = $1`, id))
		if err != nil {
			return err
		}
		var v Vars
		v.IssueKey = issueKey
		if err := tx.QueryRow(ctx, `
			SELECT i.summary, COALESCE(r.name, ''), COALESCE((SELECT name FROM app_user WHERE id = $2), '')
			FROM issue i JOIN project p ON p.id = i.project_id
			LEFT JOIN app_user r ON r.id = i.reporter_id
			WHERE p.key || '-' || i.key_num = $1`, strings.ToUpper(strings.TrimSpace(issueKey)), agent).Scan(&v.Summary, &v.CustomerName, &v.AgentName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out = Render(c.Body, v)
		return nil
	})
	return out, err
}

// invite mints the rating token for a resolved request and returns the plain
// token for the mail; a request already invited gets nothing new.
func invite(ctx context.Context, tx db.DBTX, issueID uuid.UUID) (string, error) {
	secret, digest, err := auth.GenerateToken()
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO csat_rating (org_id, issue_id, token_hash) VALUES (current_org_id(), $1, $2)
		ON CONFLICT (issue_id) DO NOTHING`, issueID, digest)
	if err != nil {
		if isCheck(err) {
			return "", nil
		}
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", nil
	}
	return secret, nil
}

// InviteRating mints a request's rating token outside the mail, for a desk
// that wants to send the link another way. Empty means one was already minted.
func (s *Service) InviteRating(ctx context.Context, issueID uuid.UUID) (string, error) {
	var token string
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		token, err = invite(ctx, tx, issueID)
		return err
	})
	return token, err
}

// RatingPage is what the rating link shows. Nobody is signed in, so the token
// names the organization first.
func (s *Service) RatingPage(ctx context.Context, token string) (*RatingPage, error) {
	digest := auth.HashToken(token)
	var orgID uuid.UUID
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT org_id FROM csat_rating WHERE token_hash = $1`, digest).Scan(&orgID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRatingNotFound
	}
	if err != nil {
		return nil, err
	}
	var out RatingPage
	err = s.db.Read(db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: orgID})), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT p.key || '-' || i.key_num, i.summary, o.name, r.rated_at IS NOT NULL
			FROM csat_rating r JOIN issue i ON i.id = r.issue_id JOIN project p ON p.id = i.project_id JOIN org o ON o.id = p.org_id
			WHERE r.token_hash = $1`, digest).Scan(&out.IssueKey, &out.Summary, &out.OrgName, &out.Rated)
	})
	return &out, err
}

// Rate records the customer's score, once.
func (s *Service) Rate(ctx context.Context, token string, score int, comment string) (*Rating, db.LSN, error) {
	if score < 1 || score > 5 {
		return nil, 0, errors.New("a rating is 1 to 5")
	}
	digest := auth.HashToken(token)
	var orgID uuid.UUID
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT org_id FROM csat_rating WHERE token_hash = $1`, digest).Scan(&orgID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, ErrRatingNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	var out *Rating
	lsn, err := s.db.Write(db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: orgID})), func(ctx context.Context, tx db.DBTX) error {
		var already bool
		if err := tx.QueryRow(ctx, `SELECT rated_at IS NOT NULL FROM csat_rating WHERE token_hash = $1 FOR UPDATE`, digest).Scan(&already); err != nil {
			return err
		}
		if already {
			return ErrAlreadyRated
		}
		var issueKey string
		if err := tx.QueryRow(ctx, `
			UPDATE csat_rating r SET score = $2, comment = $3, rated_at = now()
			FROM issue i, project p WHERE r.token_hash = $1 AND i.id = r.issue_id AND p.id = i.project_id
			RETURNING p.key || '-' || i.key_num`, digest, score, strings.TrimSpace(comment)).Scan(&issueKey); err != nil {
			return err
		}
		var err error
		out, err = s.rating(ctx, tx, issueKey)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, TopicRated, map[string]any{"issueKey": issueKey, "score": score})
	})
	return out, lsn, err
}

func (s *Service) rating(ctx context.Context, tx db.DBTX, issueKey string) (*Rating, error) {
	var r Rating
	err := tx.QueryRow(ctx, `
		SELECT p.key || '-' || i.key_num, r.score, r.comment, r.sent_at, r.rated_at
		FROM csat_rating r JOIN issue i ON i.id = r.issue_id JOIN project p ON p.id = i.project_id
		WHERE p.key || '-' || i.key_num = $1`, strings.ToUpper(strings.TrimSpace(issueKey))).Scan(&r.IssueKey, &r.Score, &r.Comment, &r.SentAt, &r.RatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRatingNotFound
	}
	return &r, err
}

// Rating is what a request's customer said, if anything yet.
func (s *Service) Rating(ctx context.Context, issueKey string) (*Rating, error) {
	var out *Rating
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.rating(ctx, tx, issueKey)
		return err
	})
	return out, err
}

// Calendar is a project's business hours, or nothing yet.
func (s *Service) Calendar(ctx context.Context, projectKey string) (*Calendar, error) {
	var (
		timezone string
		hours    []byte
		holidays []string
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT bc.timezone, bc.hours, bc.holidays::text[] FROM business_calendar bc JOIN project p ON p.id = bc.project_id WHERE p.key = $1`,
			project.NormalizeKey(projectKey)).Scan(&timezone, &hours, &holidays)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoCalendar
	}
	if err != nil {
		return nil, err
	}
	return parseCalendar(timezone, hours, holidays)
}

// SaveCalendar sets a project's hours.
func (s *Service) SaveCalendar(ctx context.Context, projectKey string, c Calendar) (*Calendar, db.LSN, error) {
	if err := c.Validate(); err != nil {
		return nil, 0, err
	}
	hours, _ := json.Marshal(c.Hours)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO business_calendar (org_id, project_id, timezone, hours, holidays)
			SELECT current_org_id(), id, $2, $3, $4::date[] FROM project WHERE key = $1
			ON CONFLICT (project_id) DO UPDATE SET timezone = EXCLUDED.timezone, hours = EXCLUDED.hours, holidays = EXCLUDED.holidays`,
			project.NormalizeKey(projectKey), c.Timezone, hours, c.Holidays)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return project.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &c, lsn, nil
}

func isCheck(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23514"
}
