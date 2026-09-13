package csvio

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/field"
)

// moments are the ways the trackers people leave write a time. Jira's own
// export uses two of them in one file.
var moments = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"02/Jan/06 15:04",
	"02/Jan/2006 15:04",
	"02/Jan/06 3:04 PM",
	"02/Jan/2006 3:04 PM",
	"02/Jan/06",
	"01/02/2006 15:04",
}

// parseMoment reads a time however the file wrote it.
func parseMoment(text string) (time.Time, error) {
	clean := strings.TrimSpace(text)
	// A Jira timestamp column ends in a tenth of a second nobody wants.
	clean = strings.TrimSuffix(clean, ".0")
	for _, layout := range moments {
		if at, err := time.Parse(layout, clean); err == nil {
			return at.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a date this can read", text)
}

// parseDay keeps the day and drops the time, for the fields that are days.
func parseDay(text string) (time.Time, error) {
	at, err := parseMoment(text)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC), nil
}

const (
	// minutesPerHour, hoursPerDay and daysPerWeek are Jira's working week, which
	// is what its exported durations are written in.
	minutesPerHour = 60
	hoursPerDay    = 8
	daysPerWeek    = 5
	secondsPerMin  = 60
)

// parseMinutes reads a duration. A bare number is seconds, as every Jira
// export writes it; a spelled one is 2w 3d 4h 30m.
func parseMinutes(text string) (int, error) {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return 0, nil
	}
	if seconds, err := strconv.ParseFloat(clean, 64); err == nil {
		return int(seconds / secondsPerMin), nil
	}
	units := map[byte]int{
		'w': minutesPerHour * hoursPerDay * daysPerWeek,
		'd': minutesPerHour * hoursPerDay,
		'h': minutesPerHour,
		'm': 1,
	}
	total, read := 0, false
	for _, part := range strings.Fields(clean) {
		if len(part) < 2 {
			return 0, fmt.Errorf("%q is not a length of time", text)
		}
		per, ok := units[part[len(part)-1]]
		if !ok {
			return 0, fmt.Errorf("%q is not a length of time", text)
		}
		n, err := strconv.ParseFloat(part[:len(part)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a length of time", text)
		}
		total += int(n * float64(per))
		read = true
	}
	if !read {
		return 0, fmt.Errorf("%q is not a length of time", text)
	}
	return total, nil
}

// splitList reads the several values one cell may hold.
func splitList(text string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ';' || r == ',' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// keyNumber reads the number out of a key another tracker minted.
func keyNumber(key string) (int64, bool) {
	at := strings.LastIndex(key, "-")
	if at < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(key[at+1:]), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// fieldValue writes a cell as the JSON a custom field of that kind stores.
func fieldValue(f field.Field, text string) (json.RawMessage, error) {
	switch f.Kind {
	case field.Number:
		var n float64
		if _, err := fmt.Sscanf(text, "%g", &n); err != nil {
			return nil, fmt.Errorf("%s %q is not a number", f.Name, text)
		}
		return json.Marshal(n)
	case field.Checkbox:
		switch strings.ToLower(text) {
		case "true", "yes", "1", "x":
			return json.Marshal(true)
		case "false", "no", "0", "":
			return json.Marshal(false)
		}
		return nil, fmt.Errorf("%s %q is not yes or no", f.Name, text)
	case field.Date:
		day, err := parseDay(text)
		if err != nil {
			return nil, fmt.Errorf("%s %q is not a date this can read", f.Name, text)
		}
		return json.Marshal(day.Format(dateLayout))
	default:
		return json.Marshal(text)
	}
}
