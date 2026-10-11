package issue

import (
	"errors"
	"time"
)

// ErrStale refuses an edit made to an older copy of the issue, so the later of
// two concurrent saves cannot erase the earlier one without anybody noticing.
var ErrStale = errors.New("somebody changed this issue since you opened it")

// CheckFresh compares the updatedAt an edit started from with the issue's own.
// A nil start is a client that did not say, which is let through as before.
func CheckFresh(current time.Time, startedFrom *time.Time) error {
	if startedFrom == nil || startedFrom.Equal(current) {
		return nil
	}
	return ErrStale
}
