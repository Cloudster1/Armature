package filter

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// The mail ends with the page that runs the saved search, which is the web
// app's /filters/$filterId (#412), not the list of them.
func TestASubscriptionMailLinksToItsSavedSearch(t *testing.T) {
	id := uuid.New()
	s := &Subscriptions{appURL: "http://app.test"}
	m := s.mail("ada@armature.test", "My open work", id, &issue.Result{
		Total:  1,
		Issues: []issue.Issue{{Key: "CP-1", Summary: "Oil the hinge", Status: workflow.Status{Name: "To Do"}}},
	})
	want := "Open the search at http://app.test/filters/" + id.String() + "\n"
	if !strings.HasSuffix(m.Body, want) {
		t.Errorf("body = %q, want it to end %q", m.Body, want)
	}
	if !strings.Contains(m.Body, "- CP-1  Oil the hinge  [To Do]") || m.Subject != "My open work: 1 issue" {
		t.Errorf("mail = %+v, want the issue listed under the search's name", m)
	}
}

func TestDue(t *testing.T) {
	now := time.Date(2026, 9, 7, 9, 30, 0, 0, time.UTC) // a Monday
	yesterday := now.AddDate(0, 0, -1)
	if !due(Daily, 8, nil, nil, now) || !due(Daily, 8, nil, &yesterday, now) {
		t.Error("a daily subscription at 08:00 is due at 09:30 when never or last sent yesterday")
	}
	sentToday := time.Date(2026, 9, 7, 8, 5, 0, 0, time.UTC)
	if due(Daily, 8, nil, &sentToday, now) {
		t.Error("a daily subscription sent at 08:05 is not due again at 09:30")
	}
	sentYesterdayAfter := time.Date(2026, 9, 6, 10, 5, 0, 0, time.UTC)
	if due(Daily, 10, nil, &sentYesterdayAfter, now) {
		t.Error("a daily subscription at 10:00 sent yesterday at 10:05 is not due at 09:30")
	}
	monday := int(time.Monday)
	tuesday := int(time.Tuesday)
	lastWeek := now.AddDate(0, 0, -7)
	friday := now.AddDate(0, 0, -3)
	if !due(Weekly, 8, &monday, &lastWeek, now) || due(Weekly, 8, &tuesday, &friday, now) {
		t.Error("weekly on Monday 08:00 is due Monday 09:30; weekly on Tuesday sent on Friday is not")
	}
	if !due(Weekly, 8, &tuesday, &lastWeek, now) {
		t.Error("a weekly subscription that missed its day catches up")
	}
	if err := (Subscription{Schedule: Weekly, Hour: 8}).Validate(); err == nil {
		t.Error("weekly without a weekday was accepted")
	}
	if err := (Subscription{Schedule: Daily, Hour: 25}).Validate(); err == nil {
		t.Error("hour 25 was accepted")
	}
}
