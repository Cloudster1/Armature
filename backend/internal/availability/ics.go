package availability

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxICSBytes is the largest calendar file read; years of holidays fit in kilobytes.
	MaxICSBytes = 1 << 20
	// icsDateLayout is a DATE value as RFC 5545 writes it.
	icsDateLayout = "20060102"
	// untitledHoliday names an event that has no summary, which is still a day off.
	untitledHoliday = "Holiday"
	// byteOrderMark is what some exporters put before the first line.
	byteOrderMark    = "\xef\xbb\xbf"
	bytesPerKilobyte = 1 << 10
	icsDaysPerWeek   = 7
)

// icsLength is a DURATION in whole weeks and days, such as P3D, P1W or P1W2D.
var icsLength = regexp.MustCompile(`^\+?P(?:(\d+)W)?(?:(\d+)D)?$`)

// icsEvent is what one VEVENT says about the days it covers.
type icsEvent struct {
	start, end string
	// length is the days a DURATION gives instead of an end, zero when none does.
	length    int
	summary   string
	recurring bool
	cancelled bool
}

// ImportICS reads the all-day events of an iCalendar file as holidays, one per
// day an event covers. A recurring event is refused rather than guessed at.
func ImportICS(r io.Reader) ([]Holiday, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 || !strings.EqualFold(lines[0], "BEGIN:VCALENDAR") {
		return nil, badFile("that is not an iCalendar file; export the calendar as .ics and upload that")
	}

	var events []icsEvent
	var current *icsEvent
	// Components inside an event, such as an alarm, have properties that are not the event's.
	depth := 0
	for _, line := range lines {
		name, params, value := contentLine(line)
		switch {
		case name == "BEGIN" && strings.EqualFold(value, "VEVENT") && current == nil:
			current = &icsEvent{}
			depth = 0
		case current == nil:
			continue
		case name == "BEGIN":
			depth++
		case name == "END" && depth > 0:
			depth--
		case name == "END" && strings.EqualFold(value, "VEVENT"):
			events = append(events, *current)
			current = nil
		case depth > 0:
			continue
		case name == "DTSTART":
			if current.start, err = icsDate(params, value); err != nil {
				return nil, err
			}
		case name == "DTEND":
			if current.end, err = icsDate(params, value); err != nil {
				return nil, err
			}
		case name == "DURATION":
			if current.length, err = icsDuration(value); err != nil {
				return nil, err
			}
		case name == "SUMMARY":
			current.summary = unescapeText(value)
		case name == "RRULE" || name == "RDATE":
			current.recurring = true
		case name == "STATUS" && strings.EqualFold(strings.TrimSpace(value), "CANCELLED"):
			current.cancelled = true
		}
	}
	if len(events) == 0 {
		return nil, badFile("the file has no events in it; export a calendar that has its holidays in it")
	}

	byDay := map[string]Holiday{}
	for _, e := range events {
		days, err := e.days()
		if err != nil {
			return nil, err
		}
		for _, day := range days {
			byDay[day] = Holiday{Day: day, Name: holidayName(e.summary)}
		}
		if len(byDay) > MaxDays {
			return nil, badFile("the file holds more than %d days; import one year at a time", MaxDays)
		}
	}
	out := make([]Holiday, 0, len(byDay))
	for _, h := range byDay {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out, nil
}

// days lists the days an event covers. Its end is exclusive, as the format
// says; an event with no end, or one that ends the day it starts, is that day.
func (e icsEvent) days() ([]string, error) {
	switch {
	case e.cancelled:
		return nil, nil
	case e.recurring:
		return nil, badFile("%q repeats; export the calendar with recurring days written out one by one, then import it again", e.summary)
	case e.start == "":
		return nil, badFile("%q has no start day; give every event a DTSTART", e.summary)
	}
	start, _ := time.Parse(icsDateLayout, e.start)
	end := start.AddDate(0, 0, 1)
	switch {
	case e.end != "":
		last, _ := time.Parse(icsDateLayout, e.end)
		if last.Before(start) {
			return nil, badFile("%q ends before it starts; fix its DTEND", e.summary)
		}
		if last.After(start) {
			end = last
		}
	case e.length > 0:
		end = start.AddDate(0, 0, e.length)
	}
	var out []string
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		if len(out) == MaxDays {
			return nil, badFile("%q covers more than %d days; import one year at a time", e.summary, MaxDays)
		}
		out = append(out, day.Format(DateLayout))
	}
	return out, nil
}

// icsDate reads a DATE value. A time of day is refused: a holiday is a whole
// day, and an instant would have to be guessed into one.
func icsDate(params, value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "T") || strings.Contains(strings.ToUpper(params), "VALUE=DATE-TIME") {
		return "", badFile("%q is a time of day; export holidays as all-day events", value)
	}
	day, err := time.Parse(icsDateLayout, value)
	if err != nil {
		return "", badFile("%q is not a day written as YYYYMMDD; fix the event and import it again", value)
	}
	return day.Format(icsDateLayout), nil
}

// icsDuration reads a DURATION as whole days. A length with hours in it is
// refused for the same reason a time of day is.
func icsDuration(value string) (int, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if strings.HasPrefix(strings.TrimPrefix(value, "+"), "P") && strings.Contains(value, "T") {
		return 0, badFile("%q lasts part of a day; export holidays as all-day events", value)
	}
	parts := icsLength.FindStringSubmatch(value)
	if parts == nil || (parts[1] == "" && parts[2] == "") {
		return 0, badFile("%q is not a length in days such as P3D; fix the event's DURATION", value)
	}
	weeks, _ := strconv.Atoi(parts[1])
	days, _ := strconv.Atoi(parts[2])
	return weeks*icsDaysPerWeek + days, nil
}

// unfold joins the lines a long property was folded into: a line starting
// with a space or a tab carries on the one before it.
func unfold(r io.Reader) ([]string, error) {
	body, err := io.ReadAll(io.LimitReader(r, MaxICSBytes+1))
	if err != nil {
		return nil, badFile("the file could not be read; export it again and upload the new copy")
	}
	if len(body) > MaxICSBytes {
		return nil, badFile("the file is larger than %d kilobytes; import one year at a time", MaxICSBytes/bytesPerKilobyte)
	}
	if !utf8.Valid(body) {
		return nil, badFile("the file is not written in UTF-8; export it again as UTF-8")
	}
	var lines []string
	for _, raw := range strings.Split(strings.TrimPrefix(string(body), byteOrderMark), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// contentLine splits "NAME;PARAM=x:value" at the first colon outside quotes.
func contentLine(line string) (name, params, value string) {
	quoted := false
	for i, c := range line {
		switch {
		case c == '"':
			quoted = !quoted
		case c == ':' && !quoted:
			name, params, _ = strings.Cut(line[:i], ";")
			return strings.ToUpper(strings.TrimSpace(name)), params, line[i+1:]
		}
	}
	return strings.ToUpper(strings.TrimSpace(line)), "", ""
}

// unescapeText undoes the escapes a TEXT value is written with. A name is
// one line, so a newline in it becomes a space.
func unescapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i == len(s)-1 {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte(' ')
		default:
			b.WriteByte(s[i])
		}
	}
	return strings.TrimSpace(b.String())
}

// holidayName is the summary held to a name's bounds.
func holidayName(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return untitledHoliday
	}
	if utf8.RuneCountInString(summary) > MaxNameLength {
		summary = strings.TrimSpace(string([]rune(summary)[:MaxNameLength]))
	}
	return summary
}

func badFile(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBadFile}, args...)...)
}
