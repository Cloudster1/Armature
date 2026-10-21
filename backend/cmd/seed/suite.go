package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/demo"
	"github.com/armature/armature/backend/internal/issue"
)

// suitePeople report and work the suite demo's issues, the realm's alice and bob among them,
// so they find work of their own when they come through single sign-on.
var suitePeople = []string{
	"ada@armature.test", "grace@armature.test", "alan@armature.test", "rita@armature.test",
	"alice@suite.test", "bob@suite.test",
}

// seedSuite adds the suite demo, the three tools' GitHub issues, or brings it up to the snapshot.
func seedSuite(ctx context.Context, cluster *db.Cluster, creds *auth.Credentials, log *slog.Logger) error {
	members, err := membersByEmail(ctx, cluster)
	if err != nil {
		return err
	}
	people := make([]uuid.UUID, 0, len(suitePeople))
	for _, email := range suitePeople {
		id, ok := members[email]
		if !ok {
			return fmt.Errorf("the suite demo is worked by %s, who is no member; seed an empty installation, after make clean and make up", email)
		}
		people = append(people, id)
	}
	actor := issue.Actor{UserID: creds.Principal.User.ID, OrgRole: creds.Principal.Role}
	if err := demo.SeedSuite(ctx, cluster, actor, people, log); err != nil {
		return fmt.Errorf("add the suite demo: %w", err)
	}
	return nil
}
