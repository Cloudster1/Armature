package httpapi

import (
	"strings"
	"testing"
)

// A project has several boards, so a route that names only the project cannot
// say whose swimlanes or grouping it changes; it used to change the first's.
func TestSwimlanesAreAddressedByTheirBoard(t *testing.T) {
	served := routed(t)
	for _, key := range []string{
		"POST /boards/{boardID}/swimlanes",
		"PUT /boards/{boardID}/swimlanes",
		"PATCH /boards/{boardID}/swimlanes/{swimlaneID}",
		"DELETE /boards/{boardID}/swimlanes/{swimlaneID}",
		"PATCH /boards/{boardID}",
	} {
		if !served[key] {
			t.Errorf("%s is not served", key)
		}
	}
	for key := range served {
		method, path, _ := strings.Cut(key, " ")
		if strings.HasPrefix(path, "/projects/{projectKey}/board/swimlanes") {
			t.Errorf("%s changes a board by its project, which cannot say which board", key)
		}
		if method == "PATCH" && path == "/projects/{projectKey}/board" {
			t.Errorf("%s regroups a board by its project, which cannot say which board", key)
		}
	}
}
