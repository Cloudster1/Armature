package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// JoinRequest is somebody the identity provider vouched for who is waiting
// to be let in.
type JoinRequest struct {
	UserID      uuid.UUID `json:"userId"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	RequestedAt time.Time `json:"requestedAt"`
}

// ErrNoSuchRequest is returned when the person named is not waiting here.
var ErrNoSuchRequest = errors.New("nobody by that name is waiting to be let in")

// JoinRequests lists who is waiting to be let in, oldest first.
func (s *Service) JoinRequests(ctx context.Context, orgID uuid.UUID) ([]JoinRequest, error) {
	out := []JoinRequest{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.email::text, u.name, r.created_at
			FROM org_join_request r JOIN app_user u ON u.id = r.user_id
			WHERE r.org_id = $1 AND u.erased_at IS NULL
			ORDER BY r.created_at, u.email`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r JoinRequest
			if err := rows.Scan(&r.UserID, &r.Email, &r.Name, &r.RequestedAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// AdmitJoinRequest lets a waiting person in with the standing given. Their
// next sign-in through the provider then succeeds.
func (s *Service) AdmitJoinRequest(ctx context.Context, orgID, userID uuid.UUID, role OrgRole, actor uuid.UUID, ip string) (*ManagedUser, db.LSN, error) {
	if role != RoleAdmin && role != RoleMember {
		return nil, 0, ErrBadUserRole
	}
	var made ManagedUser
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM org_join_request WHERE org_id = $1 AND user_id = $2`, orgID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchRequest
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role)
			VALUES ($1, $2, $3)
			ON CONFLICT (org_id, user_id) DO UPDATE SET org_role = EXCLUDED.org_role`,
			orgID, userID, string(role)); err != nil {
			return fmt.Errorf("create membership: %w", err)
		}
		if err := grantJoiningRole(ctx, tx, orgID, userID, role); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, orgID, actor, "member.admitted", "user", &userID, ip); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, orgID, events.TopicMemberJoined, map[string]any{
			"orgId":  orgID,
			"userId": userID,
			"role":   role,
		}); err != nil {
			return err
		}
		r, err := readManaged(ctx, tx, orgID, userID, RoleOwner)
		if err != nil {
			return err
		}
		made = r.user
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &made, lsn, nil
}

// DeclineJoinRequest forgets the request. The person may sign in again and
// ask once more; nothing about them is kept.
func (s *Service) DeclineJoinRequest(ctx context.Context, orgID, userID uuid.UUID, actor uuid.UUID, ip string) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM org_join_request WHERE org_id = $1 AND user_id = $2`, orgID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchRequest
		}
		return writeAudit(ctx, tx, orgID, actor, "member.declined", "user", &userID, ip)
	})
}
