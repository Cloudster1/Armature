//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/demo"
)

// The suite demo is the three tools' GitHub issues under their own numbers, built by the worker,
// added once. Its keys are an installation's to have once, so the test gets one of its own.
func TestTheSuiteDemoHoldsTheToolsIssuesUnderTheirNumbers(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api, empty := emptyInstallation(t, h, "armature_suite_demo_test")
	api.api.Auth = empty.authService().Signups(auth.SignupFirst)
	ctx := context.Background()

	owner := api.client(t)
	founded := want(t, owner.post("/api/v1/auth/signup", map[string]string{"email": h.email(t, "suite"), "password": testPassword, "name": "Suite"}), http.StatusCreated, "the first account")
	ownerID := principalField(t, founded, "principal", "user", "id").(string)
	member, _ := api.localMember(t, h, owner, "member")

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := empty.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, query, args...).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	builder := demo.NewSuiteBuilder(empty.cluster, slog.New(slog.DiscardHandler))
	snapshots, err := demo.Snapshots()
	if err != nil {
		t.Fatal(err)
	}

	for _, refused := range []response{member.get("/api/v1/installation/suite-demo"), member.post("/api/v1/installation/suite-demo", nil)} {
		if refused.Status != http.StatusForbidden {
			t.Fatalf("a member reaches the suite demo: %d %s", refused.Status, refused.Raw)
		}
	}
	if got := want(t, owner.get("/api/v1/installation/suite-demo"), http.StatusOK, "the suite demo before anybody added it"); got.Body["demo"] != nil {
		t.Fatalf("a suite demo nobody added: %s", got.Raw)
	}

	t.Run("a project under one of the keys holds the suite demo off whole", func(t *testing.T) {
		held := idOf(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "Exciter, ours", "key": "EXC", "template": "kanban"}), http.StatusCreated, "a project of the installation's own"), "project")
		refused := owner.post("/api/v1/installation/suite-demo", nil)
		fields, _ := refused.Error()["fields"].(map[string]any)
		if refused.Status != http.StatusUnprocessableEntity || !strings.Contains(fields["key"].(string), "EXC") {
			t.Fatalf("the suite demo over EXC: %d %s", refused.Status, refused.Raw)
		}
		if n := count(`SELECT count(*) FROM suite_demo`); n != 0 {
			t.Error("the refused suite demo was queued")
		}
		if _, err := empty.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `DELETE FROM project WHERE id = $1`, held)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})

	queued := obj(t, want(t, owner.post("/api/v1/installation/suite-demo", nil), http.StatusAccepted, "add the suite demo"), "demo")
	if queued["state"] != demo.SuiteQueued || queued["steps"] != float64(demo.SuiteSteps) {
		t.Fatalf("the suite demo was answered %v", queued)
	}
	if again := owner.post("/api/v1/installation/suite-demo", nil); again.Status != http.StatusConflict {
		t.Fatalf("the suite demo a second time: %d %s", again.Status, again.Raw)
	}

	built, err := builder.Once(ctx)
	if err != nil || built != 1 {
		t.Fatalf("the worker built %d suite demos: %v", built, err)
	}
	done := obj(t, want(t, owner.get("/api/v1/installation/suite-demo"), http.StatusOK, "the built suite demo"), "demo")
	if done["state"] != demo.SuiteDone || done["step"] != done["steps"] {
		t.Fatalf("the suite demo stood at %v", done)
	}
	if again, err := builder.Once(ctx); err != nil || again != 0 {
		t.Errorf("the worker built a done suite demo again: %d %v", again, err)
	}

	for _, s := range snapshots {
		closed := 0
		for _, gh := range s.Issues {
			if gh.Closed {
				closed++
			}
		}
		if n := count(`SELECT count(*) FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $1`, s.Key); n != len(s.Issues) {
			t.Errorf("%s holds %d issues, its snapshot %d", s.Key, n, len(s.Issues))
		}
		if n := count(`
			SELECT count(*) FROM issue i JOIN project p ON p.id = i.project_id JOIN issue_status st ON st.id = i.status_id
			WHERE p.key = $1 AND st.category = 'done' AND i.resolved_at IS NOT NULL`, s.Key); n != closed {
			t.Errorf("%s has %d issues done, its snapshot %d closed", s.Key, n, closed)
		}
		for _, gh := range s.Issues {
			if n := count(`SELECT count(*) FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $1 AND i.key_num = $2 AND i.external_key = $3`, s.Key, gh.Number, gh.URL); n != 1 {
				t.Errorf("%s#%d is not %s-%d", s.Repo, gh.Number, s.Key, gh.Number)
				break
			}
		}
	}

	// Exciter's suite demo links its requirements to these keys.
	risk := obj(t, want(t, member.get("/api/v1/issues/EXC-261"), http.StatusOK, "Exciter's issue #261"), "issue")
	if !strings.Contains(strings.ToLower(risk["summary"].(string)), "risk") {
		t.Errorf("EXC-261 is %q", risk["summary"])
	}
	if n := count(`
		SELECT count(*) FROM issue_link l
		JOIN issue s ON s.id = l.source_id JOIN project sp ON sp.id = s.project_id
		JOIN issue d ON d.id = l.target_id JOIN project dp ON dp.id = d.project_id
		WHERE sp.key <> dp.key AND sp.key IN ('ARM', 'EXC', 'STA') AND dp.key IN ('ARM', 'EXC', 'STA')`); n == 0 {
		t.Error("no blocking link crosses the suite demo's projects")
	}
	if n := count(`SELECT count(*) FROM audit_log WHERE action = 'installation.suite_demo_added' AND actor_user_id = $1`, ownerID); n != 1 {
		t.Errorf("adding the suite demo was recorded %d times", n)
	}
}
