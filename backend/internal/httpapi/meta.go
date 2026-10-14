package httpapi

import (
	"net/http"

	"github.com/armature/armature/backend/internal/buildinfo"
)

// product is what this tool calls itself at /meta, beside Stator and Exciter.
const product = "armature"

// apiVersion is the document's info.version: it rises only for a change that
// breaks a client, never with a release.
const apiVersion = "1"

// meta is the answer all three tools give at /meta, so a partner, an operator
// or Dynamo can tell which build it talks to.
type meta struct {
	Product      string         `json:"product"`
	Build        buildinfo.Info `json:"build"`
	APIVersion   string         `json:"apiVersion"`
	Capabilities []string       `json:"capabilities"`
}

// handleMeta answers anyone; the commit and when it was built are only for
// somebody signed in and past enrolment, as they were at /build.
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	build := buildinfo.Current()
	if p := PrincipalFrom(r.Context()); p == nil || p.MustEnrol {
		build.Commit, build.BuiltAt = "", ""
	}
	respondJSON(w, r, http.StatusOK, meta{Product: product, Build: build, APIVersion: apiVersion, Capabilities: capabilities()})
}
