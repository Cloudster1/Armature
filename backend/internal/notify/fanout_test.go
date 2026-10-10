package notify

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/events"
)

func TestTellsForKeepsTheActorOutAndTellsEachPersonOnce(t *testing.T) {
	ada, bob, cy := uuid.New(), uuid.New(), uuid.New()
	s := &subject{key: "CP-1", summary: "Fix the door", assignee: &bob, reporter: &ada, watchers: []uuid.UUID{bob, cy}, actor: "Ada"}

	got := tellsFor(events.TopicIssueTransitioned, payload{ActorID: ada, ToStatus: "Done"}, s)
	if len(got) != 2 {
		t.Fatalf("tells = %+v, want the assignee and the other watcher once each", got)
	}
	for _, tl := range got {
		if tl.user == ada {
			t.Errorf("the actor was told about their own act")
		}
		if tl.kind != KindTransitioned || tl.title != "Ada moved CP-1 to Done" {
			t.Errorf("tell = %+v", tl)
		}
	}
}

func TestTellsForAssignmentAndMentions(t *testing.T) {
	ada, bob, cy := uuid.New(), uuid.New(), uuid.New()
	s := &subject{key: "CP-2", summary: "Paint it", assignee: &bob, reporter: &ada, watchers: []uuid.UUID{cy}, actor: "Ada", comment: "look @Bob"}

	assigned := tellsFor(events.TopicIssueUpdated, payload{ActorID: ada, Changes: []struct {
		Field string `json:"field"`
	}{{Field: "assignee"}}}, s)
	if len(assigned) != 2 || assigned[0].user != bob || assigned[0].kind != KindAssigned || assigned[1].user != cy || assigned[1].kind != KindWatching {
		t.Errorf("assignment tells = %+v, want bob assigned and cy watching", assigned)
	}

	commented := tellsFor(events.TopicCommentAdded, payload{ActorID: ada, Mentions: []uuid.UUID{bob}}, s)
	if len(commented) != 2 || commented[0].user != bob || commented[0].kind != KindMentioned || commented[0].body != "look @Bob" || commented[1].user != cy || commented[1].kind != KindCommented {
		t.Errorf("comment tells = %+v, want bob mentioned and cy commented", commented)
	}

	if got := tellsFor(events.TopicIssueUpdated, payload{ActorID: ada}, s); got != nil {
		t.Errorf("an update with no changes told %+v", got)
	}
	if got := tellsFor(events.TopicWatcherAdded, payload{ActorID: ada, UserID: ada}, s); got != nil {
		t.Errorf("watching yourself told %+v", got)
	}
}

func TestPreferencesDefaultToOn(t *testing.T) {
	p := DefaultPreferences()
	if !p.mails(KindAssigned) || !p.shows(KindMentioned) {
		t.Fatal("an unset kind should be on")
	}
	p.Mail[KindAssigned] = false
	if p.mails(KindAssigned) || !p.shows(KindAssigned) {
		t.Fatal("turning mail off should leave the inbox on")
	}
	if err := (Preferences{Digest: "weekly"}).Validate(); err == nil {
		t.Fatal("an unknown digest schedule was accepted")
	}
	if err := (Preferences{Digest: DigestOff, Mail: map[string]bool{"shouting": true}}).Validate(); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
}

// A customer's reply that the workflow could not take back out of done tells
// the agents on the request, or whoever finished it when nobody is on it.
func TestAReplyLeftInDoneIsToldToTheAgents(t *testing.T) {
	sam, bob, cy, dee := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	p := payload{ActorID: sam, Status: "Resolved"}

	on := &subject{key: "SD-1", summary: "Printer", assignee: &bob, watchers: []uuid.UUID{cy}, resolver: &dee, actor: "Sam", comment: "still broken"}
	got := tellsFor(events.TopicRequestStaysDone, p, on)
	if len(got) != 2 || got[0].user != bob || got[1].user != cy {
		t.Fatalf("tells = %+v, want the assignee and the agent following, not the resolver", got)
	}
	for _, tl := range got {
		if tl.kind != KindCommented || tl.title != "Sam replied to SD-1, which stays Resolved" {
			t.Errorf("tell = %+v", tl)
		}
		if !strings.Contains(tl.body, "still broken") || !strings.Contains(tl.body, "Move it by hand") {
			t.Errorf("body = %q, want the reply and what to do about it", tl.body)
		}
	}

	nobody := &subject{key: "SD-2", summary: "Printer", resolver: &dee, actor: "Sam", comment: "still broken"}
	if got := tellsFor(events.TopicRequestStaysDone, p, nobody); len(got) != 1 || got[0].user != dee {
		t.Errorf("tells = %+v, want whoever finished it", got)
	}
}
