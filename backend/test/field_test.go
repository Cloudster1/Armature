//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
)

func (ws *workspace) fields(h *harness) *field.Service { return field.NewService(h.cluster) }

func valueOf(values []field.Value, name string) *field.Value {
	for i := range values {
		if values[i].Field.Name == name {
			return &values[i]
		}
	}
	return nil
}

func TestCustomFieldsAreDefinedPerProjectAndAnsweredPerIssue(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "fielded")
	f := ws.fields(h)

	customer, _, err := f.Create(ws.ctx, ws.project.Key, field.Input{Name: "Customer", Kind: field.Text})
	if err != nil {
		t.Fatal(err)
	}
	platform, _, err := f.Create(ws.ctx, ws.project.Key, field.Input{Name: "Platform", Kind: field.Select, Options: []string{"Web", " Mobile ", "Web"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.Options) != 2 {
		t.Fatalf("options were not tidied: %v", platform.Options)
	}

	t.Run("a name is unique within the project", func(t *testing.T) {
		if _, _, err := f.Create(ws.ctx, ws.project.Key, field.Input{Name: "customer", Kind: field.Number}); !errors.Is(err, field.ErrNameTaken) {
			t.Fatalf("want ErrNameTaken, got %v", err)
		}
	})

	t.Run("an unknown kind is refused", func(t *testing.T) {
		if _, _, err := f.Create(ws.ctx, ws.project.Key, field.Input{Name: "Colour", Kind: "colour"}); !errors.Is(err, field.ErrBadKind) {
			t.Fatalf("want ErrBadKind, got %v", err)
		}
	})

	made := ws.newIssue(t, "needs a customer")

	t.Run("every field is listed, answered or not", func(t *testing.T) {
		values, err := f.Values(ws.ctx, made.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != 2 {
			t.Fatalf("want 2 values, got %d", len(values))
		}
		if v := valueOf(values, "Customer"); v == nil || v.Value != nil {
			t.Fatalf("an unanswered field should be listed with no value: %+v", v)
		}
	})

	t.Run("an answer is stored, shown and recorded", func(t *testing.T) {
		set, _, err := f.Set(ws.ctx, made.Key, customer.ID, json.RawMessage(`"  Acme  "`), ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if set.Display != "Acme" || string(set.Value) != `"Acme"` {
			t.Fatalf("got %s / %q", set.Value, set.Display)
		}
		if _, _, err := f.Set(ws.ctx, made.Key, platform.ID, json.RawMessage(`"Mobile"`), ws.actor); err != nil {
			t.Fatal(err)
		}
		values, err := f.Values(ws.ctx, made.Key)
		if err != nil {
			t.Fatal(err)
		}
		if v := valueOf(values, "Platform"); v == nil || v.Display != "Mobile" {
			t.Fatalf("platform = %+v", v)
		}

		history, err := ws.issues.History(ws.ctx, made.Key)
		if err != nil {
			t.Fatal(err)
		}
		var recorded bool
		for _, entry := range history {
			for _, change := range entry.Changes {
				if change.Field == "Customer" && change.To == "Acme" {
					recorded = true
				}
			}
		}
		if !recorded {
			t.Fatalf("the changelog does not record the field under its own name: %+v", history)
		}
	})

	t.Run("a value that does not fit is refused", func(t *testing.T) {
		if _, _, err := f.Set(ws.ctx, made.Key, platform.ID, json.RawMessage(`"Desk"`), ws.actor); !errors.Is(err, field.ErrBadValue) {
			t.Fatalf("want ErrBadValue, got %v", err)
		}
	})

	t.Run("a null clears the answer", func(t *testing.T) {
		if _, _, err := f.Set(ws.ctx, made.Key, customer.ID, json.RawMessage(`null`), ws.actor); err != nil {
			t.Fatal(err)
		}
		values, _ := f.Values(ws.ctx, made.Key)
		if v := valueOf(values, "Customer"); v == nil || v.Value != nil {
			t.Fatalf("customer should be cleared: %+v", v)
		}
	})

	t.Run("deleting the field takes its answers with it", func(t *testing.T) {
		if _, err := f.Delete(ws.ctx, platform.ID); err != nil {
			t.Fatal(err)
		}
		var left int
		_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue_field_value WHERE field_id = $1`, platform.ID).Scan(&left)
		if left != 0 {
			t.Fatalf("%d values left behind", left)
		}
	})
}

func TestAFieldBelongsToOneProjectAndTheDatabaseKnowsIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "twoproj")
	f := ws.fields(h)

	other := ws.fromTemplate(t, h, "kanban", "Other")
	otherField, _, err := f.Create(ws.ctx, other.Key, field.Input{Name: "Region", Kind: field.Text})
	if err != nil {
		t.Fatal(err)
	}
	made := ws.newIssue(t, "in the first project")

	if _, _, err := f.Set(ws.ctx, made.Key, otherField.ID, json.RawMessage(`"EU"`), ws.actor); !errors.Is(err, field.ErrOtherProject) {
		t.Fatalf("service: want ErrOtherProject, got %v", err)
	}

	// The same thing straight through SQL, as the application role.
	_, err = h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO issue_field_value (org_id, issue_id, field_id, value)
			VALUES (current_org_id(), $1, $2, '"EU"'::jsonb)`, made.ID, otherField.ID)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "issue_field_value_same_project" {
		t.Fatalf("database: want the same-project trigger to refuse, got %v", err)
	}
}

func TestFieldsAreTenantIsolated(t *testing.T) {
	h := newHarness(t)
	a := h.newWorkspace(t, "fielda")
	b := h.newWorkspace(t, "fieldb")
	f := a.fields(h)

	made, _, err := f.Create(a.ctx, a.project.Key, field.Input{Name: "Secret", Kind: field.Text})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Field(b.ctx, made.ID); !errors.Is(err, field.ErrNotFound) {
		t.Fatalf("the other tenant should not see the field, got %v", err)
	}
	if _, err := f.Delete(b.ctx, made.ID); !errors.Is(err, field.ErrNotFound) {
		t.Fatalf("the other tenant should not be able to delete the field, got %v", err)
	}
	var seen int
	if err := h.cluster.Read(b.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM custom_field WHERE id = $1`, made.ID).Scan(&seen)
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Fatal("row level security let another tenant read the field")
	}
}

func TestFieldsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "fieldapi")

	if made := owner.post("/api/v1/projects", map[string]any{"name": "Fielded", "key": "FLD"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	kinds := owner.get("/api/v1/field-kinds")
	if kinds.Status != http.StatusOK || len(kinds.Body["kinds"].([]any)) != len(field.Kinds()) {
		t.Fatalf("field kinds: %d %s", kinds.Status, kinds.Raw)
	}

	created := owner.post("/api/v1/projects/FLD/fields", map[string]any{"name": "Cost", "kind": "number"})
	if created.Status != http.StatusCreated {
		t.Fatalf("create field: %d %s", created.Status, created.Raw)
	}
	fieldID := created.Body["field"].(map[string]any)["id"].(string)

	issued := owner.post("/api/v1/projects/FLD/issues", map[string]any{"summary": "priced work"})
	if issued.Status != http.StatusCreated {
		t.Fatalf("create issue: %d %s", issued.Status, issued.Raw)
	}
	key := issued.Body["issue"].(map[string]any)["key"].(string)

	set := owner.put("/api/v1/issues/"+key+"/fields/"+fieldID, map[string]any{"value": "12.5"})
	if set.Status != http.StatusOK {
		t.Fatalf("set value: %d %s", set.Status, set.Raw)
	}
	if display := set.Body["value"].(map[string]any)["display"]; display != "12.5" {
		t.Fatalf("display = %v", display)
	}
	if bad := owner.put("/api/v1/issues/"+key+"/fields/"+fieldID, map[string]any{"value": "a lot"}); bad.Status != http.StatusBadRequest {
		t.Fatalf("a non-number should be refused with 400, got %d %s", bad.Status, bad.Raw)
	}

	listed := owner.get("/api/v1/issues/" + key + "/fields")
	values := listed.Body["values"].([]any)
	if len(values) != 1 || values[0].(map[string]any)["value"] != 12.5 {
		t.Fatalf("values = %s", listed.Raw)
	}

	t.Run("another tenant cannot touch the field", func(t *testing.T) {
		stranger := api.client(t)
		stranger.signup(t, h, "fieldstranger")
		if resp := stranger.delete("/api/v1/fields/" + fieldID); resp.Status != http.StatusNotFound {
			t.Fatalf("want 404, got %d %s", resp.Status, resp.Raw)
		}
		if resp := stranger.get("/api/v1/issues/" + key + "/fields"); resp.Status != http.StatusNotFound {
			t.Fatalf("want 404, got %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("a renamed field keeps its answers", func(t *testing.T) {
		if resp := owner.do(http.MethodPatch, "/api/v1/fields/"+fieldID, map[string]any{"name": "Price"}); resp.Status != http.StatusOK {
			t.Fatalf("rename: %d %s", resp.Status, resp.Raw)
		}
		again := owner.get("/api/v1/issues/" + key + "/fields")
		v := again.Body["values"].([]any)[0].(map[string]any)
		if v["field"].(map[string]any)["name"] != "Price" || v["display"] != "12.5" {
			t.Fatalf("after rename: %s", again.Raw)
		}
	})
}
