package report

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/db"
)

// The words a chart falls back to when it is not told, and the two the server
// reads rather than passes on.
const (
	defaultGroup    = "status"
	defaultMeasure  = "count"
	defaultShape    = "bar"
	defaultInterval = "week"
	defaultSeries   = "created"
	// groupNone is a line over everything rather than one line per group.
	groupNone = "none"
	// shapeLine is the shape the server draws with a different query.
	shapeLine = "line"
)

// isLine says whether the chart is drawn over time.
func (p Params) isLine() bool { return p.Shape == shapeLine }

// chartGroup is one thing a chart may group by: the expression that reads it
// and the join it needs. The map is the whitelist; nothing else reaches SQL.
type chartGroup struct {
	expr string
	join string
	// category names the status category beside a status label, so the page
	// can draw the learned colours; empty for anything else.
	category string
}

var chartGroups = map[string]chartGroup{
	"status":         {expr: `st.name`, category: `st.category::text`},
	"statusCategory": {expr: `st.category::text`, category: `st.category::text`},
	"type":           {expr: `it.name`},
	"priority":       {expr: `i.priority::text`},
	"assignee":       {expr: `COALESCE(u.name, 'Unassigned')`, join: `LEFT JOIN app_user u ON u.id = i.assignee_id`},
	"team":           {expr: `COALESCE(tm.name, 'No team')`, join: `LEFT JOIN team tm ON tm.id = i.team_id`},
	"milestone":      {expr: `COALESCE(m.name, 'No milestone')`, join: `LEFT JOIN milestone m ON m.id = i.milestone_id`},
	// An issue with two labels counts once per label: a chart by label is a
	// chart of labels, not of issues, and the total says so.
	"label": {expr: `COALESCE(l.name, 'No label')`, join: `LEFT JOIN issue_label il ON il.issue_id = i.id LEFT JOIN label l ON l.id = il.label_id`},
	// Likewise an issue fixing two versions, or in two components, counts once per each.
	"fixVersion": {expr: `COALESCE(fv.name, 'No version')`, join: `LEFT JOIN issue_version ivf ON ivf.issue_id = i.id AND ivf.role = 'fix' LEFT JOIN version fv ON fv.id = ivf.version_id`},
	"component":  {expr: `COALESCE(co.name, 'No component')`, join: `LEFT JOIN issue_component ic ON ic.issue_id = i.id LEFT JOIN component co ON co.id = ic.component_id`},
}

// chartMeasures is what a chart may count: issues, or the points on them.
var chartMeasures = map[string]string{
	"count":  `count(*)::float8`,
	"points": `COALESCE(sum(i.estimate), 0)::float8`,
}

// chartIntervals is how a line over time may be bucketed, both halves bound.
var chartIntervals = map[string]string{
	"week":  "1 week",
	"month": "1 month",
}

// chartSeries is what a line counts in each bucket.
var chartSeries = map[string]string{
	"created":  `i.created_at >= b.start AND i.created_at < b.start + $5::interval`,
	"resolved": `i.resolved_at >= b.start AND i.resolved_at < b.start + $5::interval`,
	"open":     `i.created_at < b.start + $5::interval AND (i.resolved_at IS NULL OR i.resolved_at >= b.start + $5::interval)`,
}

// chartShapes are the drawings the page knows; the server only cares whether
// the shape is a line, which is a different query.
var chartShapes = map[string]bool{"bar": true, "stacked": true, "donut": true, "line": true}

func sortedKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func chartOption[V any](m map[string]V, value, fallback, what string) (string, V, error) {
	if value == "" {
		value = fallback
	}
	v, ok := m[value]
	if !ok {
		var zero V
		return "", zero, fmt.Errorf("%w: %s must be one of %s.", ErrBadChart, what, sortedKeys(m))
	}
	return value, v, nil
}

// chart groups the scoped issues by one field, and within each group by a
// second when asked, measuring issues or points.
func chart(ctx context.Context, tx db.DBTX, sc scope, p Params) (*ChartReport, error) {
	groupBy, group, err := chartOption(chartGroups, p.GroupBy, defaultGroup, "groupBy")
	if err != nil {
		return nil, err
	}
	measure, measureExpr, err := chartOption(chartMeasures, p.Measure, defaultMeasure, "measure")
	if err != nil {
		return nil, err
	}
	if _, _, err := chartOption(chartShapes, p.Shape, defaultShape, "shape"); err != nil {
		return nil, err
	}
	splitExpr, joins := `''`, group.join
	var splitBy string
	if p.SplitBy != "" {
		name, split, err := chartOption(chartGroups, p.SplitBy, "", "splitBy")
		if err != nil {
			return nil, err
		}
		if name == groupBy {
			return nil, fmt.Errorf("%w: splitBy must differ from groupBy.", ErrBadChart)
		}
		splitBy, splitExpr = name, split.expr
		if split.join != "" && split.join != group.join {
			joins += " " + split.join
		}
	}
	category := `''`
	if group.category != "" {
		category = group.category
	}

	where, args := sc.clause("i")
	rows, err := tx.Query(ctx, `
		SELECT `+group.expr+`, `+splitExpr+`, `+category+`, `+measureExpr+`
		FROM issue i
		JOIN issue_status st ON st.id = i.status_id
		JOIN issue_type it ON it.id = i.issue_type_id
		`+joins+`
		WHERE `+where+` AND `+notSubtask+`
		GROUP BY 1, 2, 3
		ORDER BY 1, 2`, args...)
	if err != nil {
		return nil, fmt.Errorf("chart: %w", err)
	}
	defer rows.Close()

	out := &ChartReport{Groups: []ChartGroup{}, GroupBy: groupBy, SplitBy: splitBy, Measure: measure}
	byLabel := map[string]*ChartGroup{}
	for rows.Next() {
		var label, part, cat string
		var value float64
		if err := rows.Scan(&label, &part, &cat, &value); err != nil {
			return nil, err
		}
		g, ok := byLabel[label]
		if !ok {
			g = &ChartGroup{Label: label, Category: cat, Parts: []ChartPart{}}
			byLabel[label] = g
		}
		g.Value += value
		out.Total += value
		if splitBy != "" {
			g.Parts = append(g.Parts, ChartPart{Label: part, Value: value})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, g := range byLabel {
		sort.SliceStable(g.Parts, func(a, b int) bool { return g.Parts[a].Value > g.Parts[b].Value })
		out.Groups = append(out.Groups, *g)
	}
	// Largest first, so the eye lands on what matters and the legend reads in order.
	sort.SliceStable(out.Groups, func(a, b int) bool {
		if out.Groups[a].Value != out.Groups[b].Value {
			return out.Groups[a].Value > out.Groups[b].Value
		}
		return out.Groups[a].Label < out.Groups[b].Label
	})
	return out, nil
}

// chartLines counts what was created, resolved or open per bucket, one line
// per group when asked. Empty buckets stay, so the gaps show as gaps.
func chartLines(ctx context.Context, tx db.DBTX, sc scope, p Params) (*ChartSeriesReport, error) {
	interval, step, err := chartOption(chartIntervals, p.Interval, defaultInterval, "interval")
	if err != nil {
		return nil, err
	}
	series, condition, err := chartOption(chartSeries, p.Series, defaultSeries, "series")
	if err != nil {
		return nil, err
	}
	measure, measureExpr, err := chartOption(chartMeasures, p.Measure, defaultMeasure, "measure")
	if err != nil {
		return nil, err
	}
	measureExpr = strings.Replace(measureExpr, "count(*)", "count(i.id)", 1)
	groupExpr, joins := `''`, ""
	var groupBy string
	if p.GroupBy != "" && p.GroupBy != groupNone {
		name, g, err := chartOption(chartGroups, p.GroupBy, "", "groupBy")
		if err != nil {
			return nil, err
		}
		groupBy, groupExpr, joins = name, g.expr, g.join
	}

	// The scope goes into the join, so a bucket with nothing in it is still a row.
	where, args := sc.clause("i", sc.days, interval, step)
	rows, err := tx.Query(ctx, `
		WITH buckets AS (
		    SELECT generate_series(
		        date_trunc($4, now() - make_interval(days => $3)),
		        date_trunc($4, now()), $5::interval) AS start
		)
		SELECT b.start, COALESCE(`+groupExpr+`, ''), `+measureExpr+`
		FROM buckets b
		LEFT JOIN issue i ON `+where+` AND `+condition+`
		LEFT JOIN issue_status st ON st.id = i.status_id
		LEFT JOIN issue_type it ON it.id = i.issue_type_id
		`+joins+`
		WHERE i.id IS NULL OR `+notSubtask+`
		GROUP BY 1, 2
		ORDER BY 1, 2`, args...)
	if err != nil {
		return nil, fmt.Errorf("chart over time: %w", err)
	}
	defer rows.Close()

	out := &ChartSeriesReport{Series: []ChartLine{}, Interval: interval, Days: sc.days, Measure: measure, GroupBy: groupBy, Counts: series}
	var starts []time.Time
	seen := map[time.Time]bool{}
	values := map[string]map[time.Time]float64{}
	var names []string
	for rows.Next() {
		var start time.Time
		var name string
		var value float64
		if err := rows.Scan(&start, &name, &value); err != nil {
			return nil, err
		}
		if !seen[start] {
			seen[start] = true
			starts = append(starts, start)
		}
		if groupBy == "" {
			name = series
		}
		if _, ok := values[name]; !ok {
			values[name] = map[time.Time]float64{}
			names = append(names, name)
		}
		values[name][start] += value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(names)
	for _, name := range names {
		// An empty bucket joins nothing and names no group; it is every line's zero.
		if name == "" && groupBy != "" {
			continue
		}
		line := ChartLine{Name: name, Points: make([]ChartPoint, 0, len(starts))}
		for _, start := range starts {
			line.Points = append(line.Points, ChartPoint{Start: start, Value: values[name][start]})
		}
		out.Series = append(out.Series, line)
	}
	if len(out.Series) == 0 {
		line := ChartLine{Name: series, Points: make([]ChartPoint, 0, len(starts))}
		for _, start := range starts {
			line.Points = append(line.Points, ChartPoint{Start: start})
		}
		out.Series = append(out.Series, line)
	}
	return out, nil
}
