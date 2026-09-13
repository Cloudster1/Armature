package report

import (
	"context"
	"fmt"

	"github.com/armature/armature/backend/internal/db"
)

// CSATReport is what customers said of resolved requests in the window.
type CSATReport struct {
	Invited int `json:"invited"`
	Rated   int `json:"rated"`
	// Average is the mean score, 1 to 5; zero when nobody answered.
	Average float64 `json:"average"`
	// Scores counts each score from 1 to 5, in that order.
	Scores []int `json:"scores"`
	// ResponseRate is rated over invited, 0 to 1.
	ResponseRate float64 `json:"responseRate"`
	Window       int     `json:"window"`
}

// csat sums the ratings of requests resolved in the window.
func csat(ctx context.Context, tx db.DBTX, sc scope) (*CSATReport, error) {
	where, args := sc.clause("i", sc.days)
	rows, err := tx.Query(ctx, `
		SELECT r.score FROM csat_rating r JOIN issue i ON i.id = r.issue_id
		WHERE `+where+` AND r.sent_at >= now() - make_interval(days => $3)`, args...)
	if err != nil {
		return nil, fmt.Errorf("csat: %w", err)
	}
	defer rows.Close()
	out := &CSATReport{Scores: make([]int, 5), Window: sc.days}
	sum := 0
	for rows.Next() {
		var score *int
		if err := rows.Scan(&score); err != nil {
			return nil, err
		}
		out.Invited++
		if score == nil {
			continue
		}
		out.Rated++
		sum += *score
		if *score >= 1 && *score <= 5 {
			out.Scores[*score-1]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out.Rated > 0 {
		out.Average = float64(sum) / float64(out.Rated)
	}
	if out.Invited > 0 {
		out.ResponseRate = float64(out.Rated) / float64(out.Invited)
	}
	return out, nil
}
