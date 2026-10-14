package git

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

// carryOutOnce skips a command this repository already did on the issue, as a
// squash commit asks; recording first keeps two racing deliveries to one.
func (s *Service) carryOutOnce(ctx context.Context, receipt *Receipt, repo *Repository, key string, cmd Command, actor issue.Actor, via string) {
	first, err := s.recordCommand(ctx, repo.ID, key, cmd)
	if err != nil {
		receipt.Refused = append(receipt.Refused, fmt.Sprintf("%s: %v", key, err))
		return
	}
	if !first {
		return
	}
	refused := len(receipt.Refused)
	s.carryOut(ctx, receipt, key, cmd, actor, via)
	if len(receipt.Refused) > refused {
		// A command that was refused was not carried out; a later commit asking
		// again, once the issue has moved on, is a fresh request.
		if err := s.forgetCommand(ctx, repo.ID, key, cmd); err != nil {
			receipt.Refused = append(receipt.Refused, fmt.Sprintf("%s: %v", key, err))
		}
	}
}

func (s *Service) forgetCommand(ctx context.Context, repoID uuid.UUID, key string, cmd Command) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		issueID, err := s.issueIDFor(ctx, tx, key)
		if err != nil || issueID == nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM git_smart_command WHERE repository_id = $1 AND issue_id = $2 AND command_hash = $3`,
			repoID, *issueID, cmd.Identity()); err != nil {
			return fmt.Errorf("forget the refused command: %w", err)
		}
		return nil
	})
	return err
}

// recordCommand notes a command against the issue its key names and says
// whether it is new. A key naming no issue here is left to carryOut to refuse.
func (s *Service) recordCommand(ctx context.Context, repoID uuid.UUID, key string, cmd Command) (bool, error) {
	first := true
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		issueID, err := s.issueIDFor(ctx, tx, key)
		if err != nil || issueID == nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO git_smart_command (repository_id, issue_id, command_hash)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, repoID, *issueID, cmd.Identity())
		if err != nil {
			return fmt.Errorf("record the command: %w", err)
		}
		first = tag.RowsAffected() == 1
		return nil
	})
	return first, err
}
