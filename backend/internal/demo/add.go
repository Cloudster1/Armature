package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
)

// KeyTakenError is a demo held off by a project already under one of its keys:
// an earlier demo, or the installation's own work.
type KeyTakenError struct {
	Key string
}

func (e *KeyTakenError) Error() string {
	return fmt.Sprintf("the project key %s is taken already", e.Key)
}

// Keys are the demo's projects, which it needs free before it starts.
var Keys = []string{FactoryKey, MaintenanceKey}

// Add fills the installation with the demo for an administrator and records
// it. The keys are checked first, so a refusal leaves nothing half made.
func Add(ctx context.Context, cluster *db.Cluster, p *auth.Principal, ip string, log *slog.Logger) (*project.Project, db.LSN, error) {
	ctx = db.PinPrimary(ctx)
	projects := project.NewService(cluster)
	for _, key := range Keys {
		free, err := projects.KeyAvailable(ctx, key)
		if err != nil {
			return nil, 0, err
		}
		if !free {
			return nil, 0, &KeyTakenError{Key: key}
		}
	}
	made, err := Factory(ctx, cluster, p, log)
	if errors.Is(err, project.ErrKeyTaken) {
		// Somebody else took it between the check and the demo.
		return nil, 0, &KeyTakenError{Key: FactoryKey}
	}
	if err != nil {
		return nil, 0, err
	}
	lsn, err := cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return audit.Write(ctx, tx, audit.Entry{
			Action: "installation.demo_added", TargetType: "project", TargetID: &made.ID, Actor: p.User.ID, IP: ip,
			Data: map[string]any{"projects": Keys},
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return made, lsn, nil
}
