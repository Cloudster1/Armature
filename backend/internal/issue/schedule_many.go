package issue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
)

// MaxScheduleMoves bounds one set of moves, as bulk edit bounds its selection,
// so a request stays inside the route's timeout.
const MaxScheduleMoves = 200

var (
	// ErrMovedSince is returned when an issue's dates are no longer the ones a
	// set of moves was worked out from.
	ErrMovedSince = errors.New("an issue was moved since its dates were read")
	// ErrNotInProject is returned for a move of an issue the project does not hold.
	ErrNotInProject = errors.New("an issue is not in this project")
	// ErrMovedTwice is returned for a set of moves naming one issue twice.
	ErrMovedTwice = errors.New("an issue is moved twice")
)

// MovedSinceError is ErrMovedSince for one issue, named so a client can say in
// its own words what to do about it.
type MovedSinceError struct{ Key string }

func (e MovedSinceError) Error() string {
	return fmt.Sprintf("%s was moved since these dates were read, so nothing was saved.", e.Key)
}

func (e MovedSinceError) Unwrap() error { return ErrMovedSince }

// ScheduleMove is one issue's new range, with the range it is expected to have
// now. Either end of either range may be nil, an unset day.
type ScheduleMove struct {
	Key                        string
	ExpectedStart, ExpectedDue *time.Time
	Start, Due                 *time.Time
}

// ScheduleMany moves several of a project's issues in one transaction: all of
// them, or none when any is not where the moves expect it.
func (s *Service) ScheduleMany(ctx context.Context, projectKey string, moves []ScheduleMove, actor Actor) ([]Issue, db.LSN, error) {
	projectKey = project.NormalizeKey(projectKey)
	nums := make([]int64, 0, len(moves))
	byNum := make(map[int64]ScheduleMove, len(moves))
	for _, m := range moves {
		key, num, err := ParseKey(m.Key)
		if err != nil {
			return nil, 0, levelError{ErrInvalidKey, fmt.Sprintf("%q is not an issue key, so nothing was saved. Give keys such as %s-12.", m.Key, projectKey)}
		}
		if key != projectKey {
			return nil, 0, outsideProject(m.Key, projectKey)
		}
		if _, twice := byNum[num]; twice {
			return nil, 0, levelError{ErrMovedTwice, fmt.Sprintf("%s is moved twice, so nothing was saved. Give each issue once.", m.Key)}
		}
		byNum[num] = m
		nums = append(nums, num)
	}

	out := []Issue{}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Locked in key order, so two sets of moves over the same issues cannot deadlock.
		locked, err := readIssues(ctx, tx, selectIssue+`
			WHERE p.key = $1 AND i.key_num = ANY($2) ORDER BY i.key_num FOR UPDATE OF i`, projectKey, nums)
		if err != nil {
			return err
		}
		found := make(map[int64]bool, len(locked))
		for _, before := range locked {
			found[keyNum(before.Key)] = true
		}
		for _, num := range nums {
			if !found[num] {
				return outsideProject(byNum[num].Key, projectKey)
			}
		}
		for i := range locked {
			before := &locked[i]
			m := byNum[keyNum(before.Key)]
			if !sameDay(before.StartDate, m.ExpectedStart) || !sameDay(before.DueDate, m.ExpectedDue) {
				return MovedSinceError{Key: before.Key}
			}
			if err := CheckDates(m.Start, m.Due); err != nil {
				return levelError{errors.Unwrap(err), fmt.Sprintf("%s cannot move: %s", before.Key, err.Error())}
			}
		}
		for i := range locked {
			m := byNum[keyNum(locked[i].Key)]
			updated, err := s.writeSchedule(ctx, tx, &locked[i], m.Start, m.Due, actor)
			if err != nil {
				return err
			}
			out = append(out, *updated)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

func outsideProject(key, projectKey string) error {
	return levelError{ErrNotInProject, fmt.Sprintf("%s is not an issue of %s, so nothing was saved. Move only the project's own issues.", key, projectKey)}
}

// keyNum is the number of a key that has already been parsed once.
func keyNum(key string) int64 {
	_, num, _ := ParseKey(key)
	return num
}
