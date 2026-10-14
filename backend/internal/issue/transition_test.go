package issue

import (
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/workflow"
)

func TestAMoveNamesTheStatusCategoriesItCrosses(t *testing.T) {
	id := uuid.New()
	before := &Issue{ID: id, Key: "NOJ-7", Status: workflow.Status{Name: "To Do", Category: workflow.CategoryTodo}}
	after := &Issue{ID: id, Key: "NOJ-7", Status: workflow.Status{Name: "In Progress", Category: workflow.CategoryInProgress}}
	actor := Actor{UserID: uuid.New()}

	got := movePayload(before, after, "Start progress", actor)

	want := map[string]any{
		"issueId":            id,
		"key":                "NOJ-7",
		"transition":         "Start progress",
		"fromStatus":         "To Do",
		"toStatus":           "In Progress",
		"fromStatusCategory": workflow.CategoryTodo,
		"toStatusCategory":   workflow.CategoryInProgress,
		"actorId":            actor.UserID,
	}
	for field, value := range want {
		if got[field] != value {
			t.Errorf("%s = %v, want %v", field, got[field], value)
		}
	}
	if len(got) != len(want) {
		t.Errorf("payload has %d fields, want %d: %v", len(got), len(want), got)
	}
}
