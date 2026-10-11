package audit

import "testing"

// A role's definition says what everybody holding it may do, so changing it is
// as much a change of who may do what as a grant, and so is removing a group.
func TestCopiedTakesEveryChangeToWhoMayDoWhat(t *testing.T) {
	for topic, target := range map[string]string{
		"role.granted":   "role",
		"role.revoked":   "role",
		"role.defined":   "role",
		"role.redefined": "role",
		"role.removed":   "role",
		"group.created":  "group",
		"group.deleted":  "group",
	} {
		if got, ok := Copied[topic]; !ok || got != target {
			t.Errorf("Copied[%q] = %q, %v; want %q", topic, got, ok, target)
		}
	}
}
