package desk

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// QueueFilter is which of a desk's requests an agent wants to see.
type QueueFilter string

const (
	QueueOpen       QueueFilter = "open"
	QueueUnassigned QueueFilter = "unassigned"
	QueueMine       QueueFilter = "mine"
	QueueBreached   QueueFilter = "breached"
	QueueAll        QueueFilter = "all"
)

// QueuePageSize is a page of a queue unless asked otherwise; MaxQueuePage is
// the most one read returns, as for any other list of issues.
const (
	QueuePageSize = 50
	MaxQueuePage  = 200
)

// QueuePage is one page of a queue, and how many requests the whole holds.
type QueuePage struct {
	Rows   []QueueRow `json:"rows"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// queueRank is most pressing first; the key settles ties so pages never overlap.
const queueRank = `breached DESC, left_seconds ASC NULLS LAST, created_at DESC, key_num DESC`

// queueOrder ranks the whole queue in the database and cuts the page from it.
// A clock on business hours counts as breached once the watch records it.
const queueOrder = `
WITH q AS (
	SELECT i.id, p.key || '-' || i.key_num AS key, i.created_at, i.key_num,
	       COALESCE(u.breached, false) AS breached, u.left_seconds
	FROM issue i
	JOIN project p ON p.id = i.project_id
	JOIN issue_status s ON s.id = i.status_id
	LEFT JOIN LATERAL (
		SELECT bool_or(c.breached_at IS NOT NULL OR (c.left_seconds < 0 AND NOT c.by_calendar)) AS breached,
		       min(c.left_seconds) AS left_seconds
		FROM (
			SELECT t.breached_at, sp.use_calendar AND bc.project_id IS NOT NULL AS by_calendar,
			       t.goal_minutes * 60 - t.elapsed_seconds
			         - COALESCE(EXTRACT(EPOCH FROM $6::timestamptz - t.running_since), 0) AS left_seconds
			FROM sla_timer t
			JOIN sla_policy sp ON sp.id = t.policy_id
			LEFT JOIN business_calendar bc ON bc.project_id = sp.project_id
			WHERE t.issue_id = i.id AND t.completed_at IS NULL
		) c
	) u ON true
	WHERE p.key = $1
	  AND (cardinality($2::status_category[]) = 0 OR s.category = ANY($2::status_category[]))
	  AND (NOT $3 OR i.assignee_id IS NULL)
	  AND ($4::uuid IS NULL OR i.assignee_id = $4)
	  AND (NOT $5 OR COALESCE(u.breached, false))
)
SELECT (SELECT count(*) FROM q), pg.id, pg.key
FROM (SELECT 1) one
LEFT JOIN LATERAL (
	SELECT * FROM q ORDER BY ` + queueRank + ` LIMIT $7 OFFSET $8
) pg ON true
ORDER BY ` + queueRank

// Queue reads one page of a desk's requests, breached first, then least time
// left. A business hours clock's time left is read by the wall, never later.
func (s *Service) Queue(ctx context.Context, projectKey string, filter QueueFilter, agent uuid.UUID, page issue.Page) (*QueuePage, error) {
	var (
		categories = []string{string(workflow.CategoryTodo), string(workflow.CategoryInProgress)}
		unassigned bool
		assignee   *uuid.UUID
	)
	switch filter {
	case QueueOpen, QueueBreached, "":
	case QueueUnassigned:
		unassigned = true
	case QueueMine:
		assignee = &agent
	case QueueAll:
		categories = []string{}
	default:
		return nil, fmt.Errorf("%q is not a queue", filter)
	}
	if page.Limit <= 0 || page.Limit > MaxQueuePage {
		page.Limit = QueuePageSize
	}
	page.Offset = max(page.Offset, 0)
	key := project.NormalizeKey(projectKey)
	out := &QueuePage{Rows: []QueueRow{}, Limit: page.Limit, Offset: page.Offset}

	var (
		ids  []uuid.UUID
		keys []string
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, queueOrder, key, categories, unassigned, assignee, filter == QueueBreached, s.now().UTC(), page.Limit, page.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id     *uuid.UUID
				issKey *string
			)
			if err := rows.Scan(&out.Total, &id, &issKey); err != nil {
				return err
			}
			// Past the end the count still comes, on a row with no request.
			if id != nil {
				ids, keys = append(ids, *id), append(keys, *issKey)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("order the queue: %w", err)
	}
	if len(ids) == 0 {
		return out, nil
	}

	found, err := s.issues.List(ctx, issue.Filter{ProjectKey: key, Keys: keys}, issue.Page{Limit: len(keys)})
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]issue.Issue{}
	for _, i := range found.Issues {
		byID[i.ID] = i
	}
	timers, rated, err := s.clocksOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		i, ok := byID[id]
		if !ok {
			continue
		}
		row := QueueRow{Issue: i, Timers: timers[id], RequestTypeName: i.RequestTypeName}
		if row.Timers == nil {
			row.Timers = []Timer{}
		}
		if score, ok := rated[id]; ok {
			row.Csat = &score
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// clocksOf reads the clocks and ratings of a page of requests.
func (s *Service) clocksOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]Timer, map[uuid.UUID]int, error) {
	byIssue := map[uuid.UUID][]Timer{}
	rated := map[uuid.UUID]int{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectTimer+` WHERE t.issue_id = ANY($1) ORDER BY sp.metric`, ids)
		if err != nil {
			return err
		}
		timers, err := s.scanTimers(rows)
		if err != nil {
			return err
		}
		for _, t := range timers {
			byIssue[t.IssueID] = append(byIssue[t.IssueID], t)
		}
		scores, err := tx.Query(ctx, `SELECT issue_id, score FROM csat_rating WHERE issue_id = ANY($1) AND score IS NOT NULL`, ids)
		if err != nil {
			return err
		}
		defer scores.Close()
		for scores.Next() {
			var (
				id    uuid.UUID
				score int
			)
			if err := scores.Scan(&id, &score); err != nil {
				return err
			}
			rated[id] = score
		}
		return scores.Err()
	})
	return byIssue, rated, err
}
