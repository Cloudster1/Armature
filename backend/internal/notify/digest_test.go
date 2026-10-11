package notify

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Each organization mails a person on its own record: one organization's
// bundle going out at the digest hour never makes the other's wait a day.
func TestADailyDigestIsDueInEachOrganizationOfItsOwn(t *testing.T) {
	type key struct{ org, user uuid.UUID }
	person, a, b := uuid.New(), uuid.New(), uuid.New()
	at := func(hour, minute int) time.Time { return time.Date(2026, 3, 2, hour, minute, 0, 0, time.Local) }

	sent := map[key]time.Time{{a, person}: at(DailyDigestHour, 0)}
	if !due(DigestDaily, sent[key{b, person}], at(DailyDigestHour, 0)) {
		t.Error("the second organization's digest waited because the first one's went at the same pass")
	}
	if !due(DigestDaily, sent[key{b, person}], at(DailyDigestHour, 15)) {
		t.Error("the second organization's digest was not due at the next pass either")
	}
	sent[key{b, person}] = at(DailyDigestHour, 15)
	for _, org := range []uuid.UUID{a, b} {
		if due(DigestDaily, sent[key{org, person}], at(DailyDigestHour, 30)) {
			t.Errorf("the organization %s was due again on the day it mailed", org)
		}
	}
}

// A daily digest goes once a day from the digest hour on: a pass that missed
// the hour sends it later that day instead of skipping to tomorrow.
func TestADailyDigestThatMissedItsHourGoesAtTheNextPass(t *testing.T) {
	day := func(d, hour, minute int) time.Time { return time.Date(2026, 3, d, hour, minute, 0, 0, time.Local) }
	cases := []struct {
		name      string
		last, now time.Time
		want      bool
	}{
		{"never sent, before the hour", time.Time{}, day(2, DailyDigestHour-1, 45), false},
		{"never sent, at the hour", time.Time{}, day(2, DailyDigestHour, 0), true},
		{"never sent, the hour missed", time.Time{}, day(2, 14, 30), true},
		{"sent yesterday, the hour missed", day(1, DailyDigestHour, 0), day(2, DailyDigestHour+2, 0), true},
		{"sent yesterday, before the hour", day(1, DailyDigestHour, 0), day(2, DailyDigestHour-1, 0), false},
		{"sent today at the hour", day(2, DailyDigestHour, 0), day(2, DailyDigestHour+1, 0), false},
		{"sent today late", day(2, 23, 0), day(2, 23, 45), false},
		{"sent today before the hour, under another schedule", day(2, DailyDigestHour-1, 30), day(2, DailyDigestHour, 0), true},
	}
	for _, c := range cases {
		if got := due(DigestDaily, c.last, c.now); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}

// An hourly digest goes once an hour whatever the clock says, and anything
// queued under "off" goes at once.
func TestAnHourlyDigestGoesOnceAnHour(t *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 3, 2, hour, minute, 0, 0, time.Local) }
	if !due(DigestHourly, time.Time{}, at(3, 0)) {
		t.Error("an hourly digest never sent was not due")
	}
	if due(DigestHourly, at(3, 0), at(3, 45)) {
		t.Error("an hourly digest was due again within the hour")
	}
	if !due(DigestHourly, at(3, 0), at(4, 0)) {
		t.Error("an hourly digest was not due an hour later")
	}
	if !due(DigestOff, at(3, 0), at(3, 1)) {
		t.Error("rows queued before the person turned the digest off were held back")
	}
}
