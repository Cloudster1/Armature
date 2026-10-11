package report

import (
	"testing"
	"time"
)

func TestReleaseBurndownSpan(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, time.March, d, 0, 0, 0, 0, time.UTC) }
	at := func(d, hour int) *time.Time { v := day(d).Add(time.Duration(hour) * time.Hour); return &v }
	now := day(25).Add(9 * time.Hour)
	cases := []struct {
		name              string
		createdAt         time.Time
		startOn, released *time.Time
		wantFrom, wantTo  time.Time
	}{
		{"an unreleased version runs from its start to today", day(1).Add(8 * time.Hour), at(3, 0), nil, day(3), day(25)},
		{"without a start it runs from when it was made", day(2).Add(17 * time.Hour), nil, nil, day(2), day(25)},
		{"a released version stops on its release day", day(1), at(1, 0), at(20, 15), day(1), day(20)},
		{"a start after today is today", day(1), at(28, 0), nil, day(25), day(25)},
		{"released before it started, the series is the release day", day(1), at(10, 0), at(8, 11), day(8), day(8)},
	}
	for _, c := range cases {
		from, to := burndownSpan(c.createdAt, c.startOn, c.released, now)
		if !from.Equal(c.wantFrom) || !to.Equal(c.wantTo) {
			t.Errorf("%s: span %s to %s, want %s to %s", c.name, from.Format(time.DateOnly), to.Format(time.DateOnly), c.wantFrom.Format(time.DateOnly), c.wantTo.Format(time.DateOnly))
		}
	}
}
