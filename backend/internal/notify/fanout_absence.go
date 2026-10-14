package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// absencesPath is the page where a person finds their own absences.
const absencesPath = "/settings/profile"

// absenceBody sends the person to the days instead of carrying them, since a
// mail leaves the building and the profile does not.
const absenceBody = "Your profile lists the days you are away."

func isAbsenceTopic(topic string) bool {
	switch topic {
	case events.TopicAbsenceRecorded, events.TopicAbsenceChanged, events.TopicAbsenceRemoved:
		return true
	}
	return false
}

// absenceTell words what somebody else did to a person's days. What the person
// did themselves needs no telling.
func absenceTell(topic string, p payload, actor string) (tell, bool) {
	if p.UserID == uuid.Nil || p.UserID == p.ActorID {
		return tell{}, false
	}
	var title string
	switch topic {
	case events.TopicAbsenceRecorded:
		title = fmt.Sprintf("%s recorded an absence for you", actor)
	case events.TopicAbsenceChanged:
		title = fmt.Sprintf("%s changed one of your absences", actor)
	case events.TopicAbsenceRemoved:
		title = fmt.Sprintf("%s removed one of your absences", actor)
	default:
		return tell{}, false
	}
	return tell{user: p.UserID, kind: KindAbsence, title: title, body: absenceBody}, true
}

// handleAbsence tells the person, while they still work here, that somebody
// else recorded, changed or removed one of their absences.
func (f *FanOut) handleAbsence(ctx context.Context, e events.Event, p payload) error {
	if _, ok := absenceTell(e.Topic, p, ""); !ok {
		return nil
	}
	ctx = db.PinPrimary(ctx)
	var (
		actor  string
		member bool
	)
	err := f.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT COALESCE((SELECT name FROM app_user WHERE id = $2), 'Somebody'),
			       EXISTS (SELECT 1 FROM org_member m JOIN app_user u ON u.id = m.user_id AND u.is_active
			               WHERE m.user_id = $1 AND m.org_id = current_org_id() AND m.org_role <> 'customer')`,
			p.UserID, p.ActorID).Scan(&actor, &member)
	})
	if err != nil || !member {
		return err
	}
	t, _ := absenceTell(e.Topic, p, actor)
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return f.deliver(ctx, e.ID, place{link: f.appURL + absencesPath}, t)
}
