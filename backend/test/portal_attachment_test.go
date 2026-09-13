//go:build integration

package test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// A customer's files are the request's: they see the desk's beside their own,
// a walk-in at another desk sees none of them, and only their own come off.
func TestACustomerPutsFilesOnTheirRequestAndTakesBackOnlyTheirOwn(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "filedesk")
	slug := principalField(t, signedUp, "principal", "org", "slug").(string)

	want(t, owner.post("/api/v1/projects", map[string]any{"name": "IT desk", "key": "FIT", "template": "service-desk"}), http.StatusCreated, "a desk")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "HR desk", "key": "FHR", "template": "service-desk"}), http.StatusCreated, "another desk")
	want(t, owner.patch("/api/v1/projects/FHR", map[string]any{"portalVerifies": false}), http.StatusOK, "open the HR door")
	itTypes := list(t, want(t, owner.get("/api/v1/projects/FIT/request-types"), http.StatusOK, "IT request types"), "requestTypes")
	key, customer := raiseAsCustomer(t, h, api, slug, h.email(t, "filer"), itTypes[0].(map[string]any)["id"].(string))

	mine := []byte("The screen went black at 9:12.\n")
	attached := want(t, customer.upload("/api/v1/portal/requests/"+key+"/attachments", "file", "mine.txt", "text/plain", mine), http.StatusCreated, "a customer's file")
	mineID := obj(t, attached, "attachment")["id"].(string)
	want(t, customer.post("/api/v1/portal/requests/"+key+"/attachments", map[string]any{"file": "not a form"}), http.StatusBadRequest, "not multipart")

	theirs := []byte("Try holding the power button for ten seconds.\n")
	answered := want(t, owner.upload("/api/v1/issues/"+key+"/attachments", "file", "agent.txt", "text/plain", theirs), http.StatusCreated, "the desk's file")
	agentID := obj(t, answered, "attachment")["id"].(string)

	t.Run("the customer sees the desk's file beside their own", func(t *testing.T) {
		names := fileNames(t, want(t, customer.get("/api/v1/portal/requests/"+key+"/attachments"), http.StatusOK, "the files"))
		if names != "agent.txt mine.txt" && names != "mine.txt agent.txt" {
			t.Fatalf("files = %q, want both", names)
		}
		resp, data := customer.download("/api/v1/portal/attachments/" + agentID)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(data, theirs) {
			t.Fatalf("download: %d %q", resp.StatusCode, data)
		}
		if disposition := resp.Header.Get("Content-Disposition"); !strings.Contains(disposition, "agent.txt") {
			t.Errorf("Content-Disposition = %q", disposition)
		}
	})

	t.Run("a walk-in at another desk finds nothing", func(t *testing.T) {
		walkIn := api.client(t)
		want(t, walkIn.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": h.email(t, "walkin"), "name": "Walk In", "desk": "FHR"}), http.StatusOK, "in at HR")
		want(t, walkIn.get("/api/v1/portal/requests/"+key+"/attachments"), http.StatusNotFound, "not their request")
		resp, _ := walkIn.download("/api/v1/portal/attachments/" + agentID)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("a walk-in read the file: %d", resp.StatusCode)
		}
	})

	t.Run("only their own file comes off", func(t *testing.T) {
		refused := want(t, customer.delete("/api/v1/portal/attachments/"+agentID), http.StatusForbidden, "the desk's file")
		if refused.ErrorCode() != "not_your_file" {
			t.Errorf("code = %q", refused.ErrorCode())
		}
		want(t, customer.delete("/api/v1/portal/attachments/"+mineID), http.StatusNoContent, "their own")
		if names := fileNames(t, want(t, customer.get("/api/v1/portal/requests/"+key+"/attachments"), http.StatusOK, "the files")); names != "agent.txt" {
			t.Fatalf("files after = %q", names)
		}
		if names := fileNames(t, want(t, owner.get("/api/v1/issues/"+key+"/attachments"), http.StatusOK, "the agent's view")); names != "agent.txt" {
			t.Fatalf("the agent sees %q", names)
		}
	})
}

// fileNames joins the names in an attachments envelope, in order.
func fileNames(t *testing.T, resp response) string {
	t.Helper()
	var names []string
	for _, a := range list(t, resp, "attachments") {
		names = append(names, a.(map[string]any)["fileName"].(string))
	}
	return strings.Join(names, " ")
}
