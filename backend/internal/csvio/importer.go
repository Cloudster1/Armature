package csvio

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// Values says what each word in a column becomes here: Jira's Requested is
// somebody's To Do, and only the person importing knows which.
type Values map[string]map[string]string

// ImportRequest is the file and every decision made about it.
type ImportRequest struct {
	Mapping Mapping `json:"mapping"`
	Values  Values  `json:"values,omitempty"`
	People  People  `json:"people,omitempty"`
	DryRun  bool    `json:"dryRun,omitempty"`
	Source  string  `json:"source,omitempty"`
}

// Refusal is one row that did not become an issue, and why.
type Refusal struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// ImportReport is what an import did, or would have done. A row appears in
// one of imported, updated and refused, never in two.
type ImportReport struct {
	DryRun   bool       `json:"dryRun"`
	Imported []string   `json:"imported"`
	Updated  []string   `json:"updated"`
	Refused  []Refusal  `json:"refused"`
	Partial  []Refusal  `json:"partial"`
	Notes    []string   `json:"notes"`
	Rows     int        `json:"rows"`
	Mapping  Mapping    `json:"mapping"`
	Finished time.Time  `json:"finished"`
	JobID    *uuid.UUID `json:"jobId,omitempty"`
}

// Preview says what the file holds and what its words look like here, so the
// mapping is made against the file rather than against a guess.
func (s *Service) Preview(ctx context.Context, projectKey string, data []byte, mapping Mapping) (*Preview, Mapping, error) {
	headers, rows, err := Read(data)
	if err != nil {
		return nil, nil, err
	}
	if len(mapping) == 0 {
		mapping = Guess(headers)
	}
	look, err := s.lookups(ctx, projectKey)
	if err != nil {
		return nil, nil, err
	}
	columns := make([]Column, len(headers))
	for at, name := range headers {
		columns[at] = Column{At: at, Name: name, Samples: samples(rows, at)}
	}
	p := &Preview{Columns: columns, Total: len(rows), Words: words(rows, mapping, look), People: found(rows, mapping, look)}
	return p, mapping, nil
}

// samples are the first values a column holds, which is how a person tells
// two columns of the same name apart.
func samples(rows []Row, at int) []string {
	out := []string{}
	for _, row := range rows {
		if at >= len(row.Cells) {
			continue
		}
		if v := strings.TrimSpace(row.Cells[at]); v != "" {
			out = append(out, v)
		}
		if len(out) == PreviewRows {
			break
		}
	}
	return out
}

// wordTargets are the columns whose values are names of things here, and so
// have to mean something before a row can be written.
var wordTargets = []string{"type", "status", "priority"}

func words(rows []Row, mapping Mapping, look *lookups) []Word {
	out := []Word{}
	for _, target := range wordTargets {
		counts := map[string]int{}
		for _, row := range rows {
			for _, value := range row.values(mapping, target) {
				counts[value]++
			}
		}
		for value, count := range counts {
			out = append(out, Word{Target: target, Value: value, Count: count, Means: meansHere(target, value, look)})
		}
	}
	return out
}

// meansHere is the guess a name gets before anybody says otherwise.
func meansHere(target, value string, look *lookups) string {
	lower := strings.ToLower(value)
	switch target {
	case "type":
		if _, ok := look.types[lower]; ok {
			return value
		}
	case "status":
		if _, ok := look.statuses[lower]; ok {
			return value
		}
	case "priority":
		if issue.Priority(lower).Valid() {
			return lower
		}
	}
	return ""
}

// placed is a row and the issue it became, kept so that what the row says
// about other issues can be written once they all exist.
type placed struct {
	row Row
	key string
}

// runState is what one import knows as it goes: what it has written, what the
// file promises to write, and which keys it has already asked the project for.
type runState struct {
	written  map[string]string
	provides map[string]bool
	known    map[string]bool
	dry      bool
}

// fileKeys are the keys the file itself carries, which is what lets a dry run
// answer a question about a parent it has not written yet.
func fileKeys(rows []Row, mapping Mapping) map[string]bool {
	out := map[string]bool{}
	for _, row := range rows {
		if key := row.value(mapping, "key"); key != "" {
			out[key] = true
		}
	}
	return out
}

// note keeps the report to one line per thing, however many rows say it.
func note(report *ImportReport, text string) {
	for _, had := range report.Notes {
		if had == text {
			return
		}
	}
	report.Notes = append(report.Notes, text)
}

// parentHere is the key this run gave a parent, or nothing when the file names
// one it does not contain: an issue loses its parent rather than its place.
func (s *Service) parentHere(ctx context.Context, named string, run *runState, report *ImportReport) string {
	if named == "" {
		return ""
	}
	if here, ok := run.written[named]; ok {
		return here
	}
	// A dry run has written nothing, so what the file carries stands in for it.
	if run.dry && run.provides[named] {
		return named
	}
	found, asked := run.known[named]
	if !asked {
		_, err := s.issues.ByKey(ctx, named)
		found = err == nil
		run.known[named] = found
	}
	if found {
		return named
	}
	note(report, fmt.Sprintf("%s is named as a parent and is not in this file; what was under it came in without one.", named))
	return ""
}

// Import writes an issue per row, or says what it would write. Each row is its
// own transaction: a bad row is refused and the rest go on.
func (s *Service) Import(ctx context.Context, projectKey string, data []byte, req ImportRequest, actor issue.Actor) (*ImportReport, db.LSN, error) {
	headers, rows, err := Read(data)
	if err != nil {
		return nil, 0, err
	}
	if len(req.Mapping) == 0 {
		req.Mapping = Guess(headers)
	}
	if err := check(req.Mapping, headers); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "a file"
	}
	look, err := s.lookups(ctx, projectKey)
	if err != nil {
		return nil, 0, err
	}
	report := &ImportReport{
		DryRun: req.DryRun, Imported: []string{}, Updated: []string{},
		Refused: []Refusal{}, Partial: []Refusal{}, Notes: []string{},
		Rows: len(rows), Mapping: req.Mapping,
	}
	if err := s.defineFields(ctx, projectKey, req.Mapping, req.DryRun, look, report); err != nil {
		return nil, 0, err
	}
	people, err := s.resolve(ctx, found(rows, req.Mapping, look), req.People, req.DryRun, report)
	if err != nil {
		return nil, 0, err
	}
	names, err := s.namesInProject(ctx, projectKey)
	if err != nil {
		return nil, 0, err
	}

	importer := actor
	importer.Import = true
	run := &runState{
		written: map[string]string{}, known: map[string]bool{},
		provides: fileKeys(rows, req.Mapping), dry: req.DryRun,
	}
	var (
		latest db.LSN
		done   []placed
	)
	for _, row := range parentsFirst(rows, req, look) {
		key, lsn := s.importRow(ctx, projectKey, row, req, look, people, run, names, importer, report)
		latest = max(latest, lsn)
		if key != "" {
			done = append(done, placed{row: row, key: key})
		}
	}
	// What a row says about other issues waits until they all exist.
	for _, each := range done {
		latest = max(latest, s.attachRest(ctx, each.row, req, each.key, people, run.written, importer, report))
	}

	report.Finished = time.Now().UTC()
	jobID, lsn, err := s.record(ctx, projectKey, actor, report, req.Source)
	if err != nil {
		return nil, 0, err
	}
	report.JobID = &jobID
	return report, max(latest, lsn), nil
}

// parentsFirst puts the epics before their stories, so a parent named by a row
// already exists when the row is written.
func parentsFirst(rows []Row, req ImportRequest, look *lookups) []Row {
	order := make([]Row, len(rows))
	copy(order, rows)
	level := func(r Row) int {
		word := r.value(req.Mapping, "type")
		if word == "" {
			return issue.LevelStandard
		}
		return look.levels[strings.ToLower(req.Values.means("type", word))]
	}
	sort.SliceStable(order, func(a, b int) bool { return level(order[a]) > level(order[b]) })
	return order
}

// importRow writes one row, says in the report what became of it, and answers
// with the key it was given here.
func (s *Service) importRow(ctx context.Context, projectKey string, row Row, req ImportRequest,
	look *lookups, people map[string]uuid.UUID, run *runState, names *named,
	importer issue.Actor, report *ImportReport) (string, db.LSN) {
	in, labels, customs, err := s.rowInput(projectKey, row, req, look, people)
	if err != nil {
		report.Refused = append(report.Refused, Refusal{Row: row.Line, Reason: capitalize(err.Error())})
		return "", 0
	}
	places := s.placesFor(ctx, projectKey, row, req, names, importer.UserID, report)
	in.SprintID, in.TeamID, in.ComponentIDs = places.sprint, places.team, places.components
	in.ParentKey = s.parentHere(ctx, in.ParentKey, run, report)
	if req.DryRun {
		report.Imported = append(report.Imported, fmt.Sprintf("row %d: %s", row.Line, in.Summary))
		return "", 0
	}
	result, latest, err := s.issues.Import(ctx, in, importer)
	if err != nil {
		report.Refused = append(report.Refused, Refusal{Row: row.Line, Reason: capitalize(err.Error())})
		return "", 0
	}
	key := result.Issue.Key
	if in.ExternalKey != "" {
		run.written[in.ExternalKey] = key
	}
	if len(labels) > 0 {
		if _, lsn, err := s.labels.SetIssueLabels(ctx, key, labels, importer); err != nil {
			report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s was written, but its labels were refused: %v", key, err)})
		} else {
			latest = max(latest, lsn)
		}
	}
	if len(places.fix) > 0 || len(places.affects) > 0 {
		if _, lsn, err := s.issues.SetVersions(ctx, key, places.fix, places.affects, importer); err != nil {
			report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s was written, but its versions were refused: %v", key, err)})
		} else {
			latest = max(latest, lsn)
		}
	}
	for fieldID, raw := range customs {
		if _, lsn, err := s.fields.Set(ctx, key, fieldID, raw, importer); err != nil {
			report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s was written, but a field was refused: %v", key, err)})
		} else {
			latest = max(latest, lsn)
		}
	}
	if result.Updated {
		report.Updated = append(report.Updated, key)
	} else {
		report.Imported = append(report.Imported, key)
	}
	return key, latest
}

// attachRest writes what a row says beyond the issue itself: the conversation
// it had, the time logged on it, and what it points at.
func (s *Service) attachRest(ctx context.Context, row Row, req ImportRequest, key string,
	people map[string]uuid.UUID, written map[string]string, importer issue.Actor, report *ImportReport) db.LSN {
	var latest db.LSN
	for n, cell := range row.values(req.Mapping, "comments") {
		spoken, ok := parseComment(cell)
		if !ok {
			report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s kept its comment of %s, which this could not read", key, cell)})
			continue
		}
		author := personID(people, spoken.author)
		// Where the cell sits in the row is part of the name, because two
		// entries written in one minute are two entries.
		external := fmt.Sprintf("%s:comment:%d:%d", key, n, spoken.at.UnixNano())
		lsn, err := s.issues.AddImportedComment(ctx, key, issue.WikiDocument(spoken.body), author, spoken.at, external, importer)
		if err != nil {
			report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s kept a comment out: %v", key, err)})
			continue
		}
		latest = max(latest, lsn)
	}
	for n, cell := range row.values(req.Mapping, "worklogs") {
		work, ok := parseWorklog(cell)
		if !ok {
			report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s kept its worklog of %s, which this could not read", key, cell)})
			continue
		}
		day := work.at
		external := fmt.Sprintf("%s:worklog:%d:%d", key, n, work.at.UnixNano())
		lsn, err := s.issues.LogImportedWork(ctx, key,
			issue.WorklogInput{Minutes: work.minutes, StartedOn: &day, Note: work.note},
			personID(people, work.author), external, importer)
		if err != nil {
			report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s kept a worklog out: %v", key, err)})
			continue
		}
		latest = max(latest, lsn)
	}
	return max(latest, s.attachLinks(ctx, row, req, key, written, importer, report))
}

// attachLinks draws what the file says between two issues, in the direction
// the column it came from means.
func (s *Service) attachLinks(ctx context.Context, row Row, req ImportRequest, key string,
	written map[string]string, importer issue.Actor, report *ImportReport) db.LSN {
	var latest db.LSN
	for target, link := range linkTargets {
		for _, cell := range row.values(req.Mapping, target) {
			for _, named := range splitList(cell) {
				other, ok := written[named]
				if !ok {
					report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s points at %s, which is not in this file", key, named)})
					continue
				}
				from, to := key, other
				if !link.outward {
					from, to = other, key
				}
				_, lsn, err := s.issues.AddLink(ctx, from, issue.LinkInput{TypeName: link.kind, TargetKey: to}, importer)
				if err != nil {
					// The same pair usually appears from both ends of a file.
					if !strings.Contains(err.Error(), "already linked") {
						report.Partial = append(report.Partial, Refusal{Row: row.Line, Reason: fmt.Sprintf("%s could not be linked to %s: %v", from, to, err)})
					}
					continue
				}
				latest = max(latest, lsn)
			}
		}
	}
	return latest
}

// personID is whoever the file named, or nobody, which a comment reads as
// having been written by the system.
func personID(people map[string]uuid.UUID, name string) *uuid.UUID {
	if id, ok := people[personKey(name)]; ok {
		return &id
	}
	return nil
}

// defineFields makes the project fields a mapping asked for, so a column of
// another tracker's own invention has somewhere to land.
func (s *Service) defineFields(ctx context.Context, projectKey string, mapping Mapping, dry bool,
	look *lookups, report *ImportReport) error {
	for target, positions := range mapping {
		rest, ok := strings.CutPrefix(target, "field:new:")
		if !ok {
			continue
		}
		name, kind, ok := strings.Cut(rest, ":")
		if !ok || strings.TrimSpace(kind) == "" {
			kind = string(field.Text)
		}
		delete(mapping, target)
		mapping["field:"+name] = positions
		if _, known := look.fields[strings.ToLower(name)]; known {
			continue
		}
		if !field.Kind(kind).Valid() {
			return fmt.Errorf("%q is not a kind of field", kind)
		}
		if dry {
			look.fields[strings.ToLower(name)] = field.Field{Name: name, Kind: field.Kind(kind)}
			note(report, fmt.Sprintf("A %s field called %s would be defined for this project.", kind, name))
			continue
		}
		made, _, err := s.fields.Create(ctx, projectKey, field.Input{Name: name, Kind: field.Kind(kind)})
		if err != nil {
			return err
		}
		look.fields[strings.ToLower(name)] = *made
		note(report, fmt.Sprintf("A %s field called %s was defined for this project.", kind, name))
	}
	return nil
}

// record keeps what the import did, for the page and for the record.
func (s *Service) record(ctx context.Context, projectKey string, actor issue.Actor, report *ImportReport, source string) (uuid.UUID, db.LSN, error) {
	var id uuid.UUID
	raw, _ := json.Marshal(report)
	mapping, _ := json.Marshal(report.Mapping)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			INSERT INTO import_job (org_id, project_id, actor_id, mapping, dry_run, report, filename)
			SELECT current_org_id(), id, $2, $3, $4, $5, $6 FROM project WHERE key = $1 RETURNING id`,
			project.NormalizeKey(projectKey), actor.UserID, mapping, report.DryRun, raw, source).Scan(&id)
	})
	return id, lsn, err
}
