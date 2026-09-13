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

// A desk names the domains it takes requests from; the door, the portal and
// the database all keep to the list, and an empty list is everyone.
func TestADeskTrustsDomainsNotAddresses(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "trusting")
	slug := principalField(t, signedUp, "principal", "org", "slug").(string)
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM app_user WHERE email LIKE '%@elsewhere.test'`)
	})

	want(t, owner.post("/api/v1/projects", map[string]any{"name": "IT desk", "key": "TIT", "template": "service-desk"}), http.StatusCreated, "a desk")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "HR desk", "key": "THR", "template": "service-desk"}), http.StatusCreated, "another desk")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Code", "key": "TCO"}), http.StatusCreated, "a software project")

	t.Run("only a desk has a list, and it is one spelling of itself", func(t *testing.T) {
		want(t, owner.patch("/api/v1/projects/TCO", map[string]any{"trustedDomains": []string{"acme.test"}}), http.StatusConflict, "a software project has no door")
		refused := want(t, owner.patch("/api/v1/projects/TIT", map[string]any{"trustedDomains": []string{"not a domain"}}), http.StatusUnprocessableEntity, "not a domain")
		if fields, _ := refused.Error()["fields"].(map[string]any); fields["trustedDomains"] == nil {
			t.Errorf("the refusal does not name the field: %v", refused.Error())
		}
		updated := obj(t, want(t, owner.patch("/api/v1/projects/TIT", map[string]any{"trustedDomains": []string{" @Armature.TEST ", "armature.test"}}), http.StatusOK, "trust a domain"), "project")
		if domains := updated["trustedDomains"].([]any); len(domains) != 1 || domains[0] != "armature.test" {
			t.Fatalf("trustedDomains = %v", domains)
		}
	})

	t.Run("the code door refuses a domain only when no desk here takes it", func(t *testing.T) {
		stranger := api.client(t)
		want(t, stranger.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": "walkin-a@elsewhere.test"}), http.StatusAccepted, "the HR desk still trusts everyone")
		want(t, owner.patch("/api/v1/projects/THR", map[string]any{"trustedDomains": []string{"armature.test"}}), http.StatusOK, "now every desk names a domain")
		refused := want(t, api.client(t).post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": "walkin-b@elsewhere.test"}), http.StatusForbidden, "an address at another domain")
		if refused.ErrorCode() != "domain_not_trusted" || !strings.Contains(refused.Raw, "armature.test") {
			t.Fatalf("refusal = %s", refused.Raw)
		}
		want(t, api.client(t).post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": h.email(t, "trusted")}), http.StatusAccepted, "an address at the trusted domain")
	})

	t.Run("the open door keeps to the list too, and so does the database", func(t *testing.T) {
		want(t, owner.patch("/api/v1/projects/TIT", map[string]any{"portalVerifies": false}), http.StatusOK, "open the door")
		refused := want(t, api.client(t).post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": "walkin-c@elsewhere.test", "name": "Else Where", "desk": "TIT"}), http.StatusForbidden, "turned away")
		if refused.ErrorCode() != "domain_not_trusted" {
			t.Fatalf("code = %q", refused.ErrorCode())
		}
		walker := api.client(t)
		entered := want(t, walker.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": h.email(t, "walkin"), "name": "Walk In", "desk": "TIT"}), http.StatusOK, "in at the trusted domain")
		walkerID := principalField(t, entered, "principal", "user", "id").(string)

		var elsewhereID, deskID uuid.UUID
		if err := h.super.QueryRow(context.Background(), `INSERT INTO app_user (email, name) VALUES ('walkin-d@elsewhere.test', 'Else') RETURNING id`).Scan(&elsewhereID); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM project WHERE key = 'TIT' AND org_id = (SELECT id FROM org WHERE slug = $1)`, slug).Scan(&deskID); err != nil {
			t.Fatal(err)
		}
		var pgErr *pgconn.PgError
		_, err := h.super.Exec(context.Background(), `INSERT INTO user_session (user_id, token_hash, expires_at, portal_project_id) VALUES ($1, $2, now() + interval '1 hour', $3)`, elsewhereID, []byte(uuid.NewString()), deskID)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a code-less session at an untrusted domain was accepted: %v", err)
		}
		_, err = h.super.Exec(context.Background(), `UPDATE project SET trusted_domains = '{acme.test}' WHERE key = 'TCO' AND org_id = (SELECT id FROM org WHERE slug = $1)`, slug)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a software project was allowed a list: %v", err)
		}
		_, err = h.super.Exec(context.Background(), `UPDATE project SET trusted_domains = '{"not a domain"}' WHERE id = $1`, deskID)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a malformed domain was stored: %v", err)
		}

		// Narrowing the list signs out the walk-in it no longer covers.
		want(t, owner.patch("/api/v1/projects/TIT", map[string]any{"trustedDomains": []string{"acme.test"}}), http.StatusOK, "trust another domain instead")
		want(t, walker.get("/api/v1/auth/me"), http.StatusUnauthorized, "the walk-in is out")
		_ = walkerID
		want(t, owner.patch("/api/v1/projects/TIT", map[string]any{"trustedDomains": []string{"armature.test"}}), http.StatusOK, "back")
	})

	t.Run("the portal offers only the desks that trust the address", func(t *testing.T) {
		hrTypes := list(t, want(t, owner.get("/api/v1/projects/THR/request-types"), http.StatusOK, "HR request types"), "requestTypes")
		itTypes := list(t, want(t, owner.get("/api/v1/projects/TIT/request-types"), http.StatusOK, "IT request types"), "requestTypes")
		key, customer := raiseAsCustomer(t, h, api, slug, h.email(t, "proven"), hrTypes[0].(map[string]any)["id"].(string))
		desks := list(t, want(t, customer.get("/api/v1/portal/desks"), http.StatusOK, "desks"), "desks")
		if len(desks) != 2 {
			t.Fatalf("a trusted customer sees %d desks, want both", len(desks))
		}
		want(t, owner.patch("/api/v1/projects/TIT", map[string]any{"trustedDomains": []string{"acme.test"}}), http.StatusOK, "IT trusts acme only")
		desks = list(t, want(t, customer.get("/api/v1/portal/desks"), http.StatusOK, "desks"), "desks")
		if len(desks) != 1 || desks[0].(map[string]any)["projectKey"] != "THR" {
			t.Fatalf("desks = %v, want HR alone", desks)
		}
		refused := want(t, customer.post("/api/v1/portal/requests", map[string]any{"requestTypeId": itTypes[0].(map[string]any)["id"], "summary": "Sneaking in"}), http.StatusForbidden, "raise at IT")
		if refused.ErrorCode() != "domain_not_trusted" {
			t.Fatalf("code = %q", refused.ErrorCode())
		}
		want(t, customer.get("/api/v1/portal/requests/"+key), http.StatusOK, "their own request still reads")
	})
}
