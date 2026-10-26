package availability

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
)

// NextYearProposal is a calendar's latest year carried a year on, for an
// administrator to correct and save; nothing is written until they do.
type NextYearProposal struct {
	From int `json:"from"`
	To   int `json:"to"`
	// Days are the new days, none of them one the next year already holds.
	Days []Holiday `json:"days"`
	// Skipped are the days with no date to land on next year: 29 February.
	Skipped []Holiday `json:"skipped"`
}

// LatestYear is the last year a calendar has days in.
func LatestYear(days []Holiday) (int, bool) {
	latest, found := 0, false
	for _, h := range days {
		day, err := time.Parse(DateLayout, h.Day)
		if err != nil {
			continue
		}
		if !found || day.Year() > latest {
			latest, found = day.Year(), true
		}
	}
	return latest, found
}

// NextYear moves one year's days to the same dates a year on. A date already
// held keeps what it has; 29 February is skipped, not guessed onto a neighbour.
func NextYear(days []Holiday, from int) NextYearProposal {
	out := NextYearProposal{From: from, To: from + 1, Days: []Holiday{}, Skipped: []Holiday{}}
	held := map[string]bool{}
	for _, h := range days {
		held[h.Day] = true
	}
	for _, h := range days {
		day, err := time.Parse(DateLayout, h.Day)
		if err != nil || day.Year() != from {
			continue
		}
		next := time.Date(out.To, day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
		if next.Month() != day.Month() {
			out.Skipped = append(out.Skipped, h)
			continue
		}
		key := next.Format(DateLayout)
		if held[key] {
			continue
		}
		out.Days = append(out.Days, Holiday{Day: key, Name: h.Name, HalfDay: h.HalfDay})
	}
	return out
}

// ProposeNextYear carries a calendar's latest year into the year after it.
func (s *Service) ProposeNextYear(ctx context.Context, id uuid.UUID) (*NextYearProposal, error) {
	var out *NextYearProposal
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, err := reload(ctx, tx, id)
		if err != nil {
			return err
		}
		from, found := LatestYear(c.Days)
		if !found {
			return invalid("%s has no days to copy yet; add or import this year's days first", c.Name)
		}
		proposal := NextYear(c.Days, from)
		out = &proposal
		return nil
	})
	return out, err
}
