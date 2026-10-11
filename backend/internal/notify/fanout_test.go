package notify

import (
	"slices"
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

func TestAPersonIsToldWhenSomebodyElseTouchesTheirAbsences(t *testing.T) {
	ada, maxID := uuid.New(), uuid.New()
	for topic, title := range map[string]string{
		events.TopicAbsenceRecorded: "Max Master recorded an absence for you",
		events.TopicAbsenceChanged:  "Max Master changed one of your absences",
		events.TopicAbsenceRemoved:  "Max Master removed one of your absences",
	} {
		got, ok := absenceTell(topic, payload{UserID: ada, ActorID: maxID}, "Max Master")
		if !ok || got.user != ada || got.kind != KindAbsence || got.title != title {
			t.Errorf("%s tells %+v, want Ada told %q", topic, got, title)
		}
		if strings.ContainsAny(got.title+got.body, "0123456789") {
			t.Errorf("%s tells %+v, which reads like a day; the notice never carries the days", topic, got)
		}
		if _, ok := absenceTell(topic, payload{UserID: ada, ActorID: ada}, "Ada Away"); ok {
			t.Errorf("%s by the person herself told her", topic)
		}
	}
	if _, ok := absenceTell(events.TopicIssueCreated, payload{UserID: ada, ActorID: maxID}, "Max Master"); ok {
		t.Error("an issue event was told as an absence")
	}
	if _, ok := absenceTell(events.TopicAbsenceRecorded, payload{ActorID: maxID}, "Max Master"); ok {
		t.Error("an absence for nobody was told")
	}
	if !slices.Contains(Kinds, KindAbsence) {
		t.Errorf("Kinds = %v, want absences among them so the preferences page can turn them off", Kinds)
	}
	if err := (Preferences{Digest: DigestOff, Mail: map[string]bool{KindAbsence: false}}).Validate(); err != nil {
		t.Errorf("turning absence mail off was refused: %v", err)
	}
}

// A person who turned watching their own issues off hears about the issues
// they report or are assigned only when they watch, are assigned or are named.
func TestTellsForLeavesOutWhoDoesNotWatchTheirOwn(t *testing.T) {
	ada, bob, cy, dee := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	moved := payload{ActorID: ada, ToStatus: "Done"}
	users := func(tells []tell) map[uuid.UUID]string {
		out := map[uuid.UUID]string{}
		for _, tl := range tells {
			out[tl.user] = tl.kind
		}
		return out
	}

	t.Run("a reporter who does not watch their own hears nothing of a move or a comment", func(t *testing.T) {
		s := &subject{key: "CP-3", reporter: &bob, assignee: &cy, watchers: []uuid.UUID{dee}, actor: "Ada", watchOwnOff: map[uuid.UUID]bool{bob: true}}
		got := users(tellsFor(events.TopicIssueTransitioned, moved, s))
		if _, told := got[bob]; told || got[cy] != KindTransitioned || got[dee] != KindTransitioned || len(got) != 2 {
			t.Errorf("told %v, want the assignee and the watcher, not the reporter", got)
		}
		commented := users(tellsFor(events.TopicCommentAdded, payload{ActorID: ada}, s))
		if _, told := commented[bob]; told {
			t.Errorf("a comment told the reporter who does not watch their own: %v", commented)
		}
	})

	t.Run("an assignee who does not watch their own is told of the assignment and a mention only", func(t *testing.T) {
		s := &subject{key: "CP-4", reporter: &ada, assignee: &bob, actor: "Ada", watchOwnOff: map[uuid.UUID]bool{bob: true}}
		assigned := tellsFor(events.TopicIssueUpdated, payload{ActorID: ada, Changes: []struct {
			Field string `json:"field"`
		}{{Field: "assignee"}}}, s)
		if len(assigned) != 1 || assigned[0].user != bob || assigned[0].kind != KindAssigned {
			t.Errorf("assignment told %+v, want bob assigned and nothing else", assigned)
		}
		created := tellsFor(events.TopicIssueCreated, payload{ActorID: ada}, s)
		if len(created) != 1 || created[0].kind != KindAssigned {
			t.Errorf("creation told %+v, want only the assignment", created)
		}
		if got := tellsFor(events.TopicIssueTransitioned, moved, s); len(got) != 0 {
			t.Errorf("a move told %+v, want nobody", got)
		}
		mentioned := tellsFor(events.TopicCommentAdded, payload{ActorID: ada, Mentions: []uuid.UUID{bob}}, s)
		if len(mentioned) != 1 || mentioned[0].user != bob || mentioned[0].kind != KindMentioned {
			t.Errorf("a mention told %+v, want bob mentioned", mentioned)
		}
	})

	t.Run("watching an own issue on purpose still tells them, once", func(t *testing.T) {
		s := &subject{key: "CP-5", reporter: &bob, assignee: &bob, watchers: []uuid.UUID{bob}, actor: "Ada", watchOwnOff: map[uuid.UUID]bool{bob: true}}
		got := tellsFor(events.TopicIssueTransitioned, moved, s)
		if len(got) != 1 || got[0].user != bob || got[0].kind != KindTransitioned {
			t.Errorf("told %+v, want bob once as a watcher", got)
		}
	})

	t.Run("by default a reporter and an assignee watch their own", func(t *testing.T) {
		s := &subject{key: "CP-6", reporter: &bob, assignee: &cy, actor: "Ada"}
		got := users(tellsFor(events.TopicIssueTransitioned, moved, s))
		if got[bob] != KindTransitioned || got[cy] != KindTransitioned || len(got) != 2 {
			t.Errorf("told %v, want the reporter and the assignee", got)
		}
	})
}
