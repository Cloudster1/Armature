package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// accessRequestPath is the page where an administrator reads a request and
// grants it.
const accessRequestPath = "/access-requests/"

// administrators are who may administer the project through a role of their
// own or a group's, for this project alone ($3) or for all projects; never the asker.
const administrators = `
SELECT DISTINCT u.id FROM app_user u
WHERE u.is_active AND u.standing <> 'customer' AND u.id <> $2 AND EXISTS (
    SELECT 1 FROM role_assignment a
    JOIN role r ON r.key = a.role AND 'project.administer' = ANY (r.permissions)
    LEFT JOIN group_member m ON m.group_id = a.group_id AND m.user_id = u.id
    WHERE (a.user_id = u.id OR m.user_id IS NOT NULL)
      AND (CASE WHEN $3 THEN a.project_id = $1 ELSE a.project_id IS NULL END))`

// owners hear a request when nobody holds a role that administers the
// project, so an ask never goes unheard.
const owners = `SELECT id FROM app_user WHERE is_active AND standing = 'owner' AND id <> $1`

// handleAccess tells a project's administrators that somebody asked to see one
// of its issues, with the note they left and a link to grant it.
func (f *FanOut) handleAccess(ctx context.Context, e events.Event) error {
	var asked struct {
		RequestID uuid.UUID `json:"requestId"`
	}
	if err := json.Unmarshal(e.Payload, &asked); err != nil || asked.RequestID == uuid.Nil {
		return nil
	}
	ctx = db.PinPrimary(ctx)
	var (
		projectID, requester uuid.UUID
		who, key, message    string
		recipients           []uuid.UUID
	)
	err := f.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			SELECT r.project_id, r.requester_id, u.name, r.issue_key, r.message
			FROM access_request r JOIN app_user u ON u.id = r.requester_id
			WHERE r.id = $1`, asked.RequestID).Scan(&projectID, &requester, &who, &key, &message)
		if err != nil {
			return err
		}
		// The project's own administrators first; those of all projects only
		// when it has none, and the owners when nobody else administers it.
		for _, ownOnly := range []bool{true, false} {
			if recipients, err = ids(ctx, tx, administrators, projectID, requester, ownOnly); err != nil || len(recipients) > 0 {
				return err
			}
		}
		recipients, err = ids(ctx, tx, owners, requester)
		return err
	})
	if err != nil {
		// A request removed with its project or its asker leaves nobody to tell.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	at := place{link: f.appURL + accessRequestPath + asked.RequestID.String()}
	for _, user := range recipients {
		if err := f.deliver(ctx, e.ID, at, accessTell(user, who, key, message)); err != nil {
			return err
		}
	}
	return nil
}

// accessTell words a request for one administrator.
func accessTell(user uuid.UUID, who, key, message string) tell {
	body := "They left no message."
	if message != "" {
		body = fmt.Sprintf("Their message: %s", message)
	}
	return tell{user: user, kind: KindAccessRequested, title: fmt.Sprintf("%s asks for access to %s", who, key), body: body}
}

func ids(ctx context.Context, tx db.DBTX, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
