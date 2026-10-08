package issue

import (
	"errors"
	"fmt"
	"time"
)

// The years an issue can be scheduled in. A plan reads every day between an
// issue's ends, so 2206 typed for 2026 must not become two centuries of work.
var (
	EarliestDate = time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
	LatestDate   = time.Date(2199, time.December, 31, 0, 0, 0, 0, time.UTC)
)

// ErrDateOutOfRange is returned for a start or due day outside those years.
var ErrDateOutOfRange = errors.New("an issue is scheduled between 1900 and 2199")

// CheckDates says whether two ends of a schedule can be written: each inside
// the years above, and the due day not before the start.
func CheckDates(start, due *time.Time) error {
	for _, d := range []*time.Time{start, due} {
		if d != nil && (d.Before(EarliestDate) || d.After(LatestDate)) {
			return fmt.Errorf("%w: %s is outside them, so pick a day within those years.", ErrDateOutOfRange, formatDate(d))
		}
	}
	if start != nil && due != nil && due.Before(*start) {
		return fmt.Errorf("%w: %s ends before %s", ErrBackwardsRange, formatDate(due), formatDate(start))
	}
	return nil
}
