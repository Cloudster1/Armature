package auth

import "testing"

// Who each rule holds: administrators are owners and admins, everyone is every
// agent, and a portal customer is never asked.
func TestASecondFactorRuleCoversTheStandingsItNames(t *testing.T) {
	cases := []struct {
		rule SecondFactorRule
		role OrgRole
		want bool
	}{
		{SecondFactorOff, RoleOwner, false},
		{SecondFactorAdmins, RoleOwner, true},
		{SecondFactorAdmins, RoleAdmin, true},
		{SecondFactorAdmins, RoleMember, false},
		{SecondFactorEveryone, RoleMember, true},
		{SecondFactorEveryone, RoleCustomer, false},
		{SecondFactorAdmins, RoleCustomer, false},
	}
	for _, c := range cases {
		if got := c.rule.Covers(c.role); got != c.want {
			t.Errorf("%s covers %s = %v, want %v", c.rule, c.role, got, c.want)
		}
	}
	if SecondFactorRule("sometimes").Valid() || !SecondFactorEveryone.Valid() {
		t.Error("Valid misjudged a rule")
	}
}
