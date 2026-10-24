package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/issue"
)

// The shape of a set of moves is refused before any issue is read.
func TestASetOfMovesIsRefusedWhenItIsMalformed(t *testing.T) {
	move := `{"key":"PR-1","expected":{"startDate":"2027-11-01","dueDate":null},"startDate":"2027-11-08","dueDate":"2027-11-09"}`
	many := make([]string, issue.MaxScheduleMoves+1)
	for i := range many {
		many[i] = move
	}
	// The decoder's own refusals are the same for every endpoint; the rest are this one's sentences.
	decoded := map[string]bool{"not a date": true, "an unknown field": true}
	cases := map[string]string{
		"no moves":         `{"moves":[]}`,
		"too many":         `{"moves":[` + strings.Join(many, ",") + `]}`,
		"no key":           `{"moves":[{"expected":{"startDate":null,"dueDate":null},"startDate":"2027-11-08","dueDate":"2027-11-09"}]}`,
		"no expected due":  `{"moves":[{"key":"PR-1","expected":{"startDate":null},"startDate":"2027-11-08","dueDate":"2027-11-09"}]}`,
		"no new start":     `{"moves":[{"key":"PR-1","expected":{"startDate":null,"dueDate":null},"dueDate":"2027-11-09"}]}`,
		"not a date":       `{"moves":[{"key":"PR-1","expected":{"startDate":null,"dueDate":null},"startDate":"soon","dueDate":"2027-11-09"}]}`,
		"an unknown field": `{"moves":[` + move + `],"force":true}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPut, "/api/v1/projects/PR/plan/schedule", strings.NewReader(body))
			rec := httptest.NewRecorder()
			(&Server{}).handleSchedulePlan(rec, r)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			var answer struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || (!decoded[name] && !strings.HasSuffix(answer.Error.Message, ".")) {
				t.Errorf("the refusal %s is not a sentence", rec.Body)
			}
		})
	}
}
