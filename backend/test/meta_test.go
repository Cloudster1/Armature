//go:build integration

package test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/buildinfo"
	"github.com/armature/armature/backend/internal/httpapi"
)

// What a partner, an operator or Dynamo reads to tell which build it talks to:
// the stamp, the API version and the capabilities, the last to anyone.
func TestMetaReportsTheRunningBuild(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	current := buildinfo.Current()

	signedIn := api.client(t)
	signedIn.signIn(t, h.person(t, "meta", auth.RoleMember))
	meta := want(t, signedIn.get("/api/v1/meta"), http.StatusOK, "meta, signed in")
	if meta.Body["product"] != "armature" {
		t.Errorf("product = %v, want armature", meta.Body["product"])
	}
	if meta.Body["apiVersion"] != httpapi.Spec().Info.Version {
		t.Errorf("apiVersion = %v, the document says %s", meta.Body["apiVersion"], httpapi.Spec().Info.Version)
	}
	wantCaps := []any{"calendar@1", "issue-embed@1", "oauth.partner-tokens@1", "oauth.token-exchange@1", "plan@1", "remote-link-relations@1", "remote-links@1", "reports@1", "theme-export@1", "webhooks.partner@1", "webhooks@1"}
	if caps, ok := meta.Body["capabilities"].([]any); !ok || !slices.Equal(caps, wantCaps) {
		t.Errorf("capabilities = %v, want %v", meta.Body["capabilities"], wantCaps)
	}
	build := obj(t, meta, "build")
	if build["version"] != current.Version || build["commit"] != current.Commit || build["builtAt"] != current.BuiltAt {
		t.Errorf("build = %v, want the running build %+v", build, current)
	}

	anonymous := want(t, api.client(t).get("/api/v1/meta"), http.StatusOK, "meta, nobody signed in")
	seen := obj(t, anonymous, "build")
	if seen["version"] != current.Version {
		t.Errorf("version = %v, want %s", seen["version"], current.Version)
	}
	if _, told := seen["commit"]; told {
		t.Errorf("somebody not signed in was told the commit: %s", anonymous.Raw)
	}
	if _, told := seen["builtAt"]; told {
		t.Errorf("somebody not signed in was told when it was built: %s", anonymous.Raw)
	}
	if caps, ok := anonymous.Body["capabilities"].([]any); !ok || !slices.Equal(caps, wantCaps) {
		t.Errorf("capabilities = %v anonymously, want %v", anonymous.Body["capabilities"], wantCaps)
	}
	if anonymous.Body["apiVersion"] != meta.Body["apiVersion"] {
		t.Errorf("apiVersion = %v anonymously, %v signed in", anonymous.Body["apiVersion"], meta.Body["apiVersion"])
	}
}
