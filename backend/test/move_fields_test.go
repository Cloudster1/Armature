//go:build integration

package test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// changeOf finds the change to one field in a history entry.
func changeOf(entry issue.HistoryEntry, name string) *issue.Change {
	for i := range entry.Changes {
		if entry.Changes[i].Field == name {
			return &entry.Changes[i]
		}
	}
	return nil
}

// A field on every project applies wherever the issue goes, so a move keeps
// its value; only the old project's own fields are dropped, each named in the history.
func TestAMoveKeepsFieldsOnEveryProjectAndNamesTheValuesItDrops(t *testing.T) {
	h := newHarness(t)
	mvfKey := h.projectKey(t, "MVF")
	ws := h.newProject(t, "movefields")
	fields := ws.fields(h)
	costName, platformName, customerName := h.name(t, "Cost"), h.name(t, "Platform"), h.name(t, "Customer")

	cost, _, err := fields.CreateOrg(ws.ctx, field.Input{Name: costName, Kind: field.Number})
	if err != nil {
		t.Fatal(err)
	}
	platform, _, err := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: platformName, Kind: field.Select, Options: []string{"Web", "Mobile"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: customerName, Kind: field.Text}); err != nil {
		t.Fatal(err)
	}

	parent := ws.newIssue(t, "Priced work")
	child, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key, Summary: "Priced part", TypeID: h.issueTypeID(t, ws, bootstrap.TypeSubtask), ParentKey: parent.Key,
	}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range []struct {
		key   string
		field uuid.UUID
		value string
	}{
		{parent.Key, cost.ID, `120`},
		{parent.Key, platform.ID, `"Web"`},
		{child.Key, cost.ID, `30`},
		{child.Key, platform.ID, `"Mobile"`},
	} {
		if _, _, err := fields.Set(ws.ctx, set.key, set.field, json.RawMessage(set.value), ws.actor); err != nil {
			t.Fatalf("set %s on %s: %v", set.value, set.key, err)
		}
	}

	target, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Target", Key: mvfKey}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	moved, _, err := ws.issues.Move(ws.ctx, parent.Key, issue.MoveInput{ProjectKey: target.Key}, ws.actor)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	kids, err := ws.issues.Children(ws.ctx, moved.Key)
	if err != nil || len(kids) != 1 {
		t.Fatalf("children of the moved issue = %+v, %v; want the subtask", kids, err)
	}
	movedChild := kids[0]

	t.Run("the field on every project keeps its value", func(t *testing.T) {
		for key, want := range map[string]string{moved.Key: "120", movedChild.Key: "30"} {
			values, err := fields.Values(ws.ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			if v := valueOf(values, costName); v == nil || v.Display != want {
				t.Errorf("%s: Cost = %+v, want %s kept through the move", key, v, want)
			}
			if v := valueOf(values, platformName); v != nil {
				t.Errorf("%s still lists the old project's field: %+v", key, v)
			}
		}
	})

	t.Run("the database holds no value of the old project's fields", func(t *testing.T) {
		var orgValues, projectValues int
		err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE f.project_id IS NULL), count(*) FILTER (WHERE f.project_id IS NOT NULL)
			FROM issue_field_value v JOIN custom_field f ON f.id = v.field_id
			WHERE v.issue_id = ANY($1)`, []uuid.UUID{parent.ID, child.ID}).Scan(&orgValues, &projectValues)
		if err != nil {
			t.Fatal(err)
		}
		if orgValues != 2 || projectValues != 0 {
			t.Errorf("rows left: %d on every project's field, %d on the project's own; want 2 and 0", orgValues, projectValues)
		}
	})

	t.Run("each dropped value is named in the history", func(t *testing.T) {
		for key, want := range map[string]string{moved.Key: "Web", movedChild.Key: "Mobile"} {
			history, err := ws.issues.History(ws.ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			if len(history) == 0 {
				t.Fatalf("%s has no history", key)
			}
			latest := history[0]
			if c := changeOf(latest, platformName); c == nil || c.From != want || c.To != "" {
				t.Errorf("%s: latest history %+v, want %s cleared from %s", key, latest.Changes, platformName, want)
			}
			for _, kept := range []string{costName, customerName} {
				if c := changeOf(latest, kept); c != nil {
					t.Errorf("%s: the move records %s, which it did not clear: %+v", key, kept, c)
				}
			}
		}
	})
}
