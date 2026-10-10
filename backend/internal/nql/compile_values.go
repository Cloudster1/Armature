package nql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// keyPattern is the issue package's, repeated here because the issue package
// is what imports this one.
var keyPattern = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]{0,17})$`)

// keys binds issue keys into a template with two slots, project key and number.
func (c *compiler) keys(values []Value, template string) (string, error) {
	var parts []string
	for _, v := range values {
		if v.Kind != VWord && v.Kind != VString {
			return "", errAt(v.Pos, "%q is not an issue key. Write it like PROJ-12.", v.String())
		}
		m := keyPattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(v.Text)))
		if m == nil {
			return "", errAt(v.Pos, "%q is not an issue key. Write it like PROJ-12.", v.Text)
		}
		num, _ := strconv.ParseInt(m[2], 10, 64)
		parts = append(parts, fmt.Sprintf(template, c.bind(m[1]), c.bind(num)))
	}
	return anyOf(parts), nil
}

// texts reads plain values, normalized, refusing functions and numbers.
func (c *compiler) texts(def fieldDef, values []Value, normalize func(string) string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		text, err := plainText(def, v)
		if err != nil {
			return nil, err
		}
		out = append(out, normalize(text))
	}
	return out, nil
}

func plainText(def fieldDef, v Value) (string, error) {
	switch v.Kind {
	case VWord, VString:
		return v.Text, nil
	case VNumber:
		return v.Text, nil
	case VFunction:
		return "", errAt(v.Pos, "%s() cannot be compared with %s. Write the value itself.", v.Text, def.name)
	default:
		return "", errAt(v.Pos, "%q is not a value %s takes.", v.Text, def.name)
	}
}

func number(def fieldDef, v Value) (float64, error) {
	if v.Kind != VNumber {
		return 0, errAt(v.Pos, "%s is a number, and %s is not one.", def.name, v)
	}
	n, err := strconv.ParseFloat(v.Text, 64)
	if err != nil {
		return 0, errAt(v.Pos, "%q is not a number.", v.Text)
	}
	return n, nil
}

// dateFunctions are the moments a query can name relative to now.
var dateFunctions = []string{"now", "startOfDay", "endOfDay", "startOfWeek", "endOfWeek", "startOfMonth", "endOfMonth"}

// instant resolves a date value. wholeDay says the value named a day with no
// time, which the timestamp comparison turns into a range.
func (c *compiler) instant(def fieldDef, v Value) (time.Time, bool, error) {
	loc := c.env.Location
	switch v.Kind {
	case VDate:
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if at, err := time.ParseInLocation(layout, v.Text, loc); err == nil {
				return at, false, nil
			}
		}
		at, err := time.ParseInLocation("2006-01-02", v.Text, loc)
		if err != nil {
			return time.Time{}, false, errAt(v.Pos, "%q is not a calendar date. Write it like 2026-09-04.", v.Text)
		}
		return at, true, nil
	case VDuration:
		d, err := duration(v)
		if err != nil {
			return time.Time{}, false, err
		}
		return c.env.Now.In(loc).Add(d), false, nil
	case VFunction:
		ref := c.env.Now.In(loc)
		if v.Arg != nil {
			d, err := duration(*v.Arg)
			if err != nil {
				return time.Time{}, false, err
			}
			ref = ref.Add(d)
		}
		y, m, d := ref.Date()
		day := time.Date(y, m, d, 0, 0, 0, 0, loc)
		// Weeks start on Monday, as the plan's weeks do.
		weekday := (int(day.Weekday()) + 6) % 7
		week := day.AddDate(0, 0, -weekday)
		month := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		switch strings.ToLower(v.Text) {
		case "now":
			return ref, false, nil
		case "startofday":
			return day, false, nil
		case "endofday":
			return day.AddDate(0, 0, 1).Add(-time.Second), false, nil
		case "startofweek":
			return week, false, nil
		case "endofweek":
			return week.AddDate(0, 0, 7).Add(-time.Second), false, nil
		case "startofmonth":
			return month, false, nil
		case "endofmonth":
			return month.AddDate(0, 1, 0).Add(-time.Second), false, nil
		}
		return time.Time{}, false, errAt(v.Pos, "There is no function %s(). The date functions are %s.", v.Text, describeOperators(withParens(dateFunctions)))
	}
	return time.Time{}, false, errAt(v.Pos, "%s is not a date. Write 2026-09-04, -7d or startOfWeek().", v)
}

// duration reads -7d, 2w or 36h. A day is 24 hours here; a query does not
// care about the hour a clock changed.
func duration(v Value) (time.Duration, error) {
	text := v.Text
	unit := text[len(text)-1]
	n, err := strconv.Atoi(text[:len(text)-1])
	if err != nil {
		return 0, errAt(v.Pos, "%q is not a duration. Write it like -7d, 2w or 36h.", text)
	}
	switch unit {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
}

func withParens(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + "()"
	}
	return out
}

func anyOf(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func contains(list []string, s string) bool {
	for _, each := range list {
		if each == s {
			return true
		}
	}
	return false
}
