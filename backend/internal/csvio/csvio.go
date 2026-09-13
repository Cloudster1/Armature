// Package csvio moves issues in and out as CSV: a query's result out with the
// columns asked for, and a file in, mapped by position and tried dry first.
package csvio

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
)

const (
	// MaxImportRows bounds one file; a bigger one is split by the person.
	MaxImportRows = 5000
	// MaxImportBytes bounds what is read before the rows are even counted.
	MaxImportBytes = 8 << 20
	// PreviewRows is how many values of a column a preview shows.
	PreviewRows = 5
	// ExportRows caps one export, a page of the search at its largest.
	ExportRows = 5000
	// dateLayout is how a day is written in both directions.
	dateLayout = "2006-01-02"
)

// Columns an export may carry, in the order they are offered.
var Columns = []string{"key", "summary", "type", "status", "statusCategory", "priority", "assignee", "reporter", "labels", "sprint", "milestone", "fixVersions", "components", "estimate", "created", "updated", "resolved", "due", "start", "parent"}

// DefaultColumns is what an export carries when nobody chose.
var DefaultColumns = []string{"key", "summary", "type", "status", "priority", "assignee", "created"}

type Service struct {
	db     *db.Cluster
	issues *issue.Service
	labels *label.Service
	fields *field.Service
	auth   *auth.Service
	plan   planning
}

func NewService(cluster *db.Cluster, issues *issue.Service, labels *label.Service, fields *field.Service, accounts *auth.Service) *Service {
	return &Service{db: cluster, issues: issues, labels: labels, fields: fields, auth: accounts}
}

// Export writes the issues a filter matches as CSV with the chosen columns.
func (s *Service) Export(ctx context.Context, w io.Writer, filter issue.Filter, columns []string) error {
	if len(columns) == 0 {
		columns = DefaultColumns
	}
	for _, c := range columns {
		if !contains(Columns, c) {
			return fmt.Errorf("%q is not a column; the columns are %s", c, strings.Join(Columns, ", "))
		}
	}
	result, err := s.issues.List(ctx, filter, issue.Page{Limit: ExportRows, OrderBy: "key"})
	if err != nil {
		return err
	}
	out := csv.NewWriter(w)
	if err := out.Write(columns); err != nil {
		return err
	}
	for _, i := range result.Issues {
		row := make([]string, len(columns))
		for n, c := range columns {
			row[n] = Cell(cell(&i, c))
		}
		if err := out.Write(row); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// Cell is a value as a spreadsheet should read it: text, never a formula. What
// a customer typed lands in an agent's spreadsheet, and "=" there is a program.
func Cell(s string) string {
	if s == "" || !strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return s
	}
	return "'" + s
}

// cell is one issue field as a CSV value.
func cell(i *issue.Issue, column string) string {
	day := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format(dateLayout)
	}
	stamp := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	switch column {
	case "key":
		return i.Key
	case "summary":
		return i.Summary
	case "type":
		return i.Type.Name
	case "status":
		return i.Status.Name
	case "statusCategory":
		return string(i.Status.Category)
	case "priority":
		return string(i.Priority)
	case "assignee":
		if i.Assignee != nil {
			return i.Assignee.Email
		}
	case "reporter":
		if i.Reporter != nil {
			return i.Reporter.Email
		}
	case "labels":
		var names []string
		for _, l := range i.Labels {
			names = append(names, l.Name)
		}
		return strings.Join(names, "; ")
	case "sprint":
		if i.Sprint != nil {
			return i.Sprint.Name
		}
	case "milestone":
		if i.Milestone != nil {
			return i.Milestone.Name
		}
	case "fixVersions":
		var names []string
		for _, v := range i.FixVersions {
			names = append(names, v.Name)
		}
		return strings.Join(names, "; ")
	case "components":
		var names []string
		for _, c := range i.Components {
			names = append(names, c.Name)
		}
		return strings.Join(names, "; ")
	case "estimate":
		if i.Estimate != nil {
			return fmt.Sprintf("%g", *i.Estimate)
		}
	case "created":
		return stamp(&i.CreatedAt)
	case "updated":
		return stamp(&i.UpdatedAt)
	case "resolved":
		return stamp(i.ResolvedAt)
	case "due":
		return day(i.DueDate)
	case "start":
		return day(i.StartDate)
	case "parent":
		return i.ParentKey
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, each := range list {
		if each == s {
			return true
		}
	}
	return false
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
