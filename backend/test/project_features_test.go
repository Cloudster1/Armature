//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// A template decides which pages a project has. Off means hidden and refused,
// not deleted, and an administrator may turn a page back on.
func TestATemplateDecidesAProjectsFeatures(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "featured")
	slug := principalField(t, signedUp, "principal", "org", "slug").(string)

	desk := obj(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "Helpdesk", "key": "FHD", "template": "service-desk"}), http.StatusCreated, "a desk"), "project")
	code := obj(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "Code", "key": "FCO"}), http.StatusCreated, "a kanban project"), "project")

	names := func(p map[string]any) string {
		parts := make([]string, 0)
		for _, f := range p["features"].([]any) {
			parts = append(parts, f.(string))
		}
		return strings.Join(parts, ",")
	}

	t.Run("a desk has the desk's pages and a kanban project everything else", func(t *testing.T) {
		if got := names(desk); got != "board,calendar,dashboard,queues,desk,teams,automation,import" {
			t.Fatalf("desk features = %s", got)
		}
		if got := names(code); strings.Contains(got, "queues") || !strings.Contains(got, "sprints") || !strings.Contains(got, "repositories") {
			t.Fatalf("kanban features = %s", got)
		}
		templates := list(t, want(t, owner.get("/api/v1/project-templates"), http.StatusOK, "templates"), "templates")
		for _, each := range templates {
			tpl := each.(map[string]any)
			if tpl["key"] == "task-tracking" && strings.Contains(names(tpl), "sprints") {
				t.Fatal("task tracking offers sprints")
			}
		}
	})

	t.Run("a page the project does not have is refused, and the answer says where to turn it on", func(t *testing.T) {
		refused := want(t, owner.post("/api/v1/projects/FHD/sprints", map[string]any{"name": "Sprint 1"}), http.StatusConflict, "a sprint on a desk")
		if refused.ErrorCode() != "feature_off" || !strings.Contains(refused.Raw, "FHD does not use sprints. Turn it on under the project's settings.") {
			t.Fatalf("refusal = %s", refused.Raw)
		}
		want(t, owner.post("/api/v1/projects/FHD/milestones", map[string]any{"name": "Launch"}), http.StatusConflict, "a milestone on a desk")
		want(t, owner.post("/api/v1/projects/FHD/teams", map[string]any{"name": "Front line"}), http.StatusCreated, "a team on a desk is fine")
	})

	t.Run("and the database refuses the row whatever the service believed", func(t *testing.T) {
		var deskID, codeID uuid.UUID
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM project WHERE key = 'FHD' AND org_id = (SELECT id FROM org WHERE slug = $1)`, slug).Scan(&deskID); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM project WHERE key = 'FCO' AND org_id = (SELECT id FROM org WHERE slug = $1)`, slug).Scan(&codeID); err != nil {
			t.Fatal(err)
		}
		var pgErr *pgconn.PgError
		_, err := h.super.Exec(context.Background(), `INSERT INTO sprint (org_id, project_id, name) VALUES ((SELECT org_id FROM project WHERE id = $1), $1, 'Sneaked in')`, deskID)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a sprint went into a desk: %v", err)
		}
		_, err = h.super.Exec(context.Background(), `UPDATE project SET features = '{queues}' WHERE id = $1`, codeID)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a software project was given queues: %v", err)
		}
		_, err = h.super.Exec(context.Background(), `UPDATE project SET features = '{gantt}' WHERE id = $1`, codeID)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("an unknown feature was accepted: %v", err)
		}
	})

	t.Run("an administrator turns a page on, and the desk's pages stay the desk's", func(t *testing.T) {
		refused := want(t, owner.patch("/api/v1/projects/FHD", map[string]any{"features": []string{"board", "gantt"}}), http.StatusUnprocessableEntity, "not a feature")
		if fields, _ := refused.Error()["fields"].(map[string]any); fields["features"] == nil {
			t.Errorf("the refusal does not name the field: %v", refused.Error())
		}
		want(t, owner.patch("/api/v1/projects/FCO", map[string]any{"features": []string{"board", "queues"}}), http.StatusConflict, "queues off a desk")
		updated := obj(t, want(t, owner.patch("/api/v1/projects/FHD", map[string]any{"features": []string{"sprints", "board", "desk", "queues", "sprints"}}), http.StatusOK, "turn sprints on"), "project")
		if got := names(updated); got != "board,sprints,queues,desk" {
			t.Fatalf("features after the change = %s", got)
		}
		want(t, owner.post("/api/v1/projects/FHD/sprints", map[string]any{"name": "Sprint 1"}), http.StatusCreated, "a sprint once the page is on")
	})
}
