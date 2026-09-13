package issue

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/nql"
)

// SuggestInput is what the search bar has so far: the text, where the caret
// is in it, and the project the page is about, if any.
type SuggestInput struct {
	Query      string
	At         int
	ProjectKey string
	// Viewer is who is asking, for currentUser() in a query.
	Viewer Actor
	// Limit caps the issues offered; the words are capped the same way.
	Limit int
	// Within is the projects the asker may read; Scoped says the list applies,
	// so an empty one offers nothing rather than everything.
	Within []string
	Scoped bool
}

// Suggestions are what the bar offers for the text so far: words the query
// language accepts at the caret, and issues the words so far find.
type Suggestions struct {
	Completion nql.Completion `json:"completion"`
	Words      []Word         `json:"words"`
	Issues     []Issue        `json:"issues"`
}

// Word is one completion: what to put in, and a hint at what it is.
type Word struct {
	Text string `json:"text"`
	// Detail says what kind of word it is: a field, an operator, a status.
	Detail string `json:"detail"`
}

// operatorish tells a query from words: a query has an operator or keyword in it.
var operatorish = regexp.MustCompile(`(?i)[=<>~!]|\b(AND|OR|IN|IS|ORDER)\b`)

// Suggest reads the text up to the caret and offers what could come next.
// The completions come from the same catalog the compiler reads, so a
// suggestion is never something the query cannot say; the names come from
// the tables, scoped to the project when there is one.
func (s *Service) Suggest(ctx context.Context, in SuggestInput) (*Suggestions, error) {
	if in.Limit <= 0 {
		in.Limit = 6
	}
	c := nql.Complete(in.Query, in.At)
	out := &Suggestions{Completion: c, Words: []Word{}, Issues: []Issue{}}

	words := []Word{}
	for _, w := range c.Words {
		words = append(words, Word{Text: w, Detail: detailOf(c.Slot, w)})
	}
	if c.Slot == nql.SlotValue && c.Field != "" && !c.Custom {
		names, detail, err := s.valueNames(ctx, c.Field, in, c.Prefix)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			words = append(words, Word{Text: quoteIfNeeded(name), Detail: detail})
		}
	}
	if len(words) > in.Limit {
		words = words[:in.Limit]
	}
	out.Words = words

	text := strings.TrimSpace(in.Query)
	if text == "" {
		return out, nil
	}
	filter := Filter{ProjectKey: in.ProjectKey, Within: in.Within, Scoped: in.Scoped}
	if projectKey, num, err := ParseKey(text); err == nil {
		filter.Keys = []string{fmt.Sprintf("%s-%d", projectKey, num)}
	} else if operatorish.MatchString(text) {
		// A query that parses finds issues; one that does not yet finds none.
		parsed, err := nql.Parse(text)
		if err != nil {
			return out, nil
		}
		compiled, err := parsed.Compile(nql.Env{UserID: in.Viewer.UserID, Now: time.Now()})
		if err != nil {
			return out, nil
		}
		filter.Query = compiled
	} else {
		filter.Text = text
	}
	result, err := s.List(ctx, filter, Page{Limit: in.Limit, OrderBy: "updated", Desc: true})
	if err != nil {
		return nil, err
	}
	if result.Issues != nil {
		out.Issues = result.Issues
	}
	return out, nil
}

func detailOf(slot nql.Slot, word string) string {
	switch slot {
	case nql.SlotField:
		return "field"
	case nql.SlotOperator:
		return "operator"
	case nql.SlotKeyword:
		return "keyword"
	case nql.SlotValue:
		if strings.HasSuffix(word, "()") {
			return "function"
		}
		return "value"
	}
	return ""
}

// quoteIfNeeded wraps a name the lexer would split.
func quoteIfNeeded(name string) string {
	for _, r := range name {
		if r == ' ' || r == '"' || r == '\'' || r == '(' || r == ')' || r == ',' {
			return `"` + strings.ReplaceAll(name, `"`, `\"`) + `"`
		}
	}
	return name
}

// valueNames lists the names a field's values may take, from the table the
// compiler will look them up in.
func (s *Service) valueNames(ctx context.Context, field string, in SuggestInput, prefix string) ([]string, string, error) {
	var query, detail string
	args := []any{strings.ToLower(prefix) + "%"}
	// A name is offered only from the project the page is about, and only from
	// the projects the asker may read.
	byProject := ""
	if in.ProjectKey != "" {
		args = append(args, strings.ToUpper(in.ProjectKey))
		byProject += fmt.Sprintf(` AND project_id = (SELECT id FROM project WHERE key = $%d)`, len(args))
	}
	byKey := ""
	if in.Scoped {
		args = append(args, append([]string{}, in.Within...))
		byProject += fmt.Sprintf(` AND project_id IN (SELECT id FROM project WHERE key = ANY($%d))`, len(args))
		byKey = fmt.Sprintf(` AND key = ANY($%d)`, len(args))
	}
	switch strings.ToLower(field) {
	case "status":
		query, detail = `SELECT name FROM issue_status WHERE lower(name) LIKE $1 ORDER BY position, name`, "status"
	case "type":
		query, detail = `SELECT name FROM issue_type WHERE lower(name) LIKE $1 ORDER BY name`, "type"
	case "assignee", "reporter":
		query, detail = `SELECT u.name FROM app_user u JOIN org_member m ON m.user_id = u.id WHERE m.org_id = current_org_id() AND u.is_active AND lower(u.name) LIKE $1 ORDER BY u.name`, "person"
	case "project":
		query, detail = `SELECT key FROM project WHERE archived_at IS NULL AND lower(key) LIKE $1`+byKey+` ORDER BY key`, "project"
	case "sprint":
		query, detail = `SELECT name FROM sprint WHERE lower(name) LIKE $1`+byProject+` ORDER BY name`, "sprint"
	case "team":
		query, detail = `SELECT name FROM team WHERE lower(name) LIKE $1`+byProject+` ORDER BY name`, "team"
	case "milestone":
		query, detail = `SELECT name FROM milestone WHERE lower(name) LIKE $1`+byProject+` ORDER BY name`, "milestone"
	case "labels":
		query, detail = `SELECT name FROM label WHERE lower(name) LIKE $1 ORDER BY name`, "label"
	case "fixversion", "affectsversion":
		query, detail = `SELECT name FROM version WHERE lower(name) LIKE $1`+byProject+` ORDER BY name`, "version"
	case "component":
		query, detail = `SELECT name FROM component WHERE lower(name) LIKE $1`+byProject+` ORDER BY name`, "component"
	default:
		return nil, "", nil
	}
	// A table with no project of its own takes only the prefix.
	if !strings.Contains(query, "$2") {
		args = args[:1]
	} else if !strings.Contains(query, "$3") && len(args) > 2 {
		args = args[:2]
	}
	var names []string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("suggest %s: %w", field, err)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			names = append(names, name)
		}
		return rows.Err()
	})
	return names, detail, err
}
