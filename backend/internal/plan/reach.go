package plan

import "time"

// The load, and the availability under it, is read over at most LoadSpanDays
// of the window, starting no earlier than LoadBackDays before today. One due
// date typed a century out must not make every plan read a century of
// everybody's days, while work planned a few years ahead keeps its load.
const (
	LoadBackDays = 2 * 366
	LoadSpanDays = 10 * 366
)

// LoadSpan is the part of a window the load is read over. The window always
// holds today, so the span is never empty.
func LoadSpan(from, to, today time.Time) (time.Time, time.Time) {
	from = maxTime(from, midnight(today).AddDate(0, 0, -LoadBackDays))
	return from, minTime(to, from.AddDate(0, 0, LoadSpanDays))
}
