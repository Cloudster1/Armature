package csvio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
)

// lookups are the names a row may use, read once per import.
type lookups struct {
	types    map[string]uuid.UUID
	levels   map[string]int
	statuses map[string]uuid.UUID
	people   map[string]uuid.UUID
	emails   map[uuid.UUID]string
	fields   map[string]field.Field
}

func (s *Service) lookups(ctx context.Context, projectKey string) (*lookups, error) {
	l := &lookups{
		types: map[string]uuid.UUID{}, levels: map[string]int{}, statuses: map[string]uuid.UUID{},
		people: map[string]uuid.UUID{}, emails: map[uuid.UUID]string{},
		fields: map[string]field.Field{},
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := l.readTypes(ctx, tx); err != nil {
			return err
		}
		if err := readNames(ctx, tx, `SELECT id, name FROM issue_status`, l.statuses); err != nil {
			return err
		}
		// Inactive members are included: an account made by an earlier import
		// stands for a person who worked here and cannot sign in.
		people, err := tx.Query(ctx, `
			SELECT u.id, u.email, u.name FROM app_user u
			JOIN org_member m ON m.user_id = u.id AND m.org_id = current_org_id()`)
		if err != nil {
			return err
		}
		defer people.Close()
		for people.Next() {
			var (
				id          uuid.UUID
				email, name string
			)
			if err := people.Scan(&id, &email, &name); err != nil {
				return err
			}
			l.emails[id] = email
			for _, key := range []string{personKey(email), personKey(name), personKey(localPart(email))} {
				if _, taken := l.people[key]; !taken && key != "" {
					l.people[key] = id
				}
			}
		}
		return people.Err()
	})
	if err != nil {
		return nil, err
	}
	fields, err := s.fields.Fields(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		l.fields[strings.ToLower(f.Name)] = f
	}
	return l, nil
}

// readTypes keeps the level beside the name, which is what puts the epics of
// a file before the stories under them.
func (l *lookups) readTypes(ctx context.Context, tx db.DBTX) error {
	rows, err := tx.Query(ctx, `SELECT id, name, hierarchy_level FROM issue_type`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    uuid.UUID
			name  string
			level int
		)
		if err := rows.Scan(&id, &name, &level); err != nil {
			return err
		}
		l.types[strings.ToLower(name)] = id
		l.levels[strings.ToLower(name)] = level
	}
	return rows.Err()
}

func readNames(ctx context.Context, tx db.DBTX, query string, into map[string]uuid.UUID) error {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		into[strings.ToLower(name)] = id
	}
	return rows.Err()
}

// personKey is how a person's name is compared, so that Steve.Berthold, steve
// berthold and Steve Berthold are one person.
func personKey(s string) string {
	clean := strings.ToLower(strings.TrimSpace(s))
	clean = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(clean)
	return strings.Join(strings.Fields(clean), " ")
}

func localPart(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return ""
	}
	return email[:at]
}

// means is what a word in the file becomes here, which the person mapping the
// file decides when the name is not already ours.
func (v Values) means(target, word string) string {
	if v == nil {
		return word
	}
	if m, ok := v[target]; ok {
		if to, ok := m[word]; ok {
			return to
		}
		if to, ok := m[strings.ToLower(word)]; ok {
			return to
		}
	}
	return word
}

// rowInput turns one row into the issue it describes, plus the labels and
// custom values that are written after it.
func (s *Service) rowInput(projectKey string, row Row, req ImportRequest, look *lookups,
	people map[string]uuid.UUID) (issue.ImportInput, []string, map[uuid.UUID]json.RawMessage, error) {
	in := issue.ImportInput{
		CreateInput: issue.CreateInput{ProjectKey: projectKey, Summary: row.value(req.Mapping, "summary")},
		Source:      req.Source,
	}
	if in.Summary == "" {
		return in, nil, nil, errors.New("the summary is empty")
	}
	if text := row.value(req.Mapping, "description"); text != "" {
		in.Description = issue.WikiDocument(text)
	}
	// The file names a parent by its own key; whether that is a key here is the
	// importer's question, since it depends on what this run has written.
	in.ParentKey = row.value(req.Mapping, "parent")
	if in.ParentKey == "" {
		in.ParentKey = row.value(req.Mapping, "epic")
	}
	if key := row.value(req.Mapping, "key"); key != "" {
		in.ExternalKey = key
		if num, ok := keyNumber(key); ok {
			in.KeyNum = &num
		}
	}
	if err := s.rowNames(&in, row, req, look, people); err != nil {
		return in, nil, nil, err
	}
	if err := rowTimes(&in, row, req.Mapping); err != nil {
		return in, nil, nil, err
	}
	if text := row.value(req.Mapping, "estimate"); text != "" {
		var points float64
		if _, err := fmt.Sscanf(text, "%g", &points); err != nil {
			return in, nil, nil, fmt.Errorf("estimate %q is not a number", text)
		}
		in.Estimate = &points
	}

	var labels []string
	for _, cell := range row.values(req.Mapping, "labels") {
		for _, name := range splitList(cell) {
			labels = append(labels, strings.Join(strings.Fields(name), "-"))
		}
	}
	customs, err := s.rowFields(row, req, look)
	return in, labels, customs, err
}

// rowNames resolves the words a row uses for a type, a status, a priority and
// the people on it.
func (s *Service) rowNames(in *issue.ImportInput, row Row, req ImportRequest, look *lookups,
	people map[string]uuid.UUID) error {
	if word := row.value(req.Mapping, "type"); word != "" {
		if name := req.Values.means("type", word); name != "" {
			id, ok := look.types[strings.ToLower(name)]
			if !ok {
				return fmt.Errorf("%q is not an issue type here; say what it means", word)
			}
			in.TypeID = id
		}
	}
	if word := row.value(req.Mapping, "status"); word != "" {
		if name := req.Values.means("status", word); name != "" {
			id, ok := look.statuses[strings.ToLower(name)]
			if !ok {
				return fmt.Errorf("%q is not a status here; say what it means", word)
			}
			in.StatusID = id
		}
	}
	if word := row.value(req.Mapping, "priority"); word != "" {
		if name := req.Values.means("priority", word); name != "" {
			in.Priority = issue.Priority(strings.ToLower(name))
			if !in.Priority.Valid() {
				return fmt.Errorf("%q is not a priority; say what it means", word)
			}
		}
	}
	if who := row.value(req.Mapping, "assignee"); who != "" {
		if id, ok := people[personKey(who)]; ok {
			in.AssigneeID = &id
		}
	}
	if who := row.value(req.Mapping, "reporter"); who != "" {
		if id, ok := people[personKey(who)]; ok {
			in.ReporterID = &id
		}
	}
	return nil
}

// rowTimes reads the days and the moments, which every tracker writes its own
// way and Jira writes two ways in one file.
func rowTimes(in *issue.ImportInput, row Row, mapping Mapping) error {
	days := map[string]**time.Time{"due": &in.DueDate, "start": &in.StartDate}
	for target, into := range days {
		text := row.value(mapping, target)
		if text == "" {
			continue
		}
		at, err := parseDay(text)
		if err != nil {
			return fmt.Errorf("%s %q is not a date this can read", target, text)
		}
		*into = &at
	}
	moments := map[string]*time.Time{"created": &in.CreatedAt, "updated": &in.UpdatedAt}
	for target, into := range moments {
		text := row.value(mapping, target)
		if text == "" {
			continue
		}
		at, err := parseMoment(text)
		if err != nil {
			return fmt.Errorf("%s %q is not a date this can read", target, text)
		}
		*into = at
	}
	if text := row.value(mapping, "resolved"); text != "" {
		at, err := parseMoment(text)
		if err != nil {
			return fmt.Errorf("resolved %q is not a date this can read", text)
		}
		in.ResolvedAt = &at
	}
	spans := map[string]**int{"timeEstimate": &in.TimeEstimateMinutes, "timeRemaining": &in.TimeRemainingMinutes}
	for target, into := range spans {
		text := row.value(mapping, target)
		if text == "" {
			continue
		}
		minutes, err := parseMinutes(text)
		if err != nil {
			return fmt.Errorf("%s: %v", target, err)
		}
		*into = &minutes
	}
	return nil
}

// rowFields reads the cells that fill this project's own fields.
func (s *Service) rowFields(row Row, req ImportRequest, look *lookups) (map[uuid.UUID]json.RawMessage, error) {
	customs := map[uuid.UUID]json.RawMessage{}
	for target := range req.Mapping {
		name, ok := strings.CutPrefix(target, "field:")
		if !ok {
			continue
		}
		f, known := look.fields[strings.ToLower(name)]
		if !known {
			return nil, fmt.Errorf("%q is not a field of this project", name)
		}
		text := row.value(req.Mapping, target)
		if text == "" {
			continue
		}
		raw, err := fieldValue(f, text)
		if err != nil {
			return nil, err
		}
		customs[f.ID] = raw
	}
	return customs, nil
}
