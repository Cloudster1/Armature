//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// anotherProject adds a project to the workspace to move issues into.
func (ws *workspace) anotherProject(t *testing.T, name string) *project.Project {
	t.Helper()
	p, _, err := ws.projects.Create(ws.ctx, project.CreateInput{
		Name: name, Key: strings.ToUpper(name[:min(len(name), 3)] + uuid.New().String()[:3]),
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create project %s: %v", name, err)
	}
	return p
}

// Every key an issue has had finds it: two moves leave two old keys, and both,
// like each key its subtask had, still open what the issue is now.
func TestEveryKeyAnIssueHadStillFindsIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "rekeyed")
	second := ws.anotherProject(t, "Second")
	third := ws.anotherProject(t, "Third")

	story := ws.newIssue(t, "Moved twice")
	step, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key, Summary: "A step of it",
		TypeID: h.issueTypeID(t, ws, bootstrap.TypeSubtask), ParentKey: story.Key,
	}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}

	once, _, err := ws.issues.Move(ws.ctx, story.Key, issue.MoveInput{ProjectKey: second.Key}, ws.actor)
	if err != nil {
		t.Fatalf("first move: %v", err)
	}
	stepOnce, err := ws.issues.ByKey(ws.ctx, step.Key)
	if err != nil || stepOnce.ProjectKey != second.Key {
		t.Fatalf("the subtask after the first move: %+v, %v", stepOnce, err)
	}
	twice, _, err := ws.issues.Move(ws.ctx, once.Key, issue.MoveInput{ProjectKey: third.Key}, ws.actor)
	if err != nil {
		t.Fatalf("second move: %v", err)
	}
	stepTwice, err := ws.issues.ByKey(ws.ctx, stepOnce.Key)
	if err != nil || stepTwice.ProjectKey != third.Key {
		t.Fatalf("the subtask after the second move: %+v, %v", stepTwice, err)
	}

	for _, c := range []struct {
		old  string
		want *issue.Issue
		now  string
	}{
		{story.Key, story, twice.Key},
		{once.Key, story, twice.Key},
		{step.Key, step, stepTwice.Key},
		{stepOnce.Key, step, stepTwice.Key},
		// However it is spelled, as a typed address would be.
		{strings.ToLower(story.Key), story, twice.Key},
	} {
		found, err := ws.issues.ByKey(ws.ctx, c.old)
		if err != nil {
			t.Errorf("%s no longer finds the issue: %v", c.old, err)
			continue
		}
		if found.ID != c.want.ID || found.Key != c.now {
			t.Errorf("%s found %s (%s), want %s", c.old, found.Key, found.ID, c.now)
		}
	}
}

// The same over the API, which is what the pages and a mailed link open.
func TestEveryKeyAnIssueHadOpensItOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "rekeyapi")
	var projects []string
	for _, name := range []string{"Start", "Middle", "End"} {
		made := want(t, c.post("/api/v1/projects", map[string]any{
			"name": name, "key": strings.ToUpper(name[:3]) + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban",
		}), http.StatusCreated, "project "+name)
		projects = append(projects, obj(t, made, "project")["key"].(string))
	}
	created := want(t, c.post("/api/v1/projects/"+projects[0]+"/issues", map[string]any{"summary": "Moved twice"}), http.StatusCreated, "issue")
	first := obj(t, created, "issue")["key"].(string)
	once := obj(t, want(t, c.post("/api/v1/issues/"+first+"/move", map[string]any{"projectKey": projects[1]}), http.StatusOK, "first move"), "issue")["key"].(string)
	twice := obj(t, want(t, c.post("/api/v1/issues/"+once+"/move", map[string]any{"projectKey": projects[2]}), http.StatusOK, "second move"), "issue")["key"].(string)

	for _, old := range []string{first, once} {
		if r := want(t, c.get("/api/v1/issues/"+old), http.StatusOK, "the old key "+old); obj(t, r, "issue")["key"] != twice {
			t.Errorf("%s answered %s, want %s", old, r.Raw, twice)
		}
	}
}

// A reply naming a key the request had before it moved still lands on it, so
// the subject of the first mail the customer got keeps working.
func TestAReplyToAKeyARequestHadBeforeItMovedReachesIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "mailmove")
	d, first := ws.aDesk(t, h, "First desk")
	_, second := ws.aDesk(t, h, "Second desk")
	customer := ws.customerOf(t, h, "mover")
	var customerEmail string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, customer.UserID).Scan(&customerEmail); err != nil {
		t.Fatal(err)
	}
	types, _ := d.RequestTypes(ws.ctx, first.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the invoice?"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	// Out to the second desk and back: the key it was raised under is now
	// neither its own nor the one it last left.
	away, _, err := ws.issues.Move(ws.ctx, raised.Key, issue.MoveInput{ProjectKey: second.Key}, ws.actor)
	if err != nil {
		t.Fatalf("move to the second desk: %v", err)
	}
	back, _, err := ws.issues.Move(ws.ctx, away.Key, issue.MoveInput{ProjectKey: first.Key}, ws.actor)
	if err != nil {
		t.Fatalf("move back: %v", err)
	}
	if back.Key == raised.Key {
		t.Fatalf("the request came back under its first key %s", back.Key)
	}

	run := uuid.NewString()[:8]
	inbox := &fakeInbox{mail: map[string]string{}}
	reader := desk.NewInbound(h.cluster, d, ws.issues, &fakeMailer{}, inbox,
		desk.InboundConfig{Address: inboxAddress, From: "Armature <no-reply@armature.test>", AppURL: "http://app.test"},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	for i, old := range []string{raised.Key, away.Key} {
		messageID := "moved-" + old + "-" + run + "@example.com"
		inbox.mail = map[string]string{"m" + old: rawMail(customerEmail, inboxAddress, "Re: ["+old+"] your request", messageID, "Any news on "+old+"?")}
		if err := reader.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		var outcome string
		if err := h.super.QueryRow(context.Background(), `SELECT outcome FROM inbound_mail WHERE message_id = $1`, messageID).Scan(&outcome); err != nil {
			t.Fatalf("no row for the reply to %s: %v", old, err)
		}
		if outcome != string(desk.Commented) {
			t.Errorf("the reply to %s was %s, want commented", old, outcome)
		}
		comments, err := ws.issues.Comments(ws.ctx, back.Key, true)
		if err != nil {
			t.Fatal(err)
		}
		var mine []issue.Comment
		for _, c := range comments {
			if c.Author != nil && c.Author.ID == customer.UserID {
				mine = append(mine, c)
			}
		}
		if len(mine) != i+1 || issue.PlainText(mine[i].Body) != "Any news on "+old+"?" {
			t.Errorf("after the reply to %s the customer has %d comments on %s: %+v", old, len(mine), back.Key, mine)
		}
	}
}

// The same reads and writes as the service, straight through SQL: a key names
// one issue in an organization, and none in another.
func TestAnOldKeyStaysInItsOrganizationEvenThroughSQL(t *testing.T) {
	h := newHarness(t)
	home := h.newWorkspace(t, "oldhome")
	away := h.newWorkspace(t, "oldaway")
	elsewhere := home.anotherProject(t, "Elsewhere")
	mine := home.newIssue(t, "moved once")
	other := home.newIssue(t, "never moved")
	theirs := away.newIssue(t, "in another organization")
	if _, _, err := home.issues.Move(home.ctx, mine.Key, issue.MoveInput{ProjectKey: elsewhere.Key}, home.actor); err != nil {
		t.Fatal(err)
	}
	old := mine.Key
	insert := func(ctx context.Context, orgID any, issueID uuid.UUID, key string) error {
		_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO issue_old_key (org_id, issue_id, key) VALUES (COALESCE($1, current_org_id()), $2, $3)`, orgID, issueID, key)
			return err
		})
		return err
	}
	rowsFor := func(t *testing.T, issueID uuid.UUID) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue_old_key WHERE issue_id = $1`, issueID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if rowsFor(t, mine.ID) != 1 {
		t.Fatalf("the moved issue has %d old keys, want 1", rowsFor(t, mine.ID))
	}

	t.Run("an old key names one issue in its organization", func(t *testing.T) {
		err := insert(home.ctx, nil, other.ID, old)
		if err == nil || !strings.Contains(err.Error(), "issue_old_key_pkey") {
			t.Errorf("a second issue took the old key %s: %v", old, err)
		}
	})

	t.Run("a key that is not one is refused", func(t *testing.T) {
		err := insert(home.ctx, nil, other.ID, "not a key")
		if err == nil || !strings.Contains(err.Error(), "issue_old_key_shape") {
			t.Errorf("a key of the wrong shape went in: %v", err)
		}
	})

	t.Run("another organization reads none of it", func(t *testing.T) {
		var seen int
		_, err := h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM issue_old_key WHERE key = $1`, old).Scan(&seen)
		})
		if err != nil {
			t.Fatal(err)
		}
		if seen != 0 {
			t.Error("another organization sees the old key")
		}
		if _, err := away.issues.ByKey(away.ctx, old); err == nil {
			t.Error("another organization opened the issue by its old key")
		}
	})

	t.Run("another organization changes none of it", func(t *testing.T) {
		var touched int64
		_, err := h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `UPDATE issue_old_key SET issue_id = $2 WHERE key = $1`, old, theirs.ID)
			if err != nil {
				return err
			}
			touched = tag.RowsAffected()
			tag, err = tx.Exec(ctx, `DELETE FROM issue_old_key WHERE key = $1`, old)
			touched += tag.RowsAffected()
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if touched != 0 || rowsFor(t, mine.ID) != 1 {
			t.Errorf("another organization changed %d rows", touched)
		}
	})

	// The trigger runs before the policy's check, and refuses first: from there
	// the other organization's issue cannot be seen at all.
	t.Run("a row planted in another organization is refused", func(t *testing.T) {
		if err := insert(away.ctx, home.orgID, mine.ID, "PLANT-1"); err == nil {
			t.Error("an old key went into another organization")
		}
		if rowsFor(t, mine.ID) != 1 {
			t.Errorf("the issue has %d old keys, want only its own", rowsFor(t, mine.ID))
		}
	})

	t.Run("a row pointing at another organization's issue is refused by the trigger", func(t *testing.T) {
		err := insert(away.ctx, nil, mine.ID, "PLANT-2")
		if err == nil || !strings.Contains(err.Error(), "same organization as its issue") {
			t.Errorf("an old key reached another organization's issue: %v", err)
		}
	})

	t.Run("the same key is another organization's own to give", func(t *testing.T) {
		if err := insert(away.ctx, nil, theirs.ID, old); err != nil {
			t.Fatalf("the other organization could not use %s: %v", old, err)
		}
		if found, err := away.issues.ByKey(away.ctx, old); err != nil || found.ID != theirs.ID {
			t.Errorf("%s there found %+v, %v; want their own issue", old, found, err)
		}
		if found, err := home.issues.ByKey(home.ctx, old); err != nil || found.ID != mine.ID {
			t.Errorf("%s here found %+v, %v; want the moved issue", old, found, err)
		}
	})

	t.Run("the old keys go with their issue", func(t *testing.T) {
		moved, err := home.issues.ByKey(home.ctx, old)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := home.issues.Delete(home.ctx, moved.Key, home.actor); err != nil {
			t.Fatal(err)
		}
		if rowsFor(t, mine.ID) != 0 {
			t.Error("an old key outlived its issue")
		}
	})
}

// The one key each issue kept before is carried into the table by the
// migration, as the owner a cluster migrates as, whose tables hide their rows.
func TestOldKeysComeOverFromTheColumnThatKeptOne(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_old_key_migrate_test", "armature_old_key_owner_test")

	const beforeOldKeys = 921
	if _, err := provider.UpTo(ctx, beforeOldKeys); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeOldKeys, err)
	}
	row := func(sql string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := super.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}
	orgID := row(`INSERT INTO org (slug, name) VALUES ('acme', 'Acme') RETURNING id`)
	typeID := row(`INSERT INTO issue_type (org_id, name) VALUES ($1, 'Task') RETURNING id`, orgID)
	statusID := row(`INSERT INTO issue_status (org_id, name, category) VALUES ($1, 'To Do', 'todo') RETURNING id`, orgID)
	projectID := row(`INSERT INTO project (org_id, key, name) VALUES ($1, 'NEW', 'New home') RETURNING id`, orgID)
	movedID := row(`INSERT INTO issue (org_id, project_id, key_num, issue_type_id, status_id, summary, rank, moved_from)
		VALUES ($1, $2, 1, $3, $4, 'Moved before the table', '0V', 'OLD-7') RETURNING id`, orgID, projectID, typeID, statusID)
	row(`INSERT INTO issue (org_id, project_id, key_num, issue_type_id, status_id, summary, rank)
		VALUES ($1, $2, 2, $3, $4, 'Never moved', '0W') RETURNING id`, orgID, projectID, typeID, statusID)

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate the rest as the owner: %v", err)
	}
	var (
		rows       int
		keptOrg    uuid.UUID
		keptIssue  uuid.UUID
		leftColumn int
	)
	if err := super.QueryRow(ctx, `SELECT count(*) FROM issue_old_key`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("the table holds %d old keys, want the one the column kept", rows)
	}
	if err := super.QueryRow(ctx, `SELECT org_id, issue_id FROM issue_old_key WHERE key = 'OLD-7'`).Scan(&keptOrg, &keptIssue); err != nil {
		t.Fatalf("OLD-7 did not come over: %v", err)
	}
	if keptOrg != orgID || keptIssue != movedID {
		t.Errorf("OLD-7 names %s in %s, want %s in %s", keptIssue, keptOrg, movedID, orgID)
	}
	// One place holds the old keys, so a column left behind cannot disagree.
	if err := super.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'issue' AND column_name = 'moved_from'`).Scan(&leftColumn); err != nil {
		t.Fatal(err)
	}
	if leftColumn != 0 {
		t.Error("issue still has a moved_from column beside the table")
	}
}
