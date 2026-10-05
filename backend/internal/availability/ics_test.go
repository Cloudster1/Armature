package availability

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// calendarFile wraps events in the envelope every exporter writes, with the
// line endings the format asks for.
func calendarFile(events ...string) string {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Test//Holidays//EN"}
	for _, e := range events {
		lines = append(lines, "BEGIN:VEVENT")
		lines = append(lines, strings.Split(e, "\n")...)
		lines = append(lines, "END:VEVENT")
	}
	lines = append(lines, "END:VCALENDAR")
	return strings.Join(lines, "\r\n") + "\r\n"
}

func TestAnICSFileBecomesHolidays(t *testing.T) {
	file := calendarFile(
		"UID:1\nDTSTART;VALUE=DATE:20261225\nDTEND;VALUE=DATE:20261226\nSUMMARY:Christmas Day",
		// A name folded over two lines, with the escapes the format uses.
		"UID:2\nDTSTART;VALUE=DATE:20261231\nSUMMARY:New Year's Eve\\, the office\n  closes early\\; really",
		// No VALUE parameter, and an alarm whose own summary is not the event's.
		"UID:3\nDTSTART:20260101\nSUMMARY:New Year's Day\nBEGIN:VALARM\nSUMMARY:Reminder\nTRIGGER:-PT15M\nEND:VALARM",
		// A cancelled event is not a day off.
		"UID:4\nDTSTART;VALUE=DATE:20260704\nSTATUS:CANCELLED\nSUMMARY:Picnic",
	)
	got, err := ImportICS(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	want := []Holiday{
		{Day: "2026-01-01", Name: "New Year's Day"},
		{Day: "2026-12-25", Name: "Christmas Day"},
		{Day: "2026-12-31", Name: "New Year's Eve, the office closes early; really"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("holidays =\n%v\nwant\n%v", got, want)
	}
}

func TestAnEventOverSeveralDaysIsADayOffForEach(t *testing.T) {
	got, err := ImportICS(strings.NewReader(calendarFile(
		"DTSTART;VALUE=DATE:20261224\nDTEND;VALUE=DATE:20261227\nSUMMARY:Christmas",
		"DTSTART;VALUE=DATE:20270101\nSUMMARY:",
	)))
	if err != nil {
		t.Fatal(err)
	}
	days := []string{}
	for _, h := range got {
		days = append(days, h.Day+" "+h.Name)
	}
	want := "2026-12-24 Christmas,2026-12-25 Christmas,2026-12-26 Christmas,2027-01-01 " + untitledHoliday
	if strings.Join(days, ",") != want {
		t.Errorf("days = %v, want %s: the end is exclusive", days, want)
	}
}

func TestAnICSFileWithUnixLineEndingsAndAByteOrderMarkReads(t *testing.T) {
	file := byteOrderMark + strings.ReplaceAll(calendarFile("DTSTART;VALUE=DATE:20260501\nSUMMARY:Labour Day"), "\r\n", "\n")
	got, err := ImportICS(strings.NewReader(file))
	if err != nil || len(got) != 1 || got[0].Day != "2026-05-01" {
		t.Errorf("holidays = %v, %v", got, err)
	}
}

func TestAnICSFileThatCannotBeTakenIsRefusedWithASentence(t *testing.T) {
	cases := map[string]struct {
		file, says string
	}{
		"a recurring event": {
			calendarFile("DTSTART;VALUE=DATE:20260101\nRRULE:FREQ=YEARLY\nSUMMARY:New Year's Day"),
			"written out one by one",
		},
		"a date that is not one": {
			calendarFile("DTSTART;VALUE=DATE:20261341\nSUMMARY:Nonsense"),
			"YYYYMMDD",
		},
		"a time of day": {
			calendarFile("DTSTART:20261225T090000Z\nSUMMARY:Party"),
			"all-day events",
		},
		"an end before the start": {
			calendarFile("DTSTART;VALUE=DATE:20261225\nDTEND;VALUE=DATE:20261225\nSUMMARY:Nothing"),
			"ends before it starts",
		},
		"an event with no start": {
			calendarFile("SUMMARY:Floating"),
			"DTSTART",
		},
		"a file that is not a calendar": {
			"name,day\nChristmas,2026-12-25\n",
			"not an iCalendar file",
		},
		"a calendar with no events": {
			calendarFile(),
			"no events",
		},
		"an event longer than a calendar holds": {
			calendarFile("DTSTART;VALUE=DATE:20000101\nDTEND;VALUE=DATE:20300101\nSUMMARY:Forever"),
			"import one year at a time",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ImportICS(strings.NewReader(c.file))
			if !errors.Is(err, ErrBadFile) {
				t.Fatalf("error = %v, want the file refused", err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("error = %q, want it to say %q", err, c.says)
			}
		})
	}
}

func TestAnICSFileLargerThanTheLimitIsRefused(t *testing.T) {
	padding := "X-NOTE:" + strings.Repeat("x", 70)
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	for b.Len() <= MaxICSBytes {
		b.WriteString(padding + "\r\n")
	}
	_, err := ImportICS(strings.NewReader(b.String()))
	if !errors.Is(err, ErrBadFile) || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error = %v, want the size refused", err)
	}
}
