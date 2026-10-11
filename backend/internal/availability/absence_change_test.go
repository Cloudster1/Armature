package availability

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/perm"
)

func TestAManagerChangesOnlyTheAbsencesTheyRecorded(t *testing.T) {
	ada, maxID, owner := uuid.New(), uuid.New(), uuid.New()
	hers := &Absence{ID: uuid.New(), UserID: ada, UserName: "Ada Away"}
	master := Recorder{ID: maxID, Perms: perm.NewSet([]perm.Grant{{Role: perm.ScrumMaster, ProjectKey: "AWY"}})}
	admin := Recorder{ID: owner, Perms: perm.NewSet([]perm.Grant{{Role: perm.GlobalAdministrator}})}
	person := Recorder{ID: ada}
	manages := func() error { return nil }
	managesNoLonger := func() error { return ErrMayNotRecord }
	unasked := func() error {
		t.Error("whether the caller manages the person was asked when it could not matter")
		return nil
	}

	allowed := []struct {
		name       string
		by         Recorder
		recordedBy *uuid.UUID
		manages    func() error
	}{
		{"the person, one they recorded", person, &ada, unasked},
		{"the person, one a manager recorded", person, &maxID, unasked},
		{"the person, one whose recorder is gone", person, nil, unasked},
		{"an administrator, one the person recorded", admin, &ada, unasked},
		{"an administrator, one a manager recorded", admin, &maxID, unasked},
		{"the manager, one they recorded", master, &maxID, manages},
	}
	for _, c := range allowed {
		t.Run(c.name, func(t *testing.T) {
			if err := mayChange(c.by, hers, c.recordedBy, c.manages); err != nil {
				t.Errorf("refused: %v", err)
			}
		})
	}

	refused := []struct {
		name       string
		by         Recorder
		recordedBy *uuid.UUID
		manages    func() error
	}{
		{"the manager, one the person recorded", master, &ada, manages},
		{"the manager, one an administrator recorded", master, &owner, manages},
		{"the manager, one whose recorder is gone", master, nil, manages},
		{"the manager, one they recorded for somebody no longer on their team", master, &maxID, managesNoLonger},
		{"a colleague, one the person recorded", Recorder{ID: uuid.New()}, &ada, managesNoLonger},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			err := mayChange(c.by, hers, c.recordedBy, c.manages)
			var named *MayNotChangeError
			if !errors.Is(err, ErrMayNotChange) || !errors.As(err, &named) || named.Person != "Ada Away" {
				t.Errorf("err = %v, want a refusal naming Ada Away", err)
			}
		})
	}

	t.Run("a failure to read the person's teams is not taken for a refusal", func(t *testing.T) {
		broken := errors.New("the database went away")
		if err := mayChange(master, hers, &maxID, func() error { return broken }); !errors.Is(err, broken) {
			t.Errorf("err = %v, want the failure itself", err)
		}
	})
}
