package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/sprint"
)

// Work refused for belonging to another team's stream is a conflict the person
// can resolve, and the answer says how rather than reading as a server fault.
func TestAnotherTeamsSprintIsRefusedInASentence(t *testing.T) {
	cases := []struct {
		name string
		err  error
		says string
	}{
		{"committing to it", fmt.Errorf("%w: Alpha 1", issue.ErrSprintOtherTeam), "Hand the issue to"},
		{"carrying work into it", fmt.Errorf("%w: Beta 2", sprint.ErrOtherTeam), "backlog"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := toAPIError(c.err)
			if got.Status != http.StatusConflict {
				t.Errorf("status = %d, want %d", got.Status, http.StatusConflict)
			}
			if !strings.Contains(got.Message, c.says) || !strings.HasSuffix(got.Message, ".") {
				t.Errorf("message = %q, want a sentence saying what to do", got.Message)
			}
		})
	}
}
